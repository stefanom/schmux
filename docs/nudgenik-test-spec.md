# NudgeNik Test Expansion Spec

## Context

NudgeNik analyzes terminal captures from coding agents (Claude, Codex) to classify their operational state. The extractor (`tmux.ExtractLatestResponse`) pulls meaningful content from raw terminal output, which is then sent to an LLM for classification.

8 new terminal captures were added but need proper test coverage.

## Files Involved

- `internal/tmux/tmux.go` - ExtractLatestResponse function
- `internal/tmux/tmux_test.go` - Extraction tests
- `internal/tmux/testdata/*.txt` - Raw terminal captures
- `internal/tmux/testdata/*.want.txt` - Expected extraction output
- `internal/nudgenik/nudgenik.go` - NudgeNik classification logic

## Task 1: Create .want.txt Files

### New Captures Needing .want.txt Files

| Capture | Expected NudgeNik State | Notes |
|---------|------------------------|-------|
| claude5.txt | Completed | Shows code analysis comparison |
| claude9.txt | Needs Authorization | Rate limit prompt with choices |
| claude10.txt | Needs Authorization | Permission prompt "Do you want to proceed?" |
| claude11.txt | Completed | Shows diffs with "Baked for 14m" status |
| claude12.txt | Needs Authorization | "Trust this folder?" startup prompt |
| codex4.txt | Completed | Work summary with next steps |
| codex5.txt | Completed | Summary offering further help |
| codex13.txt | Needs Authorization | Commit summary with "Proceed?" prompt |

### How to Generate

Run the debug test to see actual extractor output:

```bash
# In internal/tmux/tmux_test.go, the TestDebugExtraction function
# prints extracted output for each capture (uncomment t.Skip line first)
go test -v -run TestDebugExtraction ./internal/tmux/...
```

For each capture, copy the extracted output (between `=== <file> ===` and `=== END ===`) to the corresponding `.want.txt` file.

Example:
```bash
# Output for claude12.txt becomes internal/tmux/testdata/claude12.want.txt
```

### Add Test Fixtures

In `internal/tmux/tmux_test.go`, add entries to the `fixtures` slice in `TestExtractLatestResponse`:

```go
fixtures := []struct {
    name string
    in   string
    want string
}{
    // ... existing entries ...
    {name: "claude5", in: "claude5.txt", want: "claude5.want.txt"},
    {name: "claude9", in: "claude9.txt", want: "claude9.want.txt"},
    {name: "claude10", in: "claude10.txt", want: "claude10.want.txt"},
    {name: "claude11", in: "claude11.txt", want: "claude11.want.txt"},
    {name: "claude12", in: "claude12.txt", want: "claude12.want.txt"},
    {name: "codex4", in: "codex4.txt", want: "codex4.want.txt"},
    {name: "codex5", in: "codex5.txt", want: "codex5.want.txt"},
    {name: "codex13", in: "codex13.txt", want: "codex13.want.txt"},
}
```

## Task 2: NudgeNik Integration Test

Create `internal/nudgenik/integration_test.go` with build tag so it doesn't run in normal CI.

### Structure

```go
//go:build integration

package nudgenik

import (
    "context"
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/joho/godotenv" // or manual .env parsing
    "github.com/sergek/schmux/internal/oneshot"
    "github.com/sergek/schmux/internal/tmux"
)

func TestNudgenikClassification(t *testing.T) {
    // Load .env from project root
    _ = godotenv.Load("../../.env")

    apiKey := os.Getenv("ANTHROPIC_API_KEY")
    if apiKey == "" {
        t.Skip("ANTHROPIC_API_KEY not set")
    }

    fixtures := []struct {
        capture   string
        wantState string
    }{
        {"claude5.txt", "Completed"},
        {"claude9.txt", "Needs Authorization"},
        {"claude10.txt", "Needs Authorization"},
        {"claude11.txt", "Completed"},
        {"claude12.txt", "Needs Authorization"},
        {"codex4.txt", "Completed"},
        {"codex5.txt", "Completed"},
        {"codex13.txt", "Needs Authorization"},
    }

    for _, tt := range fixtures {
        t.Run(tt.capture, func(t *testing.T) {
            // Read capture
            path := filepath.Join("../tmux/testdata", tt.capture)
            data, err := os.ReadFile(path)
            if err != nil {
                t.Fatalf("read capture: %v", err)
            }

            // Extract
            content := tmux.StripAnsi(string(data))
            lines := strings.Split(content, "\n")
            extracted := tmux.ExtractLatestResponse(lines)
            if extracted == "" {
                t.Fatal("no content extracted")
            }

            // Build prompt
            prompt := Prompt + extracted

            // Call Claude
            ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
            defer cancel()

            response, err := oneshot.Execute(ctx, "claude", "claude", prompt, nil)
            if err != nil {
                t.Fatalf("oneshot: %v", err)
            }

            // Parse JSON response
            var result struct {
                State      string   `json:"state"`
                Confidence string   `json:"confidence"`
                Evidence   []string `json:"evidence"`
                Summary    string   `json:"summary"`
            }
            if err := json.Unmarshal([]byte(response), &result); err != nil {
                t.Fatalf("parse response: %v\nraw: %s", err, response)
            }

            // Check state
            if result.State != tt.wantState {
                t.Errorf("state = %q, want %q\nconfidence: %s\nsummary: %s",
                    result.State, tt.wantState, result.Confidence, result.Summary)
            }
        })
    }
}
```

### Running the Integration Test

```bash
# Create .env file with API key
echo "ANTHROPIC_API_KEY=sk-ant-..." > .env

# Run integration tests
go test -v -tags=integration -run TestNudgenikClassification ./internal/nudgenik/...
```

### Dependencies

May need to add `github.com/joho/godotenv` or implement simple .env parsing:

```go
func loadEnvFile(path string) error {
    data, err := os.ReadFile(path)
    if err != nil {
        return err
    }
    for _, line := range strings.Split(string(data), "\n") {
        line = strings.TrimSpace(line)
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        parts := strings.SplitN(line, "=", 2)
        if len(parts) == 2 {
            os.Setenv(parts[0], parts[1])
        }
    }
    return nil
}
```

## Task 3: Cleanup

Remove or re-skip the debug test in `internal/tmux/tmux_test.go`:

```go
func TestDebugExtraction(t *testing.T) {
    t.Skip("debug only - remove skip to see extracted output")
    // ...
}
```

## Verification

After completing all tasks:

```bash
# Unit tests pass
go test ./internal/tmux/...

# Integration tests pass (requires API key)
go test -v -tags=integration ./internal/nudgenik/...

# Full test suite
go test ./...
```
