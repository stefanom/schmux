package vcs

import "context"

// ExternalVCS implements VersionControl with no-op operations.
// Use this when version control is managed externally (not by Schmux).
type ExternalVCS struct{}

// NewExternalVCS creates a new ExternalVCS instance.
func NewExternalVCS() *ExternalVCS {
	return &ExternalVCS{}
}

// Clone is a no-op for external VCS.
func (e *ExternalVCS) Clone(ctx context.Context, url, destPath string) error {
	return nil // No-op: workspace is pre-provisioned
}

// CloneBare is a no-op for external VCS.
func (e *ExternalVCS) CloneBare(ctx context.Context, url, destPath string) error {
	return nil // No-op
}

// Fetch is a no-op for external VCS.
func (e *ExternalVCS) Fetch(ctx context.Context, repoPath string) error {
	return nil // No-op
}

// Checkout is a no-op for external VCS.
func (e *ExternalVCS) Checkout(ctx context.Context, repoPath, branch string, resetToOrigin bool) error {
	return nil // No-op
}

// Pull is a no-op for external VCS.
func (e *ExternalVCS) Pull(ctx context.Context, repoPath, branch string) error {
	return nil // No-op
}

// GetCurrentBranch returns "unknown" for external VCS.
func (e *ExternalVCS) GetCurrentBranch(ctx context.Context, repoPath string) (string, error) {
	return "unknown", nil // Can't determine without VCS
}

// GetDefaultBranch returns "main" for external VCS.
func (e *ExternalVCS) GetDefaultBranch(ctx context.Context, repoPath, repoURL string) (string, error) {
	return "main", nil // Default assumption
}

// HasOriginRemote returns false for external VCS.
func (e *ExternalVCS) HasOriginRemote(ctx context.Context, repoPath string) bool {
	return false
}

// RemoteBranchExists returns false for external VCS.
func (e *ExternalVCS) RemoteBranchExists(ctx context.Context, repoPath, branch string) (bool, error) {
	return false, nil
}

// DiscardChanges is a no-op for external VCS.
func (e *ExternalVCS) DiscardChanges(ctx context.Context, repoPath string) error {
	return nil // No-op
}

// CleanUntracked is a no-op for external VCS.
func (e *ExternalVCS) CleanUntracked(ctx context.Context, repoPath string) error {
	return nil // No-op
}

// GetStatus returns a clean status for external VCS.
func (e *ExternalVCS) GetStatus(ctx context.Context, repoPath, repoURL string, getDefaultBranch func(ctx context.Context, repoURL string) (string, error)) Status {
	return Status{} // Clean status
}

// CheckSafety returns safe for external VCS.
func (e *ExternalVCS) CheckSafety(ctx context.Context, repoPath string) (*SafetyStatus, error) {
	return &SafetyStatus{Safe: true}, nil // Always safe (user manages VCS)
}

// AddWorktree is a no-op for external VCS.
func (e *ExternalVCS) AddWorktree(ctx context.Context, basePath, worktreePath, branch, repoURL string) error {
	return nil // No-op
}

// RemoveWorktree is a no-op for external VCS.
func (e *ExternalVCS) RemoveWorktree(ctx context.Context, basePath, worktreePath string) error {
	return nil // No-op
}

// PruneWorktrees is a no-op for external VCS.
func (e *ExternalVCS) PruneWorktrees(ctx context.Context, basePath string) error {
	return nil // No-op
}

// InitLocalRepo is a no-op for external VCS.
func (e *ExternalVCS) InitLocalRepo(ctx context.Context, path, branch string) error {
	return nil // No-op
}

// IsManaged returns false - external VCS is not managed by Schmux.
func (e *ExternalVCS) IsManaged() bool {
	return false
}
