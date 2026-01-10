package tmux

// TmuxService defines the interface for tmux operations.
type TmuxService interface {
	// CreateSession creates a new tmux session with the given name, directory, and command.
	CreateSession(name, dir, command string) error

	// KillSession kills a tmux session.
	KillSession(name string) error

	// ListSessions returns a list of all tmux session names.
	ListSessions() ([]string, error)

	// SessionExists checks if a tmux session with the given name exists.
	SessionExists(name string) bool

	// GetPanePID returns the PID of the first process in the tmux session's pane.
	GetPanePID(name string) (int, error)

	// CaptureOutput captures the current output of a tmux session.
	CaptureOutput(name string) (string, error)

	// CaptureLastLines captures the last N lines of the pane.
	CaptureLastLines(name string, lines int) (string, error)

	// SendKeys sends keys to a tmux session.
	SendKeys(name, keys string) error

	// GetAttachCommand returns the command to attach to a tmux session.
	GetAttachCommand(name string) string

	// SetWindowSizeManual forces tmux to ignore client resize requests.
	SetWindowSizeManual(sessionName string) error

	// ResizeWindow resizes the window to fixed dimensions.
	ResizeWindow(sessionName string, width, height int) error

	// StartPipePane begins streaming pane output to a log file.
	StartPipePane(sessionName, logPath string) error

	// StopPipePane stops streaming pane output.
	StopPipePane(sessionName string) error

	// IsPipePaneActive checks if pipe-pane is running for a session.
	IsPipePaneActive(sessionName string) bool

	// RenameSession renames an existing tmux session.
	RenameSession(oldName, newName string) error
}

// Ensure the package functions implement TmuxService via a type adapter.
// The concrete implementation is provided by the package-level functions.
type tmuxService struct{}

// NewTmuxService creates a new TmuxService backed by the package-level functions.
func NewTmuxService() TmuxService {
	return &tmuxService{}
}

func (t *tmuxService) CreateSession(name, dir, command string) error {
	return CreateSession(name, dir, command)
}

func (t *tmuxService) KillSession(name string) error {
	return KillSession(name)
}

func (t *tmuxService) ListSessions() ([]string, error) {
	return ListSessions()
}

func (t *tmuxService) SessionExists(name string) bool {
	return SessionExists(name)
}

func (t *tmuxService) GetPanePID(name string) (int, error) {
	return GetPanePID(name)
}

func (t *tmuxService) CaptureOutput(name string) (string, error) {
	return CaptureOutput(name)
}

func (t *tmuxService) CaptureLastLines(name string, lines int) (string, error) {
	return CaptureLastLines(name, lines)
}

func (t *tmuxService) SendKeys(name, keys string) error {
	return SendKeys(name, keys)
}

func (t *tmuxService) GetAttachCommand(name string) string {
	return GetAttachCommand(name)
}

func (t *tmuxService) SetWindowSizeManual(sessionName string) error {
	return SetWindowSizeManual(sessionName)
}

func (t *tmuxService) ResizeWindow(sessionName string, width, height int) error {
	return ResizeWindow(sessionName, width, height)
}

func (t *tmuxService) StartPipePane(sessionName, logPath string) error {
	return StartPipePane(sessionName, logPath)
}

func (t *tmuxService) StopPipePane(sessionName string) error {
	return StopPipePane(sessionName)
}

func (t *tmuxService) IsPipePaneActive(sessionName string) bool {
	return IsPipePaneActive(sessionName)
}

func (t *tmuxService) RenameSession(oldName, newName string) error {
	return RenameSession(oldName, newName)
}
