package runner

import (
	"context"

	"github.com/sergeknystautas/schmux/internal/tmux"
)

// LocalTmuxRunner implements SessionRunner using local tmux.
// This is the default runner for local development.
type LocalTmuxRunner struct{}

// NewLocalTmuxRunner creates a new LocalTmuxRunner.
func NewLocalTmuxRunner() *LocalTmuxRunner {
	return &LocalTmuxRunner{}
}

// ProvisionEnvironment is a no-op for local tmux (always available).
func (r *LocalTmuxRunner) ProvisionEnvironment(ctx context.Context) error {
	return nil
}

// CreateSession creates a new tmux session.
func (r *LocalTmuxRunner) CreateSession(ctx context.Context, opts CreateSessionOpts) error {
	return tmux.CreateSession(ctx, opts.SessionID, opts.WorkDir, opts.Command)
}

// KillSession terminates a tmux session.
func (r *LocalTmuxRunner) KillSession(ctx context.Context, sessionID string) error {
	return tmux.KillSession(ctx, sessionID)
}

// SessionExists checks if a tmux session exists.
func (r *LocalTmuxRunner) SessionExists(ctx context.Context, sessionID string) bool {
	return tmux.SessionExists(ctx, sessionID)
}

// GetPanePID returns the PID of the main process in the tmux pane.
func (r *LocalTmuxRunner) GetPanePID(ctx context.Context, sessionID string) (int, error) {
	return tmux.GetPanePID(ctx, sessionID)
}

// CaptureOutput captures the current terminal output.
func (r *LocalTmuxRunner) CaptureOutput(ctx context.Context, sessionID string) (string, error) {
	return tmux.CaptureOutput(ctx, sessionID)
}

// CaptureLastLines captures the last N lines of terminal output.
func (r *LocalTmuxRunner) CaptureLastLines(ctx context.Context, sessionID string, lines int) (string, error) {
	return tmux.CaptureLastLines(ctx, sessionID, lines)
}

// SendKeys sends keys to the tmux session.
func (r *LocalTmuxRunner) SendKeys(ctx context.Context, sessionID, keys string) error {
	return tmux.SendKeys(ctx, sessionID, keys)
}

// SendLiteral sends literal text to the tmux session.
func (r *LocalTmuxRunner) SendLiteral(ctx context.Context, sessionID, text string) error {
	return tmux.SendLiteral(ctx, sessionID, text)
}

// SetWindowSizeManual disables automatic window resizing.
func (r *LocalTmuxRunner) SetWindowSizeManual(ctx context.Context, sessionID string) error {
	return tmux.SetWindowSizeManual(ctx, sessionID)
}

// ResizeWindow sets the window dimensions.
func (r *LocalTmuxRunner) ResizeWindow(ctx context.Context, sessionID string, width, height int) error {
	return tmux.ResizeWindow(ctx, sessionID, width, height)
}

// StartPipePane starts streaming output to a log file.
func (r *LocalTmuxRunner) StartPipePane(ctx context.Context, sessionID, logPath string) error {
	return tmux.StartPipePane(ctx, sessionID, logPath)
}

// StopPipePane stops output streaming.
func (r *LocalTmuxRunner) StopPipePane(ctx context.Context, sessionID string) error {
	return tmux.StopPipePane(ctx, sessionID)
}

// IsPipePaneActive checks if pipe-pane is running.
func (r *LocalTmuxRunner) IsPipePaneActive(ctx context.Context, sessionID string) bool {
	return tmux.IsPipePaneActive(ctx, sessionID)
}

// RenameSession renames a tmux session.
func (r *LocalTmuxRunner) RenameSession(ctx context.Context, oldName, newName string) error {
	return tmux.RenameSession(ctx, oldName, newName)
}

// GetCursorPosition returns the cursor position (x, y) in the tmux pane.
func (r *LocalTmuxRunner) GetCursorPosition(ctx context.Context, sessionID string) (x, y int, err error) {
	return tmux.GetCursorPosition(ctx, sessionID)
}

// ListSessions returns all tmux session names.
func (r *LocalTmuxRunner) ListSessions(ctx context.Context) ([]string, error) {
	return tmux.ListSessions(ctx)
}

// GetAttachCommand returns the tmux attach command.
func (r *LocalTmuxRunner) GetAttachCommand(sessionID string) string {
	return tmux.GetAttachCommand(sessionID)
}

// GetEnvironmentID returns empty string for local runners.
func (r *LocalTmuxRunner) GetEnvironmentID() string {
	return ""
}
