package vcs

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// SaplingVCS implements version control operations using Sapling (sl).
// This is used for remote workspaces where Sapling is the VCS instead of Git.
type SaplingVCS struct {
	// runCommand is a function that executes commands on the remote.
	// It's injected to allow running commands via the session manager.
	runCommand func(ctx context.Context, command string) (string, error)
}

// NewSaplingVCS creates a new SaplingVCS with the given command runner.
// The runCommand function should execute the command on the remote workspace
// and return the output.
func NewSaplingVCS(runCommand func(ctx context.Context, command string) (string, error)) *SaplingVCS {
	return &SaplingVCS{
		runCommand: runCommand,
	}
}

// SaplingStatus represents the parsed status from 'sl status'.
type SaplingStatus struct {
	Dirty        bool
	FilesChanged int
	LinesAdded   int
	LinesRemoved int
}

// GetStatus returns the Sapling status for the workspace.
func (s *SaplingVCS) GetStatus(ctx context.Context) (*SaplingStatus, error) {
	status := &SaplingStatus{}

	// Run 'sl status' to check for modified/added/removed files
	output, err := s.runCommand(ctx, "sl status")
	if err != nil {
		return nil, fmt.Errorf("failed to run sl status: %w", err)
	}

	// Parse status output
	// Format: X filename where X is M (modified), A (added), R (removed), ? (unknown)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if len(line) < 2 {
			continue
		}
		status.FilesChanged++
		status.Dirty = true
	}

	// Get line stats from 'sl diff --stat'
	diffStatOutput, err := s.runCommand(ctx, "sl diff --stat")
	if err == nil && diffStatOutput != "" {
		// Parse diff --stat output
		// Format: filename | changes ++--
		// Last line: X files changed, Y insertions(+), Z deletions(-)
		lines := strings.Split(strings.TrimSpace(diffStatOutput), "\n")
		for _, line := range lines {
			if strings.Contains(line, "insertion") || strings.Contains(line, "deletion") {
				// Summary line
				s.parseDiffStatSummary(line, status)
			}
		}
	}

	return status, nil
}

// parseDiffStatSummary parses the summary line from sl diff --stat.
func (s *SaplingVCS) parseDiffStatSummary(line string, status *SaplingStatus) {
	// Example: " 2 files changed, 10 insertions(+), 5 deletions(-)"
	parts := strings.Split(line, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "insertion") {
			// Extract number before "insertion"
			fields := strings.Fields(part)
			if len(fields) > 0 {
				if n, err := strconv.Atoi(fields[0]); err == nil {
					status.LinesAdded = n
				}
			}
		}
		if strings.Contains(part, "deletion") {
			fields := strings.Fields(part)
			if len(fields) > 0 {
				if n, err := strconv.Atoi(fields[0]); err == nil {
					status.LinesRemoved = n
				}
			}
		}
	}
}

// SaplingCommit represents a commit from Sapling log.
type SaplingCommit struct {
	Hash      string
	ShortHash string
	Message   string
	Author    string
	Timestamp time.Time
	Parents   []string
	Phase     string // draft, public
}

// GetGraph returns the commit graph for the workspace.
func (s *SaplingVCS) GetGraph(ctx context.Context, maxCommits int) (*contracts.GitGraphResponse, error) {
	// Build sl log command with JSON-like template output
	// We use a template that outputs fields separated by a delimiter
	template := "{node}|{shortest(node,7)}|{desc|firstline}|{author|user}|{date|isodate}|{parents}|{phase}\\n"
	cmd := fmt.Sprintf("sl log -r 'ancestors(.) & draft()' --template '%s' --limit %d 2>/dev/null || sl log --template '%s' --limit %d", template, maxCommits, template, maxCommits)

	output, err := s.runCommand(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to run sl log: %w", err)
	}

	// Parse the output into commits
	commits := s.parseLogOutput(output)

	// Convert to GitGraphResponse format
	resp := &contracts.GitGraphResponse{
		Repo:     "remote",
		Nodes:    make([]contracts.GitGraphNode, 0, len(commits)),
		Branches: make(map[string]contracts.GitGraphBranch),
	}

	for _, commit := range commits {
		node := contracts.GitGraphNode{
			Hash:      commit.Hash,
			ShortHash: commit.ShortHash,
			Message:   commit.Message,
			Author:    commit.Author,
			Timestamp: commit.Timestamp.Format(time.RFC3339),
			Parents:   commit.Parents,
			Branches:  []string{},
			IsHead:    []string{},
		}
		resp.Nodes = append(resp.Nodes, node)
	}

	// Mark the first commit (HEAD) as the current branch
	if len(resp.Nodes) > 0 {
		resp.Nodes[0].IsHead = []string{"@"} // Sapling uses @ for current commit
		resp.Branches["@"] = contracts.GitGraphBranch{
			Head:   resp.Nodes[0].Hash,
			IsMain: false,
		}
	}

	return resp, nil
}

// parseLogOutput parses the sl log template output into commits.
func (s *SaplingVCS) parseLogOutput(output string) []SaplingCommit {
	var commits []SaplingCommit

	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 7)
		if len(parts) < 6 {
			continue
		}

		commit := SaplingCommit{
			Hash:      parts[0],
			ShortHash: parts[1],
			Message:   parts[2],
			Author:    parts[3],
		}

		// Parse timestamp
		if ts, err := time.Parse("2006-01-02 15:04 -0700", strings.TrimSpace(parts[4])); err == nil {
			commit.Timestamp = ts
		}

		// Parse parents (space-separated hashes)
		if parts[5] != "" {
			commit.Parents = strings.Fields(parts[5])
		}

		// Parse phase if present
		if len(parts) > 6 {
			commit.Phase = strings.TrimSpace(parts[6])
		}

		commits = append(commits, commit)
	}

	return commits
}

// SaplingDiff represents a file diff from Sapling.
type SaplingDiff struct {
	Path         string
	Status       string // added, modified, deleted
	OldContent   string
	NewContent   string
	LinesAdded   int
	LinesRemoved int
	IsBinary     bool
}

// GetDiff returns the file diffs for uncommitted changes.
func (s *SaplingVCS) GetDiff(ctx context.Context) ([]SaplingDiff, error) {
	var diffs []SaplingDiff

	// Get list of changed files with status
	statusOutput, err := s.runCommand(ctx, "sl status")
	if err != nil {
		return nil, fmt.Errorf("failed to run sl status: %w", err)
	}

	// Parse status to get file list
	files := s.parseStatusOutput(statusOutput)

	// Get diff stats for each file
	diffStatOutput, err := s.runCommand(ctx, "sl diff --stat")
	if err == nil {
		s.parseDiffStats(diffStatOutput, files)
	}

	// For now, we don't fetch full file contents (expensive over remote)
	// The frontend will show stats without inline diff viewer
	for path, status := range files {
		diff := SaplingDiff{
			Path:   path,
			Status: status,
		}
		diffs = append(diffs, diff)
	}

	return diffs, nil
}

// parseStatusOutput parses 'sl status' output into a map of path -> status.
func (s *SaplingVCS) parseStatusOutput(output string) map[string]string {
	files := make(map[string]string)
	lines := strings.Split(strings.TrimSpace(output), "\n")

	for _, line := range lines {
		if len(line) < 3 {
			continue
		}

		statusChar := line[0]
		path := strings.TrimSpace(line[2:])

		switch statusChar {
		case 'M':
			files[path] = "modified"
		case 'A':
			files[path] = "added"
		case 'R':
			files[path] = "deleted"
		case '?':
			files[path] = "added" // Untracked files are treated as added
		case '!':
			files[path] = "deleted" // Missing files
		}
	}

	return files
}

// parseDiffStats updates file diffs with line counts from sl diff --stat.
func (s *SaplingVCS) parseDiffStats(output string, files map[string]string) {
	// Parse diff --stat output format:
	// path/to/file | 10 +++++-----
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if !strings.Contains(line, "|") {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		// The file path is on the left
		// We don't need to do anything here as we're just collecting status
	}
}
