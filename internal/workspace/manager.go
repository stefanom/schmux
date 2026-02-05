package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/runner"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/vcs"
)

const (
	// workspaceNumberFormat is the format string for workspace numbering (e.g., "001", "002").
	// Supports up to 999 workspaces per repository.
	workspaceNumberFormat = "%03d"
)

// Manager manages workspace directories.
type Manager struct {
	config               *config.Config
	state                state.StateStore
	vcs                  vcs.VersionControl
	workspaceConfigs     map[string]*contracts.RepoConfig // workspace ID -> workspace config
	workspaceConfigsMu   sync.RWMutex
	configStates         map[string]configState // workspace path -> last known config file state
	configStatesMu       sync.RWMutex
	gitWatcher           *GitWatcher
	repoLocks            map[string]*sync.Mutex
	repoLocksMu          sync.Mutex
	randSuffix           func(length int) string
	defaultBranchCache   map[string]string // repoURL -> defaultBranch or "unknown"
	defaultBranchCacheMu sync.RWMutex

	// Remote provisioning fields
	runner          runner.SessionRunner   // Local tmux runner for creating provisioning sessions
	provisionLocks  map[string]*sync.Mutex // Per-workspace locks for remote provisioning
	provisionLockMu sync.Mutex             // Protects provisionLocks map
}

// New creates a new workspace manager.
// If v is nil, a GitVCS is used by default.
// If r is nil, a LocalTmuxRunner is used by default (for remote provisioning).
func New(cfg *config.Config, st state.StateStore, statePath string, v vcs.VersionControl, r runner.SessionRunner) *Manager {
	if v == nil {
		v = vcs.NewGitVCS()
	}
	if r == nil {
		r = runner.NewLocalTmuxRunner()
	}
	m := &Manager{
		config:           cfg,
		state:            st,
		vcs:              v,
		workspaceConfigs: make(map[string]*contracts.RepoConfig), // cache for .schmux/config.json per workspace
		configStates:     make(map[string]configState),           // track config file mtime to detect changes
		repoLocks:        make(map[string]*sync.Mutex),
		randSuffix:       defaultRandSuffix,
		runner:           r,
		provisionLocks:   make(map[string]*sync.Mutex),
	}
	// Pre-load workspace configs so they're available on first API call
	// (before the first poll cycle runs)
	for _, w := range st.GetWorkspaces() {
		m.RefreshWorkspaceConfig(w)
	}
	return m
}

// SetGitWatcher sets the git watcher for the manager.
func (m *Manager) SetGitWatcher(gw *GitWatcher) {
	m.gitWatcher = gw
}

// IsVCSManaged returns true if version control is managed by Schmux.
// Returns false when VCS is external (e.g., Meta's internal systems).
func (m *Manager) IsVCSManaged() bool {
	return m.vcs.IsManaged()
}

func (m *Manager) repoLock(repoURL string) *sync.Mutex {
	m.repoLocksMu.Lock()
	defer m.repoLocksMu.Unlock()
	lock, ok := m.repoLocks[repoURL]
	if !ok {
		lock = &sync.Mutex{}
		m.repoLocks[repoURL] = lock
	}
	return lock
}

// GetDefaultBranch returns the cached default branch for a repo URL.
// Returns an error if the default branch cannot be determined.
// Uses negative caching ("unknown") to avoid repeated failed git commands.
func (m *Manager) GetDefaultBranch(ctx context.Context, repoURL string) (string, error) {
	// Check in-memory cache first
	m.defaultBranchCacheMu.RLock()
	if branch, ok := m.defaultBranchCache[repoURL]; ok {
		m.defaultBranchCacheMu.RUnlock()
		if branch == "unknown" {
			// Previously failed to detect - don't keep retrying
			return "", fmt.Errorf("default branch unknown for %s", repoURL)
		}
		return branch, nil
	}
	m.defaultBranchCacheMu.RUnlock()

	// Detect from origin query repo (preferred - created on daemon startup)
	queryRepoPath, err := m.ensureOriginQueryRepo(ctx, repoURL)
	if err != nil {
		m.setDefaultBranch(repoURL, "unknown")
		return "", err
	}

	branch := m.getDefaultBranch(ctx, queryRepoPath)
	if branch != "" {
		// Cache the result
		m.setDefaultBranch(repoURL, branch)
		return branch, nil
	}

	// Detection failed - cache as "unknown"
	m.setDefaultBranch(repoURL, "unknown")
	return "", fmt.Errorf("failed to detect default branch for %s", repoURL)
}

// setDefaultBranch caches the default branch in memory.
func (m *Manager) setDefaultBranch(repoURL, branch string) {
	m.defaultBranchCacheMu.Lock()
	defer m.defaultBranchCacheMu.Unlock()
	if m.defaultBranchCache == nil {
		m.defaultBranchCache = make(map[string]string)
	}
	m.defaultBranchCache[repoURL] = branch
}

// GetByID returns a workspace by its ID.
func (m *Manager) GetByID(workspaceID string) (*state.Workspace, bool) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, false
	}
	return &w, true
}

// hasActiveSessions returns true if the workspace has any active sessions.
func (m *Manager) hasActiveSessions(workspaceID string) bool {
	for _, s := range m.state.GetSessions() {
		if s.WorkspaceID == workspaceID {
			return true
		}
	}
	return false
}

// GetOrCreate finds an existing workspace for the repoURL/branch or creates a new one.
// Returns a workspace ready for use (fetch/pull/clean already done).
// For local repositories (URL format "local:{name}"), always creates a fresh workspace.
// For remote repos, creates an external workspace pointing to the configured path.
func (m *Manager) GetOrCreate(ctx context.Context, repoURL, branch string) (*state.Workspace, error) {
	// Check if this is an remote repo
	repoConfig, found := m.config.FindRepoByURL(repoURL)
	if found && repoConfig.IsRemote() {
		return m.getOrCreateRemoteWorkspace(ctx, repoConfig, branch)
	}

	if err := ValidateBranchName(branch); err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}

	// Handle local repositories (format: "local:{name}")
	if strings.HasPrefix(repoURL, "local:") {
		repoName := strings.TrimPrefix(repoURL, "local:")
		return m.CreateLocalRepo(ctx, repoName, branch)
	}

	lock := m.repoLock(repoURL)
	lock.Lock()
	defer lock.Unlock()

	// Try to find an existing workspace with matching repoURL and branch
	for _, w := range m.state.GetWorkspaces() {
		// Check if workspace directory still exists
		if _, err := os.Stat(w.Path); os.IsNotExist(err) {
			fmt.Printf("[workspace] directory missing, skipping: id=%s path=%s\n", w.ID, w.Path)
			continue
		}
		if w.Repo == repoURL && w.Branch == branch {
			// Check if workspace has active sessions
			if !m.hasActiveSessions(w.ID) {
				fmt.Printf("[workspace] reusing existing: id=%s path=%s branch=%s\n", w.ID, w.Path, branch)
				// Prepare the workspace (fetch/pull/clean)
				if err := m.prepare(ctx, w.ID, branch); err != nil {
					return nil, fmt.Errorf("failed to prepare workspace: %w", err)
				}
				return &w, nil
			}
		}
	}

	// Try to find any unused workspace for this repo (different branch OK)
	for _, w := range m.state.GetWorkspaces() {
		if w.Repo == repoURL {
			// Check if workspace has active sessions
			if !m.hasActiveSessions(w.ID) {
				fmt.Printf("[workspace] reusing for different branch: id=%s old=%s new=%s\n", w.ID, w.Branch, branch)
				// Prepare the workspace (fetch/pull/clean) BEFORE updating state
				if err := m.prepare(ctx, w.ID, branch); err != nil {
					return nil, fmt.Errorf("failed to prepare workspace: %w", err)
				}
				// Update branch in state only after successful prepare
				w.Branch = branch
				if err := m.state.UpdateWorkspace(w); err != nil {
					return nil, fmt.Errorf("failed to update workspace in state: %w", err)
				}
				return &w, nil
			}
		}
	}

	// Create a new workspace
	w, err := m.create(ctx, repoURL, branch)
	if err != nil {
		return nil, err
	}
	fmt.Printf("[workspace] created: id=%s path=%s branch=%s repo=%s\n", w.ID, w.Path, w.Branch, repoURL)

	// Prepare the workspace
	if err := m.prepare(ctx, w.ID, w.Branch); err != nil {
		return nil, fmt.Errorf("failed to prepare workspace: %w", err)
	}

	return w, nil
}

// getOrCreateRemoteWorkspace handles workspace creation for remote repos.
// These repos use externally-managed VCS (e.g., sapling) and remote execution environments.
// No git clone or worktree is created - we just register a workspace pointing to the configured path.
// Provisioning happens here before returning, so the workspace is ready for sessions.
func (m *Manager) getOrCreateRemoteWorkspace(ctx context.Context, repo config.Repo, branch string) (*state.Workspace, error) {
	if repo.Remote == nil {
		return nil, fmt.Errorf("repo %s is marked as remote but has no remote config", repo.Name)
	}

	// For remote workspaces, keep the path as-is (including ~).
	// The ~ should expand on the remote system, not locally.
	workspacePath := repo.Remote.WorkspacePath

	lock := m.repoLock(repo.URL)
	lock.Lock()
	defer lock.Unlock()

	// For remote repos, look for existing workspace with matching repo AND branch
	// Different branches require different ODs (separate workspaces)
	for _, w := range m.state.GetWorkspaces() {
		if w.Repo == repo.URL && w.External && w.Branch == branch {
			// Reuse existing external workspace with same branch
			fmt.Printf("[workspace] reusing remote workspace: id=%s path=%s branch=%s\n", w.ID, w.Path, w.Branch)

			// If already ready, return directly
			if w.IsReady() {
				return &w, nil
			}

			// If not ready, provision (or wait for provisioning)
			if err := m.ProvisionRemote(ctx, w.ID, repo); err != nil {
				return nil, fmt.Errorf("failed to provision remote workspace: %w", err)
			}

			// Re-fetch the workspace to get updated status
			freshW, found := m.state.GetWorkspace(w.ID)
			if !found {
				return nil, fmt.Errorf("workspace not found after provisioning: %s", w.ID)
			}
			return &freshW, nil
		}
	}

	// Create new external workspace for this branch
	// Find the next available workspace number for this repo
	nextNum := 1
	for _, w := range m.state.GetWorkspaces() {
		if w.Repo == repo.URL && w.External {
			// Parse the existing workspace ID to find the highest number
			// Format: {repo-name}-{number}
			parts := strings.Split(w.ID, "-")
			if len(parts) > 0 {
				if num, err := strconv.Atoi(parts[len(parts)-1]); err == nil && num >= nextNum {
					nextNum = num + 1
				}
			}
		}
	}

	workspaceID := fmt.Sprintf("%s-"+workspaceNumberFormat, repo.Name, nextNum)

	w := state.Workspace{
		ID:       workspaceID,
		Repo:     repo.URL,
		Branch:   branch, // May not be used for remote
		Path:     workspacePath,
		External: true,
		VCSType:  m.config.GetRemoteRunnerVCSType(),
		Status:   state.WorkspaceStatusPending,
	}

	if err := m.state.AddWorkspace(w); err != nil {
		return nil, fmt.Errorf("failed to add workspace to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	fmt.Printf("[workspace] created remote workspace: id=%s path=%s repo=%s\n", w.ID, w.Path, repo.URL)

	// Provision the remote workspace
	if err := m.ProvisionRemote(ctx, w.ID, repo); err != nil {
		return nil, fmt.Errorf("failed to provision remote workspace: %w", err)
	}

	// Re-fetch the workspace to get updated status
	freshW, found := m.state.GetWorkspace(w.ID)
	if !found {
		return nil, fmt.Errorf("workspace not found after provisioning: %s", w.ID)
	}
	return &freshW, nil
}

// create creates a new workspace directory for the given repoURL using git worktrees.
func (m *Manager) create(ctx context.Context, repoURL, branch string) (*state.Workspace, error) {
	// Find repo config by URL
	repoConfig, found := m.findRepoByURL(repoURL)
	if !found {
		return nil, fmt.Errorf("repo URL not found in config: %s", repoURL)
	}

	// Find the next available workspace number
	workspaces := m.getWorkspacesForRepo(repoURL)
	nextNum := findNextWorkspaceNumber(workspaces)

	// Create workspace ID
	workspaceID := fmt.Sprintf("%s-"+workspaceNumberFormat, repoConfig.Name, nextNum)

	// Create full path
	workspacePath := filepath.Join(m.config.GetWorkspacePath(), workspaceID)

	// Ensure base repo exists (creates bare clone if needed)
	worktreeBasePath, err := m.ensureWorktreeBase(ctx, repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure worktree base: %w", err)
	}

	// Fetch latest before creating worktree
	if fetchErr := m.gitFetch(ctx, worktreeBasePath); fetchErr != nil {
		fmt.Printf("[workspace] warning: fetch failed before worktree add: %v\n", fetchErr)
	}

	createdUniqueBranch := false
	if m.config.UseWorktrees() {
		uniqueBranch, wasCreated, err := m.ensureUniqueBranch(ctx, worktreeBasePath, branch)
		if err != nil {
			return nil, fmt.Errorf("failed to pick unique branch: %w", err)
		}
		if uniqueBranch != branch {
			fmt.Printf("[workspace] using unique branch: requested=%s actual=%s\n", branch, uniqueBranch)
		}
		branch = uniqueBranch
		createdUniqueBranch = wasCreated
	}

	// Clean up worktree if creation fails
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			fmt.Printf("[workspace] cleaning up failed: %s\n", workspacePath)
			// Try worktree remove first, fall back to rm -rf
			if err := m.removeWorktree(ctx, worktreeBasePath, workspacePath); err != nil {
				os.RemoveAll(workspacePath)
			}
			if createdUniqueBranch {
				if err := m.deleteBranch(ctx, worktreeBasePath, branch); err != nil {
					fmt.Printf("[workspace] warning: failed to delete branch %s: %v\n", branch, err)
				}
			}
		}
	}()

	// Check source code management setting
	if m.config.UseWorktrees() {
		// Using worktrees - no fallback, branch conflicts are auto-resolved with suffixes
		if err := m.addWorktree(ctx, worktreeBasePath, workspacePath, branch, repoURL); err != nil {
			return nil, fmt.Errorf("failed to add worktree: %w", err)
		}
	} else {
		// Using full clones
		fmt.Printf("[workspace] source_code_manager=git, using full clone\n")
		if err := m.cloneRepo(ctx, repoURL, workspacePath); err != nil {
			return nil, fmt.Errorf("failed to clone repo: %w", err)
		}
	}

	// Copy overlay files if they exist
	if err := m.copyOverlayFiles(ctx, repoConfig.Name, workspacePath); err != nil {
		fmt.Printf("[workspace] warning: failed to copy overlay files: %v\n", err)
		// Don't fail workspace creation if overlay copy fails
	}

	// Create workspace state with branch
	w := state.Workspace{
		ID:      workspaceID,
		Repo:    repoURL,
		Branch:  branch,
		Path:    workspacePath,
		VCSType: "git",
	}

	if err := m.state.AddWorkspace(w); err != nil {
		return nil, fmt.Errorf("failed to add workspace to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	// State is persisted, workspace is valid
	cleanupNeeded = false

	// Add filesystem watches for git metadata
	if m.gitWatcher != nil {
		m.gitWatcher.AddWorkspace(w.ID, w.Path)
	}

	return &w, nil
}

// CreateLocalRepo creates a new workspace with a fresh local git repository.
// The repoName parameter is used to create the workspace ID (e.g., "myproject-001").
// A new git repository is initialized with the specified branch and an initial empty commit.
func (m *Manager) CreateLocalRepo(ctx context.Context, repoName, branch string) (*state.Workspace, error) {
	// Validate repo name (should be a valid directory name)
	if repoName == "" {
		return nil, fmt.Errorf("repo name is required")
	}
	// Basic sanitization - prevent directory traversal
	if strings.Contains(repoName, "..") || strings.Contains(repoName, "/") || strings.Contains(repoName, "\\") {
		return nil, fmt.Errorf("invalid repo name: %s", repoName)
	}

	// Construct the repo URL for state (local:{name})
	repoURL := fmt.Sprintf("local:%s", repoName)

	// Find the next available workspace number for this "local repo"
	workspaces := m.getWorkspacesForRepo(repoURL)
	nextNum := findNextWorkspaceNumber(workspaces)

	// Create workspace ID
	workspaceID := fmt.Sprintf("%s-"+workspaceNumberFormat, repoName, nextNum)

	// Create full path
	workspacePath := filepath.Join(m.config.GetWorkspacePath(), workspaceID)

	// Clean up directory if creation fails (registered before any directory creation)
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			fmt.Printf("[workspace] cleaning up failed local repo: %s\n", workspacePath)
			if err := os.RemoveAll(workspacePath); err != nil {
				fmt.Printf("[workspace] failed to cleanup local repo %s: %v\n", workspacePath, err)
			}
		}
	}()

	// Create the directory and initialize a local git repository
	if err := m.initLocalRepo(ctx, workspacePath, branch); err != nil {
		return nil, fmt.Errorf("failed to initialize local repo: %w", err)
	}

	fmt.Printf("[workspace] created local repo: id=%s path=%s branch=%s\n", workspaceID, workspacePath, branch)

	// Create workspace state
	w := state.Workspace{
		ID:      workspaceID,
		Repo:    repoURL,
		Branch:  branch,
		Path:    workspacePath,
		VCSType: "git",
	}

	if err := m.state.AddWorkspace(w); err != nil {
		return nil, fmt.Errorf("failed to add workspace to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	// State is persisted, workspace is valid even if config update fails
	cleanupNeeded = false

	// Add the new local repository to config so it appears in the spawn wizard dropdown
	m.config.Repos = append(m.config.Repos, config.Repo{
		Name: repoName,
		URL:  repoURL,
	})
	if err := m.config.Save(); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	return &w, nil
}

// prepare prepares a workspace for use (git checkout, pull, clean).
func (m *Manager) prepare(ctx context.Context, workspaceID, branch string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Check if workspace has active sessions
	if m.hasActiveSessions(workspaceID) {
		return fmt.Errorf("workspace has active sessions: %s", workspaceID)
	}

	fmt.Printf("[workspace] preparing: id=%s branch=%s\n", workspaceID, branch)

	hasOrigin := m.gitHasOriginRemote(ctx, w.Path)
	if hasOrigin {
		// Fetch latest
		if err := m.gitFetch(ctx, w.Path); err != nil {
			return fmt.Errorf("git fetch failed: %w", err)
		}
	} else {
		fmt.Printf("[workspace] no origin remote, skipping fetch\n")
	}

	remoteBranchExists := false
	if hasOrigin {
		var err error
		remoteBranchExists, err = m.gitRemoteBranchExists(ctx, w.Path, branch)
		if err != nil {
			return fmt.Errorf("git remote branch check failed: %w", err)
		}
	}

	// Discard any local changes (must happen before pull)
	if err := m.gitCheckoutDot(ctx, w.Path); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w", err)
	}

	// Clean untracked files and directories (must happen before pull)
	if err := m.gitClean(ctx, w.Path); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	// Checkout/reset branch after cleaning
	if err := m.gitCheckoutBranch(ctx, w.Path, branch, remoteBranchExists); err != nil {
		return fmt.Errorf("git checkout failed: %w", err)
	}

	// Pull with rebase (working dir is now clean)
	if remoteBranchExists {
		if err := m.gitPullRebase(ctx, w.Path, branch); err != nil {
			return fmt.Errorf("git pull --rebase failed (conflicts?): %w", err)
		}
	} else {
		fmt.Printf("[workspace] no origin/%s remote ref, skipping pull\n", branch)
	}

	fmt.Printf("[workspace] prepared: id=%s branch=%s\n", workspaceID, branch)
	return nil
}

// Cleanup cleans up a workspace by resetting git state.
func (m *Manager) Cleanup(ctx context.Context, workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	fmt.Printf("[workspace] cleaning up: id=%s path=%s\n", workspaceID, w.Path)

	// Reset all changes
	if err := m.gitCheckoutDot(ctx, w.Path); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w", err)
	}

	// Clean untracked files
	if err := m.gitClean(ctx, w.Path); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	fmt.Printf("[workspace] cleaned: id=%s\n", workspaceID)
	return nil
}

// getWorkspacesForRepo returns all workspaces for a given repoURL.
func (m *Manager) getWorkspacesForRepo(repoURL string) []state.Workspace {
	var result []state.Workspace
	for _, w := range m.state.GetWorkspaces() {
		if w.Repo == repoURL {
			result = append(result, w)
		}
	}
	return result
}

// findRepoByURL finds a repo config by URL.
func (m *Manager) findRepoByURL(repoURL string) (config.Repo, bool) {
	for _, repo := range m.config.GetRepos() {
		if repo.URL == repoURL {
			return repo, true
		}
	}
	return config.Repo{}, false
}

// findNextWorkspaceNumber finds the next available workspace number, filling gaps.
// It starts from 1 and returns the first unused number.
func findNextWorkspaceNumber(workspaces []state.Workspace) int {
	// Track which numbers are used
	used := make(map[int]bool)
	for _, w := range workspaces {
		num, err := extractWorkspaceNumber(w.ID)
		if err == nil {
			used[num] = true
		}
	}

	// Find first unused number starting from 1
	nextNum := 1
	for used[nextNum] {
		nextNum++
	}
	return nextNum
}

// extractWorkspaceNumber extracts the numeric suffix from a workspace ID.
func extractWorkspaceNumber(id string) (int, error) {
	parts := strings.Split(id, "-")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid workspace ID format: %s", id)
	}

	numStr := parts[len(parts)-1]
	return strconv.Atoi(numStr)
}

// UpdateGitStatus refreshes the git status for a single workspace.
// Returns the updated workspace or an error.
// For external (remote) workspaces, git status is not applicable.
func (m *Manager) UpdateGitStatus(ctx context.Context, workspaceID string) (*state.Workspace, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Skip git operations for external (remote) workspaces
	// They use external VCS like Sapling, not git
	if w.External {
		return &w, nil
	}

	// Calculate git status (safe to run even with active sessions)
	dirty, ahead, behind, linesAdded, linesRemoved, filesChanged := m.gitStatus(ctx, w.Path, w.Repo)

	// Detect actual current branch (may differ from state if user manually switched)
	actualBranch, err := m.gitCurrentBranch(ctx, w.Path)
	if err != nil {
		fmt.Printf("[workspace] failed to get current branch for %s: %v\n", w.ID, err)
		actualBranch = w.Branch // fallback to existing state
	}

	// Update workspace in memory
	w.GitDirty = dirty
	w.GitAhead = ahead
	w.GitBehind = behind
	w.GitLinesAdded = linesAdded
	w.GitLinesRemoved = linesRemoved
	w.GitFilesChanged = filesChanged
	w.Branch = actualBranch

	// Update the workspace in state (this updates the in-memory copy)
	if err := m.state.UpdateWorkspace(w); err != nil {
		return nil, fmt.Errorf("failed to update workspace in state: %w", err)
	}

	return &w, nil
}

// UpdateAllGitStatus refreshes git status for all workspaces.
// This is called periodically by the background goroutine.
func (m *Manager) UpdateAllGitStatus(ctx context.Context) {
	workspaces := m.state.GetWorkspaces()

	for _, w := range workspaces {
		// Refresh workspace config for this workspace
		m.RefreshWorkspaceConfig(w)

		if _, err := m.UpdateGitStatus(ctx, w.ID); err != nil {
			fmt.Printf("[workspace] failed to update git status for %s: %v\n", w.ID, err)
		}
	}
}

// EnsureWorkspaceDir ensures the workspace base directory exists.
func (m *Manager) EnsureWorkspaceDir() error {
	path := m.config.GetWorkspacePath()
	// Skip if workspace_path is empty (during wizard setup)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create workspace directory: %w", err)
	}
	return nil
}

// Dispose deletes a workspace by removing its directory and removing it from state.
func (m *Manager) Dispose(workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	fmt.Printf("[workspace] disposing: id=%s path=%s\n", workspaceID, w.Path)

	// Check if workspace has active sessions
	if m.hasActiveSessions(workspaceID) {
		return fmt.Errorf("workspace has active sessions: %s", workspaceID)
	}

	// For external workspaces (remote), skip git checks and directory operations.
	// The workspace path points to a remote location - we just remove from state.
	if w.External {
		fmt.Printf("[workspace] external workspace, skipping git/filesystem cleanup\n")
		if err := m.state.RemoveWorkspace(workspaceID); err != nil {
			return fmt.Errorf("failed to remove workspace from state: %w", err)
		}
		if err := m.state.Save(); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}
		fmt.Printf("[workspace] disposed: id=%s\n", workspaceID)
		return nil
	}

	ctx := context.Background()

	// Check if workspace directory exists
	dirExists := true
	if _, err := os.Stat(w.Path); os.IsNotExist(err) {
		dirExists = false
		fmt.Printf("[workspace] directory already deleted: %s\n", w.Path)
	}

	// Check git safety - only if directory exists
	if dirExists {
		gitStatus, err := m.checkGitSafety(ctx, workspaceID)
		if err != nil {
			return fmt.Errorf("failed to check git status: %w", err)
		}
		if !gitStatus.Safe {
			return fmt.Errorf("workspace has unsaved changes: %s", gitStatus.Reason)
		}
	}

	// Remove filesystem watches before directory removal
	if m.gitWatcher != nil {
		m.gitWatcher.RemoveWorkspace(workspaceID)
	}

	// Find base repo for worktree cleanup (works even if directory is gone)
	worktreeBasePath, worktreeBaseErr := m.findWorktreeBaseForWorkspace(w)

	// Delete workspace directory (worktree or legacy full clone)
	if dirExists {
		if isWorktree(w.Path) {
			// Use git worktree remove for worktrees
			if worktreeBaseErr != nil {
				fmt.Printf("[workspace] warning: could not find worktree base, falling back to rm: %v\n", worktreeBaseErr)
				if err := os.RemoveAll(w.Path); err != nil {
					return fmt.Errorf("failed to delete workspace directory: %w", err)
				}
			} else {
				if err := m.removeWorktree(ctx, worktreeBasePath, w.Path); err != nil {
					return fmt.Errorf("failed to remove worktree: %w", err)
				}
			}
		} else {
			// Legacy full clone - delete directory
			if err := os.RemoveAll(w.Path); err != nil {
				return fmt.Errorf("failed to delete workspace directory: %w", err)
			}
		}
	}

	// Prune stale worktree references (handles case where directory was already deleted)
	if worktreeBaseErr == nil {
		if err := m.pruneWorktrees(ctx, worktreeBasePath); err != nil {
			fmt.Printf("[workspace] warning: failed to prune worktrees: %v\n", err)
		}
	}

	// Remove from state
	if err := m.state.RemoveWorkspace(workspaceID); err != nil {
		return fmt.Errorf("failed to remove workspace from state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	if err := difftool.CleanupWorkspaceTempDirs(workspaceID); err != nil {
		fmt.Printf("[workspace] failed to cleanup diff temp dirs for %s: %v\n", workspaceID, err)
	}

	fmt.Printf("[workspace] disposed: id=%s\n", workspaceID)
	return nil
}

// getProvisionLock returns a mutex for the given workspace ID.
// This is used to serialize remote provisioning so that parallel spawns
// share the same remote connection instead of each provisioning separately.
func (m *Manager) getProvisionLock(workspaceID string) *sync.Mutex {
	m.provisionLockMu.Lock()
	defer m.provisionLockMu.Unlock()

	lock, ok := m.provisionLocks[workspaceID]
	if !ok {
		lock = &sync.Mutex{}
		m.provisionLocks[workspaceID] = lock
	}
	return lock
}

// ProvisionRemote provisions a remote workspace connection.
// Blocks until provisioning completes or fails.
// Safe to call concurrently - only one caller provisions, others wait.
func (m *Manager) ProvisionRemote(ctx context.Context, workspaceID string, repo config.Repo) error {
	// Acquire lock briefly to check state
	provisionLock := m.getProvisionLock(workspaceID)
	provisionLock.Lock()

	// Re-fetch workspace from state
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		provisionLock.Unlock()
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	fmt.Printf("[workspace] ProvisionRemote: workspace=%s LocalTmuxSession=%q Status=%s\n",
		w.ID, w.LocalTmuxSession, w.Status)

	// Case 1: Connection is ready
	if w.Status == state.WorkspaceStatusReady && w.LocalTmuxSession != "" && m.runner.SessionExists(ctx, w.LocalTmuxSession) {
		provisionLock.Unlock()
		return nil
	}

	// Case 2: Another goroutine is currently provisioning - wait for it
	if w.Status == state.WorkspaceStatusProvisioning {
		provisionLock.Unlock()
		fmt.Printf("[workspace] === WAITING FOR PROVISIONING ===\n")
		fmt.Printf("[workspace] Another goroutine is provisioning, waiting...\n")

		// Poll until provisioning completes
		if err := m.waitForProvisioning(ctx, workspaceID, 90*time.Second); err != nil {
			return fmt.Errorf("failed waiting for provisioning: %w", err)
		}

		return nil
	}

	// Case 3: First caller - start provisioning
	fmt.Printf("[workspace] === FIRST REMOTE PROVISIONING ===\n")

	// Mark as provisioning and release lock
	w.Status = state.WorkspaceStatusProvisioning
	w.StatusMessage = ""
	if err := m.state.UpdateWorkspace(w); err != nil {
		provisionLock.Unlock()
		return fmt.Errorf("failed to set provisioning status: %w", err)
	}
	m.state.Save()
	provisionLock.Unlock()

	// Do the actual provisioning
	if err := m.doProvision(ctx, workspaceID, repo); err != nil {
		// Mark as failed
		m.setWorkspaceStatus(workspaceID, state.WorkspaceStatusFailed, err.Error())
		return err
	}

	return nil
}

// doProvision does the actual provisioning work for a remote workspace.
// This includes creating the local tmux session that connects to the remote.
func (m *Manager) doProvision(ctx context.Context, workspaceID string, repo config.Repo) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Get the remote runner config
	runnerCfg := m.config.GetRemoteRunner()
	if runnerCfg == nil || runnerCfg.ProvisionPrefix == "" {
		return fmt.Errorf("remote_runner not configured (provision_prefix is required)")
	}

	// Get flavor from repo config
	flavor := ""
	if repo.Remote != nil {
		flavor = repo.Remote.Flavor
	}

	// Create the shared local tmux session name for this workspace
	sharedLocalTmux := fmt.Sprintf("schmux-%s", workspaceID)
	remoteTmuxSession := "schmux"
	workspacePath := w.Path

	// Build the provisioning bash script
	// This creates a remote tmux session with a helper window
	nestedTmuxSetup := fmt.Sprintf(
		"tmux new-session -d -s %s -n helper -c %s; "+
			"exec tmux attach -t %s:helper",
		remoteTmuxSession, workspacePath,
		remoteTmuxSession,
	)

	// Substitute flavor into provision prefix
	provisionPrefix := strings.ReplaceAll(runnerCfg.ProvisionPrefix, "{{.Flavor}}", flavor)
	fullCmd := fmt.Sprintf("%s -- bash -c %s", provisionPrefix, shellQuote(nestedTmuxSetup))

	fmt.Printf("[workspace] Flavor: %s\n", flavor)
	fmt.Printf("[workspace] Remote workspace path: %s\n", w.Path)
	fmt.Printf("[workspace] Local tmux session: %s\n", sharedLocalTmux)
	fmt.Printf("[workspace] Command:\n")
	fmt.Printf("[workspace]   %s\n", fullCmd)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "/"
	}

	// Create the shared local tmux session
	if err := m.runner.CreateSession(ctx, runner.CreateSessionOpts{
		SessionID: sharedLocalTmux,
		WorkDir:   homeDir,
		Command:   fullCmd,
	}); err != nil {
		return fmt.Errorf("failed to create local tmux session: %w", err)
	}

	// Wait for the remote tmux to be ready (lock NOT held during this wait)
	fmt.Printf("[workspace] Waiting for remote tmux to be ready...\n")
	if err := m.waitForRemoteTmuxReady(ctx, sharedLocalTmux, 60*time.Second); err != nil {
		m.runner.KillSession(ctx, sharedLocalTmux)
		return fmt.Errorf("remote tmux not ready: %w", err)
	}
	fmt.Printf("[workspace] Remote tmux is ready\n")

	// Acquire lock to update state
	provisionLock := m.getProvisionLock(workspaceID)
	provisionLock.Lock()
	defer provisionLock.Unlock()

	// Re-fetch workspace
	w, found = m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	w.LocalTmuxSession = sharedLocalTmux
	w.Status = state.WorkspaceStatusReady
	w.StatusMessage = ""
	if err := m.state.UpdateWorkspace(w); err != nil {
		return fmt.Errorf("failed to store LocalTmuxSession: %w", err)
	}
	m.state.Save()

	fmt.Printf("[workspace] Provisioning complete: LocalTmuxSession=%s\n", sharedLocalTmux)
	fmt.Printf("[workspace] =========================\n")

	// Start background hostname detection
	if w.RemoteHost == "" {
		go m.detectAndStoreHostname(workspaceID, sharedLocalTmux)
	}

	return nil
}

// waitForProvisioning polls until the workspace's Status is no longer provisioning.
func (m *Manager) waitForProvisioning(ctx context.Context, workspaceID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	pollInterval := 2 * time.Second

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		ws, found := m.state.GetWorkspace(workspaceID)
		if !found {
			return fmt.Errorf("workspace not found: %s", workspaceID)
		}

		// Check if provisioning is complete
		if ws.Status == state.WorkspaceStatusReady && ws.LocalTmuxSession != "" {
			fmt.Printf("[workspace] Provisioning complete, LocalTmuxSession=%s\n", ws.LocalTmuxSession)
			return nil
		}

		// Check if provisioning failed
		if ws.Status == state.WorkspaceStatusFailed {
			return fmt.Errorf("provisioning failed: %s", ws.StatusMessage)
		}

		// If status changed to something else unexpected, also check
		if ws.Status != state.WorkspaceStatusProvisioning {
			return fmt.Errorf("unexpected workspace status: %s", ws.Status)
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("timeout waiting for provisioning to complete")
}

// waitForRemoteTmuxReady waits for the remote tmux session to be accessible.
// It polls by checking if the local tmux session shows signs of being attached
// to the remote tmux (looking for tmux-related output in the pane).
func (m *Manager) waitForRemoteTmuxReady(ctx context.Context, localTmux string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	pollInterval := 2 * time.Second

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Check if the local tmux session still exists
		if !m.runner.SessionExists(ctx, localTmux) {
			return fmt.Errorf("local tmux session died")
		}

		// Capture the pane output to see if we're attached to the remote tmux
		output, err := m.runner.CaptureOutput(ctx, localTmux)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}

		// Look for signs that we're in a tmux session on the remote
		// When attached to remote tmux, we typically see the agent running
		// or at least a shell prompt. We check for common indicators.
		if strings.Contains(output, "claude") ||
			strings.Contains(output, "codex") ||
			strings.Contains(output, "$") ||
			strings.Contains(output, "#") ||
			strings.Contains(output, "~") ||
			len(strings.TrimSpace(output)) > 50 {
			// Looks like we're connected and have output
			return nil
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("timeout waiting for remote tmux")
}

// detectAndStoreHostname detects the hostname from a session's log file and stores it in the workspace.
// This is called automatically after provisioning a remote workspace.
func (m *Manager) detectAndStoreHostname(workspaceID, localTmux string) {
	hostnameRegex := m.config.GetRemoteRunnerHostnameRegex()
	if hostnameRegex == "" {
		fmt.Printf("[workspace] no hostname_regex configured, skipping hostname detection\n")
		return
	}

	// Poll for hostname in the captured output for up to 60 seconds
	maxWait := 60 * time.Second
	pollInterval := 2 * time.Second
	deadline := time.Now().Add(maxWait)

	fmt.Printf("[workspace] starting hostname detection for workspace %s\n", workspaceID)

	ctx := context.Background()

	for time.Now().Before(deadline) {
		// Check if workspace already has hostname (might be set by another path)
		ws, found := m.state.GetWorkspace(workspaceID)
		if !found {
			fmt.Printf("[workspace] workspace %s not found, stopping hostname detection\n", workspaceID)
			return
		}
		if ws.RemoteHost != "" {
			fmt.Printf("[workspace] hostname already set for workspace %s: %s\n", workspaceID, ws.RemoteHost)
			return
		}

		// Capture the output from the local tmux session
		output, err := m.runner.CaptureOutput(ctx, localTmux)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}

		if len(output) == 0 {
			time.Sleep(pollInterval)
			continue
		}

		// Strip ANSI escape codes before matching
		ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07`)
		output = ansiRegex.ReplaceAllString(output, "")

		// Try matching with multiline flag if regex uses ^ anchor
		regexToTry := hostnameRegex
		if strings.HasPrefix(regexToTry, "^") && !strings.HasPrefix(regexToTry, "(?m)") {
			regexToTry = "(?m)" + regexToTry
		}

		re, err := regexp.Compile(regexToTry)
		if err != nil {
			fmt.Printf("[workspace] invalid hostname regex: %v\n", err)
			return
		}

		matches := re.FindStringSubmatch(output)
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
				matches := re.FindStringSubmatch(output)
				if len(matches) > 1 {
					hostname = matches[1]
				} else if len(matches) > 0 {
					hostname = matches[0]
				}
			}
		}

		if hostname != "" {
			fmt.Printf("[workspace] detected hostname: %s\n", hostname)
			if err := m.UpdateWorkspaceRemoteHost(workspaceID, hostname); err != nil {
				fmt.Printf("[workspace] failed to store hostname: %v\n", err)
			}
			return
		}

		time.Sleep(pollInterval)
	}

	fmt.Printf("[workspace] hostname detection timed out for workspace %s\n", workspaceID)
}

// setWorkspaceStatus updates the status and status message for a workspace.
func (m *Manager) setWorkspaceStatus(workspaceID string, status state.WorkspaceStatus, message string) {
	provisionLock := m.getProvisionLock(workspaceID)
	provisionLock.Lock()
	defer provisionLock.Unlock()

	ws, found := m.state.GetWorkspace(workspaceID)
	if found {
		ws.Status = status
		ws.StatusMessage = message
		m.state.UpdateWorkspace(ws)
		m.state.Save()
	}
}

// UpdateWorkspaceRemoteHost updates the RemoteHost for a workspace.
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

	fmt.Printf("[workspace] stored remote host for workspace %s: %s\n", workspaceID, remoteHost)
	return nil
}

// shellQuote quotes a string for safe use in shell commands using single quotes.
// Single quotes preserve everything literally, including newlines.
// Embedded single quotes are handled with the '\" trick.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
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
	ws.Status = state.WorkspaceStatusDisconnected
	if err := m.state.UpdateWorkspace(ws); err != nil {
		return fmt.Errorf("failed to update workspace: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	fmt.Printf("[workspace] cleared remote host and local tmux session for workspace %s\n", workspaceID)
	return nil
}

// GetRunner returns the local tmux runner for the workspace manager.
// This is used by the session manager to check if local tmux sessions exist.
func (m *Manager) GetRunner() runner.SessionRunner {
	return m.runner
}
