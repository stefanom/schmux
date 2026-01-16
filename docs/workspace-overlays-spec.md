# Workspace Overlays Specification

## Problem Statement

Currently, schmux prepares workspaces by performing a git clone/checkout. Any additional files (`.env` files, local settings, etc.) that shouldn't be in git must be manually added to each workspace after creation.

## Proposed Solution

Add a "workspace overlay" system that copies extra files from a per-repo template directory into each newly created or prepared workspace.

## Storage Location

Overlay files are stored in:
```
~/.schmux/overlays/<repo-name>/
```

Where `<repo-name>` matches the `name` field from the `repos` config in `config.json`.

Example:
```json
{
  "repos": [
    {"name": "myproject", "url": "git@github.com:user/myproject.git"}
  ]
}
```

Overlay path: `~/.schmux/overlays/myproject/`

## Directory Structure

```
~/.schmux/overlays/
├── myproject/
│   ├── .env                 # Copied to workspace root
│   ├── config/
│   │   └── local.json      # Copied to workspace/config/local.json
│   └── credentials/
│       └── service.json    # Copied to workspace/credentials/service.json
└── other-project/
    └── .env.local
```

## File Copy Behavior

1. **Destination**: Files are copied into the workspace directory preserving the relative path structure
2. **Timing**: Files are copied during the `create()` phase after git clone completes
3. **Manual refresh**: Use `schmux refresh-overlay <workspace-id>` or dashboard button to reapply overlay files to existing workspaces
4. **Gitignore required**: Overlay files must be covered by `.gitignore`. If a file is NOT matched by `.gitignore`, the copy is **skipped** with a warning.
5. **Purpose**: Enforces that overlay files are truly local-only and won't accidentally be committed.

## Implementation Detail

The copy operation checks `.gitignore` coverage for each overlay file:

```go
// Skip if file is not covered by gitignore
if !isIgnoredByGit(workspacePath, relativePath) {
    log.Printf("WARNING: skipping overlay file (not in .gitignore): %s", relativePath)
    continue
}
```

Using `git check-ignore -q <path>` to verify coverage.

This ensures:
- Overlay files are truly meant to be local-only
- Files not in `.gitignore` are never copied from overlay (even if untracked)
- Adding a file to `.gitignore` automatically enables overlay support for it
- Fails safe: missing `.gitignore` entries prevent accidental copies

## Example Flow

For a workspace at `~/schmux-workspaces/myproject-001/`:

```
~/.schmux/overlays/myproject/.env
  -> ~/schmux-workspaces/myproject-001/.env

~/.schmux/overlays/myproject/config/local.json
  -> ~/schmux-workspaces/myproject-001/config/local.json
```

## User Workflows

### Initial Setup (One-time)

1. Create overlay directory:
   ```bash
   mkdir -p ~/.schmux/overlays/myproject
   ```

2. Add files to overlay:
   ```bash
   cp /path/to/.env ~/.schmux/overlays/myproject/
   ```

3. Ensure files are gitignored:
   ```bash
   # In the repo
   echo ".env" >> .gitignore
   ```

### Editing Overlay Files

Edit files directly in `~/.schmux/overlays/<repo-name>/`. Changes apply to:
- New workspaces automatically (on creation)
- Existing workspaces via `schmux refresh-overlay <workspace-id>` or dashboard button

## Implementation Changes

### Code Changes

**`internal/workspace/manager.go`**:

1. Add a new method `copyOverlayFiles(workspacePath, repoName string) error` that:
   - Checks if `~/.schmux/overlays/<repoName>/` exists
   - Recursively copies files preserving directory structure
   - Checks `.gitignore` coverage for each file
   - Logs what was copied and what was skipped

2. Call `copyOverlayFiles()` from `create()` after git clone completes

3. Add a new method `RefreshOverlay(ctx context.Context, workspaceID string) error` that:
   - Validates workspace exists and has no active sessions
   - Calls `copyOverlayFiles()` to reapply overlay

**New file: `internal/workspace/overlay.go`**:

- `OverlayDir(repoName string) (string, error)` - returns overlay directory path
- `CopyOverlay(srcDir, destDir string) error` - handles the recursive copy with gitignore checks
- `isIgnoredByGit(workspacePath, relativePath string) (bool, error)` - runs `git check-ignore`
- `ListOverlayFiles(repoName string) ([]string, error)` - lists overlay files (for debugging/dashboard)

**CLI command** - New file `cmd/schmux/refresh-overlay.go`:

```
Usage: schmux refresh-overlay <workspace-id>

Reapplies overlay files to an existing workspace.
```

### API Addition

**Dashboard handlers** (`internal/dashboard/handlers.go`):

- `POST /api/workspaces/:id/refresh-overlay` - Triggers overlay refresh for a workspace
- Returns 200 on success, 400 if workspace has active sessions, 404 if not found

### Configuration

No config changes required. The feature works via directory convention (like many Unix tools).

## Dashboard Integration

The settings/config page should display overlay information for each repo:

1. **Per-repo overlay status**:
   - Show overlay directory path: `~/.schmux/overlays/<repo-name>/`
   - Show status: "Exists" or "Missing" (badge/color-coded)
   - If exists: show file count (e.g., "3 files")

2. **Workspace actions** - Add "Refresh overlay" button:
   - Appears on workspace detail/settings
   - Calls `POST /api/workspaces/:id/refresh-overlay`
   - Disabled if workspace has active sessions

3. **API additions**:
   - `GET /api/overlays` - returns list of repos with overlay info
   - `POST /api/workspaces/:id/refresh-overlay` - reapplies overlay to workspace

   ```json
   {
     "overlays": [
       {
         "repo_name": "myproject",
         "path": "/Users/user/.schmux/overlays/myproject",
         "exists": true,
         "file_count": 3
       }
     ]
   }
   ```

### Future Enhancements

None currently.

## Edge Cases

1. **Overlay directory doesn't exist**: Silently skip (no overlay = git-only)
2. **File not in .gitignore**: Skip with warning log, continue with other files
3. **Overlay file already exists in workspace**: Overwrite (overlay wins, git state reset happens before)
4. **Symlinks in overlay**: Preserve as symlinks (copy as-is)
5. **Permissions**: Preserve file permissions (0644 for files, 0755 for dirs)

## Security Considerations

1. Overlay files are user-controlled local files, no remote fetching
2. No executable bit modification (copied as-is)
3. No special file types (devices, sockets) - skip with warning
4. `.gitignore` check prevents accidental shadowing of repo files
