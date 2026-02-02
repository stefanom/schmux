// Package vcs provides version control abstractions.
// Implementations include GitVCS (Git CLI) and ExternalVCS (no-op for
// externally managed version control).
package vcs

import "context"

// VersionControl abstracts source control operations.
// Implementations: GitVCS (wraps git CLI), ExternalVCS (no-op for externally managed)
type VersionControl interface {
	// Clone clones a repository to the destination path.
	Clone(ctx context.Context, url, destPath string) error

	// CloneBare clones a repository as a bare clone.
	CloneBare(ctx context.Context, url, destPath string) error

	// Fetch fetches updates from the remote.
	Fetch(ctx context.Context, repoPath string) error

	// Checkout switches to a branch.
	Checkout(ctx context.Context, repoPath, branch string, resetToOrigin bool) error

	// Pull pulls updates with rebase.
	Pull(ctx context.Context, repoPath, branch string) error

	// GetCurrentBranch returns the current branch name.
	GetCurrentBranch(ctx context.Context, repoPath string) (string, error)

	// GetDefaultBranch returns the default branch name for a remote URL.
	GetDefaultBranch(ctx context.Context, repoPath, repoURL string) (string, error)

	// HasOriginRemote checks if the repo has an origin remote.
	HasOriginRemote(ctx context.Context, repoPath string) bool

	// RemoteBranchExists checks if a remote branch exists.
	RemoteBranchExists(ctx context.Context, repoPath, branch string) (bool, error)

	// DiscardChanges discards all local changes (git checkout -- .).
	DiscardChanges(ctx context.Context, repoPath string) error

	// CleanUntracked removes untracked files (git clean -fd).
	CleanUntracked(ctx context.Context, repoPath string) error

	// GetStatus returns the git status for a workspace.
	GetStatus(ctx context.Context, repoPath, repoURL string, getDefaultBranch func(ctx context.Context, repoURL string) (string, error)) Status

	// CheckSafety checks if a workspace is safe to dispose.
	CheckSafety(ctx context.Context, repoPath string) (*SafetyStatus, error)

	// AddWorktree adds a git worktree.
	AddWorktree(ctx context.Context, basePath, worktreePath, branch, repoURL string) error

	// RemoveWorktree removes a git worktree.
	RemoveWorktree(ctx context.Context, basePath, worktreePath string) error

	// PruneWorktrees prunes stale worktree references.
	PruneWorktrees(ctx context.Context, basePath string) error

	// InitLocalRepo initializes a local repo (no remote).
	InitLocalRepo(ctx context.Context, path, branch string) error

	// IsManaged returns true if VCS is managed by Schmux.
	// Returns false for ExternalVCS where VCS is managed externally.
	IsManaged() bool
}

// Status represents the git status of a workspace.
type Status struct {
	Dirty        bool
	Ahead        int
	Behind       int
	LinesAdded   int
	LinesRemoved int
	FilesChanged int
}

// SafetyStatus represents whether a workspace is safe to dispose.
type SafetyStatus struct {
	Safe           bool
	Reason         string
	ModifiedFiles  int
	UntrackedFiles int
	AheadCommits   int
}
