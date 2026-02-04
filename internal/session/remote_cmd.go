package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/state"
)

// remoteCommandMutex serializes remote command execution to prevent
// concurrent C-b sequences from interfering with each other.
var remoteCommandMutex sync.Mutex

// RemoteCommandResult contains the result of a remote command execution.
type RemoteCommandResult struct {
	Output   string
	ExitCode int
	Error    error
}

// RunRemoteCommand executes a command on a remote workspace via the helper window.
// This works by:
// 1. Switching to the helper window in the remote tmux session
// 2. Clearing the screen and running the command
// 3. Waiting for completion
// 4. Capturing the output
// 5. Switching back to the session's window
//
// The sessionID is the local tmux session that's connected to the remote.
// The command is run in the helper window of the remote tmux session.
func (m *Manager) RunRemoteCommand(ctx context.Context, sessionID, command string) (*RemoteCommandResult, error) {
	// Serialize remote command execution to prevent concurrent C-b sequences
	// from interfering with each other
	remoteCommandMutex.Lock()
	defer remoteCommandMutex.Unlock()

	sess, found := m.state.GetSession(sessionID)
	if !found {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// Get the local tmux session name
	localTmuxSession := sess.TmuxSession

	// Remote tmux session name (fixed for remote sessions)
	remoteTmuxSession := "schmux"

	// Get the session's window name (for switching back)
	sessionWindow := sess.RemoteWindow
	if sessionWindow == "" {
		sessionWindow = sessionID
		if len(sessionWindow) > 20 {
			sessionWindow = sessionWindow[len(sessionWindow)-20:]
		}
	}

	result := &RemoteCommandResult{}

	// Step 1: Switch to helper window using tmux command mode (C-b :)
	// This sends the prefix key to the REMOTE tmux, not the local one
	fmt.Printf("[remote-cmd] switching to helper window\n")
	if err := m.sendTmuxCommand(ctx, localTmuxSession, fmt.Sprintf("select-window -t %s:helper", remoteTmuxSession)); err != nil {
		result.Error = fmt.Errorf("failed to switch to helper window: %w", err)
		return result, result.Error
	}

	time.Sleep(150 * time.Millisecond)

	// Step 2: Clear scrollback to prevent output accumulation
	if err := m.clearRemoteScrollback(ctx, localTmuxSession); err != nil {
		fmt.Printf("[remote-cmd] warning: failed to clear scrollback: %v\n", err)
	}

	// Step 3: Run the command with a completion marker
	// Use printf for screen clear instead of 'clear' to avoid terminal capability queries
	// that can leak escape sequences when switching windows
	marker := fmt.Sprintf("__SCHMUX_DONE_%d__", time.Now().UnixNano())
	wrappedCmd := fmt.Sprintf("printf '\\033[2J\\033[H'; %s; echo '%s'", command, marker)
	fmt.Printf("[remote-cmd] running: %s\n", command)
	if err := m.sendKeysToRemote(ctx, localTmuxSession, wrappedCmd); err != nil {
		result.Error = fmt.Errorf("failed to send command: %w", err)
		return result, result.Error
	}

	// Step 4: Poll for completion
	maxWait := 30 * time.Second
	pollInterval := 250 * time.Millisecond
	deadline := time.Now().Add(maxWait)

	var capturedOutput string
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			result.Error = ctx.Err()
			return result, result.Error
		default:
		}

		output, err := m.captureRemotePane(ctx, localTmuxSession)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}

		if strings.Contains(output, marker) {
			capturedOutput = output
			break
		}

		time.Sleep(pollInterval)
	}

	// Step 5: Parse output
	if capturedOutput != "" {
		lines := strings.Split(capturedOutput, "\n")
		var cleanLines []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, marker) {
				continue
			}
			if strings.HasPrefix(trimmed, "$") || strings.HasPrefix(trimmed, "clear;") {
				continue
			}
			cleanLines = append(cleanLines, line)
		}
		result.Output = strings.TrimSpace(strings.Join(cleanLines, "\n"))
	}

	// Step 6: Switch back to session's window
	fmt.Printf("[remote-cmd] switching back to %s\n", sessionWindow)
	if err := m.sendTmuxCommand(ctx, localTmuxSession, fmt.Sprintf("select-window -t %s:%s", remoteTmuxSession, sessionWindow)); err != nil {
		fmt.Printf("[remote-cmd] warning: failed to switch back: %v\n", err)
	}

	// Small delay to let tmux command mode fully close before returning
	time.Sleep(100 * time.Millisecond)

	return result, nil
}

// sendKeysToRemote sends keys to the remote tmux session via the local session.
// The local session is connected to the remote via devconnect, so we send keys
// that will be interpreted by the remote tmux.
func (m *Manager) sendKeysToRemote(ctx context.Context, localTmuxSession, keys string) error {
	// Send the keys followed by Enter
	fmt.Printf("[remote-cmd] sending to %s: %s\n", localTmuxSession, keys)
	cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", localTmuxSession, keys, "Enter")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// sendTmuxCommand sends a tmux command to the remote tmux session using the prefix key.
// This is different from sendKeysToRemote which types text into the shell.
// This uses C-b : to enter tmux command mode, so the command is executed by tmux itself.
//
// IMPORTANT: We send the keys in separate calls with small delays to ensure
// the remote tmux has time to process each keystroke properly.
func (m *Manager) sendTmuxCommand(ctx context.Context, localTmuxSession, tmuxCmd string) error {
	fmt.Printf("[remote-cmd] tmux command via C-b: %s\n", tmuxCmd)

	// Step 1: Send C-b (prefix key)
	cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", localTmuxSession, "C-b")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send prefix: %w: %s", err, strings.TrimSpace(string(output)))
	}

	// Wait for remote tmux to enter prefix mode
	time.Sleep(50 * time.Millisecond)

	// Step 2: Send : (enter command mode)
	cmd = exec.CommandContext(ctx, "tmux", "send-keys", "-t", localTmuxSession, ":")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send colon: %w: %s", err, strings.TrimSpace(string(output)))
	}

	// Wait for command mode to activate
	time.Sleep(50 * time.Millisecond)

	// Step 3: Send the command and Enter
	cmd = exec.CommandContext(ctx, "tmux", "send-keys", "-t", localTmuxSession, tmuxCmd, "Enter")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send command: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// captureRemotePane captures the current pane content from the local tmux session.
// Since the local session is attached to the remote, this captures what's visible
// in the remote helper window.
func (m *Manager) captureRemotePane(ctx context.Context, localTmuxSession string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", "capture-pane", "-t", localTmuxSession, "-p", "-S", "-100")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane failed: %w", err)
	}
	return string(output), nil
}

// clearRemoteScrollback clears the tmux scrollback history for the local session.
// This prevents output accumulation when running multiple commands.
func (m *Manager) clearRemoteScrollback(ctx context.Context, localTmuxSession string) error {
	cmd := exec.CommandContext(ctx, "tmux", "clear-history", "-t", localTmuxSession)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux clear-history failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// IsRemoteSession checks if a session is a remote/external session.
func (m *Manager) IsRemoteSession(sessionID string) bool {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return false
	}
	ws, found := m.workspace.GetByID(sess.WorkspaceID)
	if !found {
		return false
	}
	return ws.External
}

// SwitchToSessionWindow switches the remote tmux to the session's window.
// This should be called when viewing a session's terminal to ensure the correct
// window is displayed. For sessions sharing a remote connection, this switches
// to that session's specific window.
func (m *Manager) SwitchToSessionWindow(ctx context.Context, sessionID string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Only relevant for sessions with a RemoteWindow (shared remote connections)
	if sess.RemoteWindow == "" {
		return nil
	}

	// Check if the workspace is external (remote)
	ws, found := m.workspace.GetByID(sess.WorkspaceID)
	if !found || !ws.External {
		return nil
	}

	// Switch to the session's remote window
	remoteTmuxSession := "schmux"
	selectWindowCmd := fmt.Sprintf("select-window -t %s:%s", remoteTmuxSession, sess.RemoteWindow)

	fmt.Printf("[session] switching to remote window %s for session %s\n", sess.RemoteWindow, sessionID)
	return m.sendTmuxCommand(ctx, sess.TmuxSession, selectWindowCmd)
}

// GetSessionWorkspace returns the workspace for a session.
func (m *Manager) GetSessionWorkspace(sessionID string) (*state.Workspace, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	ws, found := m.workspace.GetByID(sess.WorkspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", sess.WorkspaceID)
	}
	return ws, nil
}

// UpdateWorkspaceRemoteHost updates the RemoteHost for a workspace.
// This should be called when the hostname is first detected from the session log.
func (m *Manager) UpdateWorkspaceRemoteHost(workspaceID, remoteHost string) error {
	ws, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Only update if not already set
	if ws.RemoteHost != "" {
		return nil
	}

	ws.RemoteHost = remoteHost
	if err := m.state.UpdateWorkspace(ws); err != nil {
		return fmt.Errorf("failed to update workspace: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	fmt.Printf("[session] stored remote host for workspace %s: %s\n", workspaceID, remoteHost)
	return nil
}

// ClearWorkspaceRemoteHost clears the RemoteHost and LocalTmuxSession for a workspace.
// This should be called when the last session for the workspace is disposed.
func (m *Manager) ClearWorkspaceRemoteHost(workspaceID string) error {
	ws, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil // Workspace may already be disposed
	}

	if ws.RemoteHost == "" && ws.LocalTmuxSession == "" {
		return nil // Nothing to clear
	}

	ws.RemoteHost = ""
	ws.LocalTmuxSession = ""
	if err := m.state.UpdateWorkspace(ws); err != nil {
		return fmt.Errorf("failed to update workspace: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	fmt.Printf("[session] cleared remote host and local tmux session for workspace %s\n", workspaceID)
	return nil
}

// HasActiveSessionsForWorkspace checks if a workspace has any running sessions.
func (m *Manager) HasActiveSessionsForWorkspace(ctx context.Context, workspaceID string) bool {
	sessions := m.state.GetSessions()
	for _, sess := range sessions {
		if sess.WorkspaceID == workspaceID {
			if m.IsRunning(ctx, sess.ID) {
				return true
			}
		}
	}
	return false
}

// DetectAndStoreHostname detects the hostname from a session's log file and stores it in the workspace.
// This is called automatically after provisioning a remote session.
func (m *Manager) DetectAndStoreHostname(sessionID, workspaceID string) {
	hostnameRegex := m.config.GetRemoteRunnerHostnameRegex()
	if hostnameRegex == "" {
		fmt.Printf("[session] no hostname_regex configured, skipping hostname detection\n")
		return
	}

	// Poll for hostname in the log file for up to 60 seconds
	maxWait := 60 * time.Second
	pollInterval := 2 * time.Second
	deadline := time.Now().Add(maxWait)

	fmt.Printf("[session] starting hostname detection for session %s\n", sessionID)

	for time.Now().Before(deadline) {
		// Check if workspace already has hostname (might be set by another path)
		ws, found := m.state.GetWorkspace(workspaceID)
		if !found {
			fmt.Printf("[session] workspace %s not found, stopping hostname detection\n", workspaceID)
			return
		}
		if ws.RemoteHost != "" {
			fmt.Printf("[session] hostname already set for workspace %s: %s\n", workspaceID, ws.RemoteHost)
			return
		}

		// Read the log file
		logPath, err := m.GetLogPath(sessionID)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}

		logBytes, err := os.ReadFile(logPath)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}

		logContent := string(logBytes)
		if len(logContent) == 0 {
			time.Sleep(pollInterval)
			continue
		}

		// Strip ANSI escape codes before matching
		ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07`)
		logContent = ansiRegex.ReplaceAllString(logContent, "")

		// Try matching with multiline flag if regex uses ^ anchor
		regexToTry := hostnameRegex
		if strings.HasPrefix(regexToTry, "^") && !strings.HasPrefix(regexToTry, "(?m)") {
			regexToTry = "(?m)" + regexToTry
		}

		re, err := regexp.Compile(regexToTry)
		if err != nil {
			fmt.Printf("[session] invalid hostname regex: %v\n", err)
			return
		}

		matches := re.FindStringSubmatch(logContent)
		hostname := ""
		if len(matches) > 1 {
			hostname = matches[1]
		} else if len(matches) > 0 {
			hostname = matches[0]
		}

		// If no match and regex starts with ^, try without the anchor
		if hostname == "" && strings.HasPrefix(hostnameRegex, "^") {
			regexWithoutAnchor := strings.TrimPrefix(hostnameRegex, "^")
			re, err := regexp.Compile(regexWithoutAnchor)
			if err == nil {
				matches := re.FindStringSubmatch(logContent)
				if len(matches) > 1 {
					hostname = matches[1]
				} else if len(matches) > 0 {
					hostname = matches[0]
				}
			}
		}

		if hostname != "" {
			fmt.Printf("[session] detected hostname: %s\n", hostname)
			if err := m.UpdateWorkspaceRemoteHost(workspaceID, hostname); err != nil {
				fmt.Printf("[session] failed to store hostname: %v\n", err)
			}
			return
		}

		time.Sleep(pollInterval)
	}

	fmt.Printf("[session] hostname detection timed out for session %s\n", sessionID)
}
