package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/dashboard"
	"github.com/sergek/schmux/internal/session"
	"github.com/sergek/schmux/internal/state"
	"github.com/sergek/schmux/internal/tmux"
	"github.com/sergek/schmux/internal/workspace"
)

const (
	pidFileName   = "daemon.pid"
	dashboardPort = 7337
)

var (
	shutdownChan = make(chan struct{})
	shutdownCtx  context.Context
	cancelFunc   context.CancelFunc
)

func init() {
	shutdownCtx, cancelFunc = context.WithCancel(context.Background())
}

// Daemon represents the schmux daemon.
type Daemon struct {
	config    *config.Config
	state     state.StateStore
	workspace workspace.WorkspaceManager
	session   *session.Manager
	server    *dashboard.Server
}

// ValidateReadyToRun checks if the system is ready to run the daemon.
// It verifies tmux is available, the schmux directory exists, and
// that no daemon is already running. Called by both 'start' and 'daemon-run'
// before they diverge.
func ValidateReadyToRun() error {
	// Check tmux dependency before forking
	if err := checkTmux(); err != nil {
		return err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	schmuxDir := filepath.Join(homeDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		return fmt.Errorf("failed to create schmux directory: %w", err)
	}

	pidFile := filepath.Join(schmuxDir, pidFileName)

	// Check if already running
	if _, err := os.Stat(pidFile); err == nil {
		// PID file exists, check if process is running
		pidData, err := os.ReadFile(pidFile)
		if err != nil {
			return fmt.Errorf("failed to read PID file: %w", err)
		}

		var pid int
		if _, err := fmt.Sscanf(string(pidData), "%d", &pid); err == nil {
			process, err := os.FindProcess(pid)
			if err == nil {
				if err := process.Signal(syscall.Signal(0)); err == nil {
					return fmt.Errorf("daemon is already running (PID %d)", pid)
				}
			}
		}

		// Process not running, remove stale PID file
		os.Remove(pidFile)
	}

	return nil
}

// Start starts the daemon in the background.
func Start() error {
	// Get the path to the current executable
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start daemon in background
	cmd := exec.Command(execPath, "daemon-run")
	cmd.Dir, _ = os.Getwd()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait a bit for daemon to start
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-time.After(100 * time.Millisecond):
		// Daemon started successfully
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for daemon to start")
	}

	return nil
}

// Stop stops the daemon.
func Stop() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	pidFile := filepath.Join(homeDir, ".schmux", pidFileName)

	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("daemon is not running")
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	var pid int
	if _, err := fmt.Sscanf(string(pidData), "%d", &pid); err != nil {
		return fmt.Errorf("failed to parse PID: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send SIGTERM
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	// Wait for process to exit by polling (process.Wait() doesn't work for non-child processes)
	// Check every 100ms, up to 5 seconds
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		// Check if process still exists by sending signal 0
		if err := process.Signal(syscall.Signal(0)); err != nil {
			// Process has exited
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for daemon to stop")
}

// Status returns the status of the daemon.
func Status() (running bool, url string, startedAt string, err error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false, "", "", fmt.Errorf("failed to get home directory: %w", err)
	}

	pidFile := filepath.Join(homeDir, ".schmux", pidFileName)
	startedFile := filepath.Join(homeDir, ".schmux", "daemon.started")

	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "", "", nil
		}
		return false, "", "", fmt.Errorf("failed to read PID file: %w", err)
	}

	var pid int
	if _, err := fmt.Sscanf(string(pidData), "%d", &pid); err != nil {
		return false, "", "", fmt.Errorf("failed to parse PID: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false, "", "", fmt.Errorf("failed to find process: %w", err)
	}

	// Check if process is running
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false, "", "", nil
	}

	url = fmt.Sprintf("http://localhost:%d", dashboardPort)
	if startedData, err := os.ReadFile(startedFile); err == nil {
		startedAt = strings.TrimSpace(string(startedData))
	}
	return true, url, startedAt, nil
}

// Run runs the daemon (this is the entry point for the daemon process).
func Run() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	schmuxDir := filepath.Join(homeDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		return fmt.Errorf("failed to create schmux directory: %w", err)
	}

	pidFile := filepath.Join(schmuxDir, pidFileName)
	startedFile := filepath.Join(schmuxDir, "daemon.started")

	// Write PID file
	pid := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", pid)), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer os.Remove(pidFile)

	// Record daemon start time
	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := os.WriteFile(startedFile, []byte(startedAt+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to write daemon start time: %w", err)
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Compute state path
	homeDir, err = os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	statePath := filepath.Join(homeDir, ".schmux", "state.json")

	// Load state
	st, err := state.Load(statePath)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Create managers
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)

	// Initialize LastOutputAt from log file mtimes for existing sessions
	for _, sess := range st.GetSessions() {
		logPath, err := sm.GetLogPath(sess.ID)
		if err != nil {
			continue
		}
		if info, err := os.Stat(logPath); err == nil {
			sess.LastOutputAt = info.ModTime()
			if err := st.UpdateSession(sess); err != nil {
				fmt.Printf("warning: failed to update session %s: %v\n", sess.ID, err)
			}
		}
	}

	// Start background goroutine to monitor log file mtimes for all sessions
	go func() {
		pollInterval := time.Duration(cfg.GetMtimePollIntervalMs()) * time.Millisecond
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(shutdownCtx, cfg.TmuxQueryTimeout())
				for _, sess := range st.GetSessions() {
					if !sm.IsRunning(ctx, sess.ID) {
						continue
					}
					logPath, err := sm.GetLogPath(sess.ID)
					if err != nil {
						continue
					}
					if info, err := os.Stat(logPath); err == nil {
						if info.ModTime().After(sess.LastOutputAt) {
							st.UpdateSessionLastOutput(sess.ID, info.ModTime())
						}
					}
				}
				cancel()
			case <-shutdownCtx.Done():
				return
			}
		}
	}()

	// Start background goroutine to update git status for all workspaces
	go func() {
		pollInterval := time.Duration(cfg.GetGitStatusPollIntervalMs()) * time.Millisecond
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		// Do initial update after a short delay to let daemon start
		select {
		case <-time.After(500 * time.Millisecond):
			ctx, cancel := context.WithTimeout(shutdownCtx, cfg.GitStatusTimeout())
			wm.UpdateAllGitStatus(ctx)
			cancel()
		case <-shutdownCtx.Done():
			return
		}
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(shutdownCtx, cfg.GitStatusTimeout())
				wm.UpdateAllGitStatus(ctx)
				cancel()
			case <-shutdownCtx.Done():
				return
			}
		}
	}()

	// Bootstrap log streams for active sessions with missing pipe-pane.
	seedLines := cfg.GetTerminalSeedLines()
	if seedLines <= 0 {
		return fmt.Errorf("terminal.seed_lines must be configured")
	}
	for _, sess := range st.GetSessions() {
		if err := bootstrapSession(shutdownCtx, sess, sm, cfg, seedLines); err != nil {
			return err
		}
	}

	// Start log pruner (every 60 minutes)
	pruneInterval := 60 * time.Minute
	stopLogPruner := sm.StartLogPruner(pruneInterval)
	defer stopLogPruner()

	// Ensure workspace directory exists
	if err := wm.EnsureWorkspaceDir(); err != nil {
		return fmt.Errorf("failed to create workspace directory: %w", err)
	}

	// Create dashboard server
	server := dashboard.NewServer(cfg, st, statePath, sm, wm)

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start dashboard server in background
	serverErrChan := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil {
			serverErrChan <- err
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case sig := <-sigChan:
		fmt.Printf("Received signal %v, shutting down...\n", sig)
	case err := <-serverErrChan:
		return fmt.Errorf("dashboard server error: %w", err)
	case <-shutdownChan:
		fmt.Println("Shutdown requested")
	}

	// Stop dashboard server
	if err := server.Stop(); err != nil {
		return fmt.Errorf("failed to stop server: %w", err)
	}

	return nil
}

// Shutdown triggers a graceful shutdown.
func Shutdown() {
	close(shutdownChan)
	if cancelFunc != nil {
		cancelFunc()
	}
}

// bootstrapSession bootstraps a session's log streaming if needed.
func bootstrapSession(ctx context.Context, sess state.Session, sm *session.Manager, cfg *config.Config, seedLines int) error {
	// Check if session exists
	timeoutCtx, cancel := context.WithTimeout(ctx, cfg.TmuxQueryTimeout())
	if !tmux.SessionExists(timeoutCtx, sess.TmuxSession) {
		cancel()
		return nil
	}
	cancel()

	// Check if pipe-pane is active
	timeoutCtx, cancel = context.WithTimeout(ctx, cfg.TmuxQueryTimeout())
	pipePaneActive := tmux.IsPipePaneActive(timeoutCtx, sess.TmuxSession)
	cancel()

	// Get log path
	logPath, err := sm.GetLogPath(sess.ID)
	if err != nil {
		return fmt.Errorf("failed to get log path for %s: %w", sess.ID, err)
	}

	// Check if log file exists
	_, err = os.Stat(logPath)
	logFileExists := !os.IsNotExist(err)

	// Skip if pipe-pane is active AND log file exists (everything is good)
	if pipePaneActive && logFileExists {
		return nil
	}

	// If pipe-pane is active but log is missing, stop the old pipe-pane
	if pipePaneActive && !logFileExists {
		fmt.Printf("[bootstrap] %s: pipe-pane active but log missing, stopping pipe-pane\n", sess.ID)
		timeoutCtx, cancel = context.WithTimeout(ctx, cfg.TmuxOperationTimeout())
		if err := tmux.StopPipePane(timeoutCtx, sess.TmuxSession); err != nil {
			cancel()
			return fmt.Errorf("failed to stop pipe-pane for %s: %w", sess.ID, err)
		}
		cancel()
	}

	if !pipePaneActive {
		fmt.Printf("[bootstrap] %s: pipe-pane not active, bootstrapping\n", sess.ID)
	}

	// Bootstrap: capture screen content and write to log, start pipe-pane
	timeoutCtx, cancel = context.WithTimeout(ctx, cfg.TmuxOperationTimeout())
	snapshot, err := tmux.CaptureLastLines(timeoutCtx, sess.TmuxSession, seedLines)
	cancel()
	if err != nil {
		return fmt.Errorf("failed to capture %d lines for %s: %w", seedLines, sess.ID, err)
	}

	if err := os.WriteFile(logPath, []byte(snapshot), 0644); err != nil {
		return fmt.Errorf("failed to seed log file for %s: %w", sess.ID, err)
	}

	timeoutCtx, cancel = context.WithTimeout(ctx, cfg.TmuxOperationTimeout())
	if err := tmux.StartPipePane(timeoutCtx, sess.TmuxSession, logPath); err != nil {
		cancel()
		return fmt.Errorf("failed to attach pipe-pane for %s: %w", sess.ID, err)
	}
	cancel()
	fmt.Printf("[bootstrap] %s: pipe-pane started\n", sess.ID)
	return nil
}

// checkTmux verifies that tmux is installed and accessible.
func checkTmux() error {
	cmd := exec.Command("tmux", "-V")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux is not installed or not accessible.\n-> %w", err)
	}
	// tmux -V outputs version info like "tmux 3.3a", this confirms it's working
	if len(output) == 0 {
		return fmt.Errorf("tmux command produced no output")
	}
	return nil
}
