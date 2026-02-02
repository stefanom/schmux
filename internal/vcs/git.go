package vcs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sergeknystautas/schmux/internal/difftool"
)

// GitVCS implements VersionControl using the git CLI.
type GitVCS struct{}

// NewGitVCS creates a new GitVCS instance.
func NewGitVCS() *GitVCS {
	return &GitVCS{}
}

// Clone clones a repository to the destination path.
func (g *GitVCS) Clone(ctx context.Context, url, destPath string) error {
	fmt.Printf("[vcs] cloning repository: url=%s path=%s\n", url, destPath)
	args := []string{"clone", url, destPath}
	cmd := exec.CommandContext(ctx, "git", args...)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, string(output))
	}

	fmt.Printf("[vcs] repository cloned: path=%s\n", destPath)
	return nil
}

// CloneBare clones a repository as a bare clone and configures fetch refspecs.
func (g *GitVCS) CloneBare(ctx context.Context, url, destPath string) error {
	fmt.Printf("[vcs] cloning bare repository: url=%s path=%s\n", url, destPath)
	args := []string{"clone", "--bare", url, destPath}
	cmd := exec.CommandContext(ctx, "git", args...)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone --bare failed: %w: %s", err, string(output))
	}

	// Configure fetch refspec so 'git fetch' creates remote tracking branches
	configCmd := exec.CommandContext(ctx, "git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	configCmd.Dir = destPath
	if output, err := configCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config fetch refspec failed: %w: %s", err, string(output))
	}

	fmt.Printf("[vcs] bare repository cloned: path=%s\n", destPath)
	return nil
}

// Fetch runs git fetch. For worktrees, fetches from the worktree base.
func (g *GitVCS) Fetch(ctx context.Context, repoPath string) error {
	// Resolve to worktree base if this is a worktree
	fetchDir := repoPath
	if isWorktree(repoPath) {
		if worktreeBase, err := resolveWorktreeBaseFromWorktree(repoPath); err == nil {
			fetchDir = worktreeBase
		}
	}

	args := []string{"fetch"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = fetchDir

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %w: %s", err, string(output))
	}

	return nil
}

// Checkout runs git checkout -B, optionally resetting to origin/<branch>.
func (g *GitVCS) Checkout(ctx context.Context, repoPath, branch string, resetToOrigin bool) error {
	args := []string{"checkout", "-B", branch}
	if resetToOrigin {
		args = append(args, "origin/"+branch)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout failed: %w: %s", err, string(output))
	}

	return nil
}

// Pull runs git pull --rebase origin <branch>.
func (g *GitVCS) Pull(ctx context.Context, repoPath, branch string) error {
	// Check if origin remote exists
	remoteCmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	remoteCmd.Dir = repoPath
	if _, err := remoteCmd.CombinedOutput(); err != nil {
		// No origin remote - local-only repo, nothing to pull
		fmt.Printf("[vcs] no origin remote, skipping pull\n")
		return nil
	}

	// Explicitly pull from origin/<branch> to avoid broken upstream config
	args := []string{"pull", "--rebase", "origin", branch}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git pull failed: %w: %s", err, string(output))
	}

	return nil
}

// GetCurrentBranch returns the current branch name.
func (g *GitVCS) GetCurrentBranch(ctx context.Context, repoPath string) (string, error) {
	args := []string{"rev-parse", "--abbrev-ref", "HEAD"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w: %s", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}

// GetDefaultBranch returns the default branch for a remote URL.
// This queries the remote directly using git ls-remote.
func (g *GitVCS) GetDefaultBranch(ctx context.Context, repoPath, repoURL string) (string, error) {
	// Use git ls-remote to get the default branch
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--symref", repoURL, "HEAD")
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git ls-remote failed: %w: %s", err, string(output))
	}

	// Parse output: "ref: refs/heads/main\tHEAD\n..."
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "ref: refs/heads/") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ref := strings.TrimPrefix(parts[0], "ref: refs/heads/")
				return ref, nil
			}
		}
	}

	return "", fmt.Errorf("could not determine default branch from ls-remote output")
}

// HasOriginRemote checks if the repo has an origin remote.
func (g *GitVCS) HasOriginRemote(ctx context.Context, repoPath string) bool {
	remoteCmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	remoteCmd.Dir = repoPath
	return remoteCmd.Run() == nil
}

// RemoteBranchExists checks if a remote branch exists.
func (g *GitVCS) RemoteBranchExists(ctx context.Context, repoPath, branch string) (bool, error) {
	ref := "refs/remotes/origin/" + branch
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = repoPath

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git show-ref failed: %w", err)
	}

	return true, nil
}

// DiscardChanges runs git checkout -- .
func (g *GitVCS) DiscardChanges(ctx context.Context, repoPath string) error {
	args := []string{"checkout", "--", "."}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -- . failed: %w: %s", err, string(output))
	}

	return nil
}

// CleanUntracked runs git clean -fd.
func (g *GitVCS) CleanUntracked(ctx context.Context, repoPath string) error {
	args := []string{"clean", "-fd"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clean failed: %w: %s", err, string(output))
	}

	return nil
}

// GetStatus calculates the git status for a workspace directory.
func (g *GitVCS) GetStatus(ctx context.Context, repoPath, repoURL string, getDefaultBranch func(ctx context.Context, repoURL string) (string, error)) Status {
	var status Status

	// Fetch to get latest remote state for accurate ahead/behind counts
	_ = g.Fetch(ctx, repoPath)

	// Check for dirty state (any changes: modified, added, removed, or untracked)
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = repoPath
	output, err := statusCmd.CombinedOutput()
	status.Dirty = err == nil && len(strings.TrimSpace(string(output))) > 0

	// Check ahead/behind counts using rev-list
	defaultBranch, err := getDefaultBranch(ctx, repoURL)
	if err == nil {
		revListCmd := exec.CommandContext(ctx, "git", "rev-list", "--left-right", "--count", "HEAD...origin/"+defaultBranch)
		revListCmd.Dir = repoPath
		output, err = revListCmd.CombinedOutput()
		if err != nil {
			fmt.Printf("[vcs] git rev-list HEAD...origin/%s failed for %s: %s\n", defaultBranch, repoPath, strings.TrimSpace(string(output)))
		} else {
			parts := strings.Split(strings.TrimSpace(string(output)), "\t")
			if len(parts) == 2 {
				status.Ahead, _ = strconv.Atoi(parts[0])
				status.Behind, _ = strconv.Atoi(parts[1])
			}
		}
	}

	// Get line additions/deletions from uncommitted changes
	diffCmd := exec.CommandContext(ctx, "git", "diff", "--numstat", "HEAD")
	diffCmd.Dir = repoPath
	output, err = diffCmd.CombinedOutput()
	if err == nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			lines := strings.Split(trimmed, "\n")
			status.FilesChanged = len(lines)
			for _, line := range lines {
				parts := strings.Split(line, "\t")
				if len(parts) >= 2 {
					if added, err := strconv.Atoi(parts[0]); err == nil {
						status.LinesAdded += added
					}
					if removed, err := strconv.Atoi(parts[1]); err == nil && parts[1] != "-" {
						status.LinesRemoved += removed
					}
				}
			}
		}
	}

	// Get untracked files and count their lines as additions
	untrackedCmd := exec.CommandContext(ctx, "git", "ls-files", "--others", "--exclude-standard")
	untrackedCmd.Dir = repoPath
	untrackedOutput, err := untrackedCmd.Output()
	if err == nil {
		untrackedLines := strings.Split(string(untrackedOutput), "\n")
		for _, filePath := range untrackedLines {
			if filePath == "" {
				continue
			}
			fullPath := filepath.Join(repoPath, filePath)
			if difftool.IsBinaryFile(fullPath) {
				status.FilesChanged++
				continue
			}
			content, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}
			lineCount := strings.Count(string(content), "\n")
			if !strings.HasSuffix(string(content), "\n") {
				lineCount++
			}
			status.LinesAdded += lineCount
			status.FilesChanged++
		}
	}

	return status
}

// CheckSafety checks if a workspace is safe to dispose based on git state.
func (g *GitVCS) CheckSafety(ctx context.Context, repoPath string) (*SafetyStatus, error) {
	status := &SafetyStatus{Safe: true}

	// Check for dirty state
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = repoPath
	output, err := statusCmd.CombinedOutput()
	if err != nil {
		status.Safe = false
		status.Reason = fmt.Sprintf("git status failed: %v", err)
		return status, nil
	}

	// Parse status output
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "??") {
			status.UntrackedFiles++
			status.Safe = false
			continue
		}

		status.ModifiedFiles++
		status.Safe = false
	}

	// Check ahead/behind counts
	revListCmd := exec.CommandContext(ctx, "git", "rev-list", "--left-right", "--count", "HEAD...@{u}")
	revListCmd.Dir = repoPath
	output, err = revListCmd.CombinedOutput()
	if err != nil {
		// No upstream - local-only commits are OK
		fmt.Printf("[vcs] no upstream branch for %s, skipping ahead/behind check\n", repoPath)
	} else {
		parts := strings.Split(strings.TrimSpace(string(output)), "\t")
		if len(parts) == 2 {
			ahead, _ := strconv.Atoi(parts[0])
			status.AheadCommits = ahead
			if ahead > 0 {
				status.Safe = false
			}
		}
	}

	// Build reason string
	if !status.Safe {
		var reasons []string
		if status.ModifiedFiles > 0 {
			reasons = append(reasons, fmt.Sprintf("%d modified file(s)", status.ModifiedFiles))
		}
		if status.UntrackedFiles > 0 {
			reasons = append(reasons, fmt.Sprintf("%d untracked file(s)", status.UntrackedFiles))
		}
		if status.AheadCommits > 0 {
			reasons = append(reasons, fmt.Sprintf("%d unpushed commit(s)", status.AheadCommits))
		}
		if status.Reason != "" {
			reasons = append(reasons, status.Reason)
		}
		status.Reason = strings.Join(reasons, "; ")
	}

	return status, nil
}

// AddWorktree adds a git worktree.
func (g *GitVCS) AddWorktree(ctx context.Context, basePath, worktreePath, branch, repoURL string) error {
	fmt.Printf("[vcs] adding worktree: base=%s path=%s branch=%s\n", basePath, worktreePath, branch)

	// Check if local branch exists
	localBranchCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	localBranchCmd.Dir = basePath
	localBranchExists := localBranchCmd.Run() == nil

	// Check if remote branch exists
	remoteBranch := "origin/" + branch
	remoteBranchCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/remotes/"+remoteBranch)
	remoteBranchCmd.Dir = basePath
	remoteBranchExists := remoteBranchCmd.Run() == nil

	var args []string
	if localBranchExists {
		args = []string{"worktree", "add", worktreePath, branch}
	} else if remoteBranchExists {
		args = []string{"worktree", "add", "--track", "-b", branch, worktreePath, remoteBranch}
	} else {
		// Create new local branch from default branch
		defaultBranch, err := g.GetDefaultBranch(ctx, basePath, repoURL)
		if err != nil {
			return fmt.Errorf("failed to get default branch: %w", err)
		}
		args = []string{"worktree", "add", "-b", branch, worktreePath, "origin/" + defaultBranch}
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = basePath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add failed: %w: %s", err, string(output))
	}

	fmt.Printf("[vcs] worktree added: path=%s\n", worktreePath)
	return nil
}

// RemoveWorktree removes a git worktree.
func (g *GitVCS) RemoveWorktree(ctx context.Context, basePath, worktreePath string) error {
	fmt.Printf("[vcs] removing worktree: base=%s path=%s\n", basePath, worktreePath)

	args := []string{"worktree", "remove", "--force", worktreePath}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = basePath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove failed: %w: %s", err, string(output))
	}

	fmt.Printf("[vcs] worktree removed: path=%s\n", worktreePath)
	return nil
}

// PruneWorktrees runs git worktree prune.
func (g *GitVCS) PruneWorktrees(ctx context.Context, basePath string) error {
	fmt.Printf("[vcs] pruning stale worktrees: base=%s\n", basePath)

	args := []string{"worktree", "prune"}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = basePath

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree prune failed: %w: %s", err, string(output))
	}

	fmt.Printf("[vcs] worktrees pruned: base=%s\n", basePath)
	return nil
}

// InitLocalRepo initializes a local repo (no remote).
func (g *GitVCS) InitLocalRepo(ctx context.Context, path, branch string) error {
	fmt.Printf("[vcs] initializing local repository: path=%s branch=%s\n", path, branch)

	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	initCmd := exec.CommandContext(ctx, "git", "init")
	initCmd.Dir = path
	if output, err := initCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init failed: %w: %s", err, string(output))
	}

	// Configure user for initial commit
	configUserCmd := exec.CommandContext(ctx, "git", "config", "user.email", "schmux@localhost")
	configUserCmd.Dir = path
	if output, err := configUserCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config user.email failed: %w: %s", err, string(output))
	}

	configNameCmd := exec.CommandContext(ctx, "git", "config", "user.name", "schmux")
	configNameCmd.Dir = path
	if output, err := configNameCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config user.name failed: %w: %s", err, string(output))
	}

	branchCmd := exec.CommandContext(ctx, "git", "checkout", "-b", branch)
	branchCmd.Dir = path
	if output, err := branchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -b %s failed: %w: %s", branch, err, string(output))
	}

	commitCmd := exec.CommandContext(ctx, "git", "commit", "--allow-empty", "-m", "Initial commit")
	commitCmd.Dir = path
	if output, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit failed: %w: %s", err, string(output))
	}

	fmt.Printf("[vcs] local repository initialized: path=%s\n", path)
	return nil
}

// IsManaged returns true - GitVCS manages version control.
func (g *GitVCS) IsManaged() bool {
	return true
}

// Helper functions

// isWorktree checks if a path is a worktree (has .git file) vs full clone (.git dir).
func isWorktree(path string) bool {
	gitPath := filepath.Join(path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	return !info.IsDir() // File = worktree, Dir = full clone
}

// resolveWorktreeBaseFromWorktree reads the .git file to find the worktree base path.
func resolveWorktreeBaseFromWorktree(worktreePath string) (string, error) {
	gitFilePath := filepath.Join(worktreePath, ".git")
	content, err := os.ReadFile(gitFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read .git file: %w", err)
	}

	line := strings.TrimSpace(string(content))
	if !strings.HasPrefix(line, "gitdir: ") {
		return "", fmt.Errorf("invalid .git file format")
	}

	gitdir := strings.TrimPrefix(line, "gitdir: ")

	if idx := strings.Index(gitdir, "/worktrees/"); idx >= 0 {
		return gitdir[:idx], nil
	}

	return "", fmt.Errorf("could not parse worktree base from gitdir: %s", gitdir)
}
