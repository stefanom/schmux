// Package runner provides abstractions for session execution environments.
// Implementations include LocalTmuxRunner (local tmux) and ExternalRunner
// (config-driven external commands like dev connect).
package runner

import "context"

// SessionRunner abstracts session lifecycle operations.
// Implementations: LocalTmuxRunner (current), ExternalRunner (config-driven)
type SessionRunner interface {
	// ProvisionEnvironment ensures the execution environment is ready.
	// LocalTmuxRunner: no-op (always returns nil)
	// ExternalRunner: ensures remote environment is provisioned, caches hostname
	ProvisionEnvironment(ctx context.Context) error

	// CreateSession creates a new session with the given options.
	CreateSession(ctx context.Context, opts CreateSessionOpts) error

	// KillSession terminates a session.
	KillSession(ctx context.Context, sessionID string) error

	// SessionExists checks if a session exists.
	SessionExists(ctx context.Context, sessionID string) bool

	// GetPanePID returns the PID of the main process in the session.
	GetPanePID(ctx context.Context, sessionID string) (int, error)

	// CaptureOutput captures the current terminal output.
	CaptureOutput(ctx context.Context, sessionID string) (string, error)

	// CaptureLastLines captures the last N lines of terminal output.
	CaptureLastLines(ctx context.Context, sessionID string, lines int) (string, error)

	// SendKeys sends keys to the session.
	SendKeys(ctx context.Context, sessionID, keys string) error

	// SendLiteral sends literal text to the session.
	SendLiteral(ctx context.Context, sessionID, text string) error

	// SetWindowSizeManual disables automatic window resizing.
	SetWindowSizeManual(ctx context.Context, sessionID string) error

	// ResizeWindow sets the window dimensions.
	ResizeWindow(ctx context.Context, sessionID string, width, height int) error

	// StartPipePane starts streaming output to a log file.
	StartPipePane(ctx context.Context, sessionID, logPath string) error

	// StopPipePane stops output streaming.
	StopPipePane(ctx context.Context, sessionID string) error

	// IsPipePaneActive checks if pipe-pane is running.
	IsPipePaneActive(ctx context.Context, sessionID string) bool

	// RenameSession renames a session.
	RenameSession(ctx context.Context, oldName, newName string) error

	// GetCursorPosition returns the cursor position (x, y) in the session.
	GetCursorPosition(ctx context.Context, sessionID string) (x, y int, err error)

	// ListSessions returns all session names.
	ListSessions(ctx context.Context) ([]string, error)

	// GetAttachCommand returns the command to attach to a session.
	GetAttachCommand(sessionID string) string

	// GetEnvironmentID returns the environment identifier (e.g., OD hostname).
	// Returns empty string for local runners.
	GetEnvironmentID() string
}

// CreateSessionOpts contains options for creating a session.
type CreateSessionOpts struct {
	SessionID string // Unique session identifier (used as tmux session name)
	WorkDir   string // Working directory for the session
	Command   string // Command to run in the session
}
