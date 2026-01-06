# schmux - Smart Cognitive Hub on tmux

## Specification v0.5

### Overview

A Golang application that orchestrates multiple AI coding agents (Codex, Claude Code with various LLM backends) running in tmux sessions. Provides a web dashboard for spawning, monitoring, and managing agent sessions across git repositories.

### Core Components

1. **Daemon** - Long-running background process (`schmux start` / `schmux stop`)
2. **Web Dashboard** - localhost, no auth, primary UI for managing sessions
3. **Workspace Manager** - Manages cloned repo directories
4. **tmux Integration** - Each agent runs in its own tmux session

---

### Configuration

JSON file, hand-edited for v0.5. Location: `~/.schmux/config.json`

```json
{
  "workspace_path": "~/dev/schmux-workspaces",
  "repos": [
    {
      "name": "myproject",
      "url": "git@github.com:user/myproject.git"
    }
  ],
  "agents": [
    {
      "name": "codex",
      "command": "codex"
    },
    {
      "name": "claude",
      "command": "claude"
    },
    {
      "name": "claude-glm",
      "command": "/path/to/glm-4.7"
    },
    {
      "name": "claude-minimax",
      "command": "/path/to/minimax"
    },
    {
      "name": "claude-kimi",
      "command": "/path/to/kimi-thinking"
    }
  ]
}
```

---

### Workspace Management

- **Single global workspace directory** configured in `workspace_path`
- **Sequential directory naming**: `<repo>-001`, `<repo>-002`, etc.
- **Directory status tracking**: available (clean, no active session) vs in-use
- **Git operations**:
  - Clone repo if not already present
  - Checkout to specified local branch
  - `git pull --rebase` before starting session
  - If pull/rebase fails → workspace marked unusable (conflicts need manual resolution)
  - Cleanup: `git checkout -- .` to reset state when disposing

---

### Session Lifecycle

1. User spawns session(s) via web dashboard
2. schmux finds or creates a workspace directory for the repo
3. Ensures git state is clean, correct branch checked out, pulls latest
4. Creates tmux session, runs agent command with user's prompt
5. Session tracked in state (process running/stopped)
6. User can attach via terminal (`tmux attach -t <session>`)
7. tmux session persists after agent process exits (enables resume)
8. User disposes session via dashboard when done (cleans up workspace)

---

### Web Dashboard Features

**Session List View**
- Flat list of all sessions
- Displays: project name, directory, agent type, branch, process status (running/stopped)
- Copy-able attach command for each session
- Dispose button per session

**Spawn View**
- Select git repo (dropdown from pre-registered list)
- Enter branch name
- Enter prompt (textarea)
- Agent quantity selector ("shopping cart" style - pick count per agent type)
- Submit spawns all requested sessions with same prompt

**Session Detail View**
- Real-time terminal output (scrolling text)
- Session metadata (repo, branch, agent, created time)
- Dispose button

---

### State

JSON file at `~/.schmux/state.json`

```json
{
  "workspaces": [
    {
      "id": "myproject-001",
      "repo": "myproject",
      "path": "/Users/x/dev/schmux-workspaces/myproject-001",
      "in_use": true,
      "session_id": "schmux-myproject-001-abc123",
      "usable": true
    }
  ],
  "sessions": [
    {
      "id": "schmux-myproject-001-abc123",
      "workspace_id": "myproject-001",
      "agent": "claude-glm",
      "branch": "main",
      "prompt": "fix the auth bug",
      "tmux_session": "schmux-myproject-001-abc123",
      "created_at": "2025-01-05T10:30:00Z",
      "pid": 12345
    }
  ]
}
```

---

### CLI Commands (v0.5)

```
schmux start          # start daemon in background
schmux stop           # stop daemon
schmux status         # show daemon status, web dashboard URL
```

---

### Technical Notes

- **Language**: Go
- **Web server**: Embedded in daemon, serves dashboard
- **Terminal streaming**: Capture tmux pane output, stream to browser via websocket
- **Process tracking**: Monitor agent PID to determine running/stopped status

---

### Out of Scope (v0.5)

- CLI for spawning/resume (web only for v0.5)
- Config UI (hand-edit JSON)
- Completion hooks/notifications
- Budget tracking
- Batch grouping in dashboard
- Full terminal emulator in browser

---

## Future Scope

### v1.0 Candidates

- **Config management UI** - Web and/or CLI interface instead of hand-editing JSON
- **CLI commands for spawning** - `schmux run --repo X --branch Y --agents "claude:3" --prompt "..."`
- **CLI commands for resume** - `schmux resume <session-id>`
- **Batch grouping** - Dashboard groups sessions started together with same prompt
- **Richer session status** - Beyond just process running/stopped

### v1.1 Candidates

- **Completion notification** - Via agent hooks (`--hook` on prompt complete) to distinguish "task complete" vs "waiting for input" vs "running"
- **Full terminal emulator** - xterm.js in browser with colors, cursor, full interactivity
- **Show repo diffs in browser** - View what changes agents have made to the codebase
- **Open source license** - Select and add appropriate license

### v1.1+ Candidates

- **Budget tracking** - Track API costs per agent/session
- **Feedback system** - Rate agent outputs, track which agents/backends perform better on different tasks
- **Pluggable agent configuration** - Easier way to define new LLM endpoints without wrapper scripts
- **SQLite for state** - More robust storage if JSON becomes limiting
- **Remote branch operations** - Create branches, push, PR creation
- **Getting started documentation** - Installation guide, tutorials, examples
