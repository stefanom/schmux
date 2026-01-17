# Terminal Capture Test Data

Raw terminal captures from coding agents (Claude, Codex) used to test the `ExtractLatestResponse` function.

## File Structure

- `*.txt` - Raw terminal captures (input)
- `*.want.txt` - Expected extraction output (golden files)

## Capturing New Test Data

Figure out which terminal session you need:

```bash
tmux ls
```

Capture the session to a file:

```bash
tmux capture-pane -e -p -S -100 -t "session name" > internal/tmux/testdata/claudeN.txt
```

## Naming Convention

Files are named by agent type followed by an incremental number:
- `claude1.txt`, `claude2.txt`, ...
- `codex1.txt`, `codex2.txt`, ...

## Adding New Test Cases

1. Capture terminal output (see above)

2. Add the fixture to `TestExtractLatestResponse` in `tmux_test.go`:
   ```go
   {name: "claude99", in: "claude99.txt", want: "claude99.want.txt"},
   ```

3. Generate the `.want.txt` file:
   ```bash
   UPDATE_GOLDEN=1 go test -v -run TestUpdateGoldenFiles ./internal/tmux/...
   ```

4. Review the generated `.want.txt` to ensure the extraction is correct.

## Regenerating Golden Files

If the extractor logic changes, regenerate all golden files:

```bash
UPDATE_GOLDEN=1 go test -v -run TestUpdateGoldenFiles ./internal/tmux/...
```

Then review the diffs to ensure changes are expected.

## Test Fixtures

| Capture | Description | Expected NudgeNik State |
|---------|-------------|------------------------|
| claude1-4 | Various working states | - |
| claude5 | Code analysis comparison | Completed |
| claude6-8 | Various working states | - |
| claude9 | Rate limit prompt | Needs Authorization |
| claude10 | Permission prompt | Needs Authorization |
| claude11 | Diff with status | Completed |
| claude12 | Trust folder prompt | Needs Authorization |
| codex1-3 | Codex working states | - |
| codex4 | Work summary | Completed |
| codex5 | Summary with offer | Completed |
| codex13 | Commit with Proceed? | Needs Authorization |

## How Extraction Works

The `ExtractLatestResponse` function:
1. Finds the last prompt line (`❯` or `›`)
2. Collects non-empty content lines going backwards (up to 80 lines)
3. Appends any choice menu lines after the prompt

The extracted content is sent to NudgeNik for agent state classification.
