package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"text/template"
)

// ErrProvisioningRequired indicates that no environment is available and provisioning is needed.
// Contains the command to run for provisioning, which should be spawned as an interactive session.
type ErrProvisioningRequired struct {
	ProvisionCommand string // The command to run for provisioning (e.g., "dev connect -t flavor --no-connect")
	Flavor           string // The flavor being provisioned
}

func (e *ErrProvisioningRequired) Error() string {
	return fmt.Sprintf("provisioning required for flavor %s", e.Flavor)
}

// IsProvisioningRequired checks if an error indicates provisioning is required.
func IsProvisioningRequired(err error) (*ErrProvisioningRequired, bool) {
	var provErr *ErrProvisioningRequired
	if errors.As(err, &provErr) {
		return provErr, true
	}
	return nil, false
}

// ExternalRunner executes tmux commands via a configurable connection prefix.
// This enables remote session execution via tools like SSH or dev connect.
// Only the connection method is configured - tmux commands are built-in.
type ExternalRunner struct {
	provision        string         // Command to provision environment (e.g., "dev connect -t {{.Flavor}} --no-connect")
	listEnvironments string         // Command to list environments (e.g., "ondemand list")
	connectionPrefix string         // Prefix for tmux commands (e.g., "dev connect -n {{.Hostname}} --")
	hostnameRegex    *regexp.Regexp // Regex to extract hostname from list output
	hostname         string         // Cached after provisioning
	flavor           string         // Flavor for provisioning (e.g., "xplat_react:omniview")
}

// ExternalRunnerConfig is the simplified configuration for ExternalRunner.
type ExternalRunnerConfig struct {
	Provision        string `json:"provision"`         // Command to provision environment
	ListEnvironments string `json:"list_environments"` // Command to list environments
	ConnectionPrefix string `json:"connection_prefix"` // Prefix for tmux commands (e.g., "dev connect -n {{.Hostname}} --")
	HostnameRegex    string `json:"hostname_regex"`    // Regex to extract hostname from list output
}

// NewExternalRunner creates a new ExternalRunner with the given configuration.
func NewExternalRunner(cfg ExternalRunnerConfig) (*ExternalRunner, error) {
	var hostnameRegex *regexp.Regexp
	var err error
	if cfg.HostnameRegex != "" {
		hostnameRegex, err = regexp.Compile(cfg.HostnameRegex)
		if err != nil {
			return nil, fmt.Errorf("invalid hostname_regex: %w", err)
		}
	}

	return &ExternalRunner{
		provision:        cfg.Provision,
		listEnvironments: cfg.ListEnvironments,
		connectionPrefix: cfg.ConnectionPrefix,
		hostnameRegex:    hostnameRegex,
	}, nil
}

// NewExternalRunnerWithFlavor creates a new ExternalRunner with flavor substitution.
// The flavor is substituted into command templates using {{.Flavor}}.
func NewExternalRunnerWithFlavor(cfg ExternalRunnerConfig, flavor string) (*ExternalRunner, error) {
	var hostnameRegex *regexp.Regexp
	var err error
	if cfg.HostnameRegex != "" {
		hostnameRegex, err = regexp.Compile(cfg.HostnameRegex)
		if err != nil {
			return nil, fmt.Errorf("invalid hostname_regex: %w", err)
		}
	}

	return &ExternalRunner{
		provision:        cfg.Provision,
		listEnvironments: cfg.ListEnvironments,
		connectionPrefix: cfg.ConnectionPrefix,
		hostnameRegex:    hostnameRegex,
		flavor:           flavor,
	}, nil
}

// ProvisionEnvironment ensures the remote environment is ready.
// First checks for existing environment, then returns ErrProvisioningRequired if provisioning is needed.
// The caller should create an interactive session running the ProvisionCommand from the error.
func (r *ExternalRunner) ProvisionEnvironment(ctx context.Context) error {
	// First check for existing environment
	if r.listEnvironments != "" {
		output, err := r.runCommand(ctx, r.listEnvironments, map[string]any{
			"Flavor": r.flavor,
		})
		if err == nil {
			hostname := r.parseHostname(output)
			if hostname != "" {
				r.hostname = hostname
				fmt.Printf("[runner] found existing environment: %s\n", hostname)
				return nil
			}
		}
	}

	// No existing environment - provisioning is required
	if r.provision == "" {
		return fmt.Errorf("no provision command configured")
	}

	// Build the provision command
	provisionCmd, err := r.executeTemplate(r.provision, map[string]any{
		"Flavor": r.flavor,
	})
	if err != nil {
		return fmt.Errorf("failed to build provision command: %w", err)
	}

	fmt.Printf("[runner] provisioning required for flavor %s\n", r.flavor)
	return &ErrProvisioningRequired{
		ProvisionCommand: provisionCmd,
		Flavor:           r.flavor,
	}
}

// CreateSession creates a new session.
func (r *ExternalRunner) CreateSession(ctx context.Context, opts CreateSessionOpts) error {
	// tmux new-session -d -s {session} -c {workdir} {command}
	tmuxCmd := fmt.Sprintf("tmux new-session -d -s %s -c %s %s",
		ShellQuote(opts.SessionID),
		ShellQuote(opts.WorkDir),
		ShellQuote(opts.Command))
	_, err := r.runTmuxCommand(ctx, tmuxCmd)
	return err
}

// KillSession terminates a session.
func (r *ExternalRunner) KillSession(ctx context.Context, sessionID string) error {
	tmuxCmd := fmt.Sprintf("tmux kill-session -t %s", ShellQuote(sessionID))
	_, err := r.runTmuxCommand(ctx, tmuxCmd)
	return err
}

// SessionExists checks if a session exists.
func (r *ExternalRunner) SessionExists(ctx context.Context, sessionID string) bool {
	tmuxCmd := fmt.Sprintf("tmux has-session -t %s", ShellQuote(sessionID))
	_, err := r.runTmuxCommand(ctx, tmuxCmd)
	return err == nil
}

// GetPanePID returns the PID of the main process in the session.
func (r *ExternalRunner) GetPanePID(ctx context.Context, sessionID string) (int, error) {
	tmuxCmd := fmt.Sprintf("tmux display-message -t %s -p '#{pane_pid}'", ShellQuote(sessionID))
	output, err := r.runTmuxCommand(ctx, tmuxCmd)
	if err != nil {
		return 0, err
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(output), "%d", &pid); err != nil {
		return 0, fmt.Errorf("failed to parse PID: %w", err)
	}
	return pid, nil
}

// CaptureOutput captures the current terminal output.
func (r *ExternalRunner) CaptureOutput(ctx context.Context, sessionID string) (string, error) {
	tmuxCmd := fmt.Sprintf("tmux capture-pane -t %s -e -p", ShellQuote(sessionID))
	return r.runTmuxCommand(ctx, tmuxCmd)
}

// CaptureLastLines captures the last N lines of terminal output.
func (r *ExternalRunner) CaptureLastLines(ctx context.Context, sessionID string, lines int) (string, error) {
	tmuxCmd := fmt.Sprintf("tmux capture-pane -t %s -e -p -S -%d", ShellQuote(sessionID), lines)
	return r.runTmuxCommand(ctx, tmuxCmd)
}

// SendKeys sends keys to the session.
func (r *ExternalRunner) SendKeys(ctx context.Context, sessionID, keys string) error {
	tmuxCmd := fmt.Sprintf("tmux send-keys -t %s %s", ShellQuote(sessionID), ShellQuote(keys))
	_, err := r.runTmuxCommand(ctx, tmuxCmd)
	return err
}

// SendLiteral sends literal text to the session.
func (r *ExternalRunner) SendLiteral(ctx context.Context, sessionID, text string) error {
	tmuxCmd := fmt.Sprintf("tmux send-keys -t %s -l %s", ShellQuote(sessionID), ShellQuote(text))
	_, err := r.runTmuxCommand(ctx, tmuxCmd)
	return err
}

// SetWindowSizeManual disables automatic window resizing.
func (r *ExternalRunner) SetWindowSizeManual(ctx context.Context, sessionID string) error {
	tmuxCmd := fmt.Sprintf("tmux set-option -t %s aggressive-resize off", ShellQuote(sessionID))
	_, err := r.runTmuxCommand(ctx, tmuxCmd)
	return err
}

// ResizeWindow sets the window dimensions.
func (r *ExternalRunner) ResizeWindow(ctx context.Context, sessionID string, width, height int) error {
	tmuxCmd := fmt.Sprintf("tmux resize-window -t %s -x %d -y %d", ShellQuote(sessionID), width, height)
	_, err := r.runTmuxCommand(ctx, tmuxCmd)
	return err
}

// StartPipePane starts streaming output to a log file.
func (r *ExternalRunner) StartPipePane(ctx context.Context, sessionID, logPath string) error {
	tmuxCmd := fmt.Sprintf("tmux pipe-pane -t %s 'cat >> %s'", ShellQuote(sessionID), ShellQuote(logPath))
	_, err := r.runTmuxCommand(ctx, tmuxCmd)
	return err
}

// StopPipePane stops output streaming.
func (r *ExternalRunner) StopPipePane(ctx context.Context, sessionID string) error {
	tmuxCmd := fmt.Sprintf("tmux pipe-pane -t %s", ShellQuote(sessionID))
	_, err := r.runTmuxCommand(ctx, tmuxCmd)
	return err
}

// IsPipePaneActive checks if pipe-pane is running.
func (r *ExternalRunner) IsPipePaneActive(ctx context.Context, sessionID string) bool {
	tmuxCmd := fmt.Sprintf("tmux display-message -t %s -p '#{pane_pipe}'", ShellQuote(sessionID))
	output, err := r.runTmuxCommand(ctx, tmuxCmd)
	if err != nil {
		return false
	}
	trimmed := strings.TrimSpace(output)
	return trimmed != "" && trimmed != "0"
}

// RenameSession renames a session.
func (r *ExternalRunner) RenameSession(ctx context.Context, oldName, newName string) error {
	tmuxCmd := fmt.Sprintf("tmux rename-session -t %s %s", ShellQuote(oldName), ShellQuote(newName))
	_, err := r.runTmuxCommand(ctx, tmuxCmd)
	return err
}

// GetCursorPosition returns the cursor position (x, y) in the session.
func (r *ExternalRunner) GetCursorPosition(ctx context.Context, sessionID string) (x, y int, err error) {
	tmuxCmd := fmt.Sprintf("tmux display-message -t %s -p '#{cursor_x} #{cursor_y}'", ShellQuote(sessionID))
	output, err := r.runTmuxCommand(ctx, tmuxCmd)
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Fields(strings.TrimSpace(output))
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected cursor position format: %q", output)
	}
	if _, err := fmt.Sscanf(parts[0], "%d", &x); err != nil {
		return 0, 0, fmt.Errorf("failed to parse cursor_x: %w", err)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &y); err != nil {
		return 0, 0, fmt.Errorf("failed to parse cursor_y: %w", err)
	}
	return x, y, nil
}

// ListSessions returns all session names.
func (r *ExternalRunner) ListSessions(ctx context.Context) ([]string, error) {
	tmuxCmd := "tmux list-sessions -F '#{session_name}'"
	output, err := r.runTmuxCommand(ctx, tmuxCmd)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return []string{}, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// GetAttachCommand returns the command to attach to a session.
func (r *ExternalRunner) GetAttachCommand(sessionID string) string {
	prefix, err := r.executeTemplate(r.connectionPrefix, map[string]any{
		"Hostname": r.hostname,
	})
	if err != nil {
		return fmt.Sprintf("# error generating attach command: %v", err)
	}
	return fmt.Sprintf("%s tmux attach -t %s", prefix, ShellQuote(sessionID))
}

// GetEnvironmentID returns the environment identifier (e.g., OD hostname).
func (r *ExternalRunner) GetEnvironmentID() string {
	return r.hostname
}

// runTmuxCommand executes a tmux command via the connection prefix.
func (r *ExternalRunner) runTmuxCommand(ctx context.Context, tmuxCmd string) (string, error) {
	if r.connectionPrefix == "" {
		return "", fmt.Errorf("connection_prefix not configured")
	}

	// Build the full command: prefix + tmux command
	prefix, err := r.executeTemplate(r.connectionPrefix, map[string]any{
		"Hostname": r.hostname,
	})
	if err != nil {
		return "", err
	}

	fullCmd := prefix + " " + tmuxCmd
	cmd := exec.CommandContext(ctx, "bash", "-c", fullCmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

// runCommand executes a command template with the given variables.
func (r *ExternalRunner) runCommand(ctx context.Context, tmplStr string, vars map[string]any) (string, error) {
	cmdLine, err := r.executeTemplate(tmplStr, vars)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", cmdLine)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

// executeTemplate parses and executes a template with the given variables.
func (r *ExternalRunner) executeTemplate(tmplStr string, vars map[string]any) (string, error) {
	t, err := template.New("cmd").Funcs(template.FuncMap{
		"shellquote": ShellQuote,
	}).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}
	return buf.String(), nil
}

// parseHostname extracts a hostname from command output using the configured regex.
func (r *ExternalRunner) parseHostname(output string) string {
	if r.hostnameRegex == nil {
		// No regex configured, try to use first non-empty line
		lines := strings.Split(strings.TrimSpace(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				return line
			}
		}
		return ""
	}

	matches := r.hostnameRegex.FindStringSubmatch(output)
	if len(matches) > 1 {
		return matches[1] // First capture group
	}
	if len(matches) > 0 {
		return matches[0] // Full match
	}
	return ""
}

// ShellQuote quotes a string for safe use in shell commands.
// Uses single quotes with proper escaping for embedded single quotes.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// CommandTemplates is kept for backward compatibility with old configs.
// New configs should use ExternalRunnerConfig directly.
type CommandTemplates struct {
	Provision        string `json:"provision"`
	ListEnvironments string `json:"list_environments"`
	ConnectionPrefix string `json:"connection_prefix"`
}
