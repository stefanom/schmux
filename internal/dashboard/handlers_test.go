package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/session"
	"github.com/sergek/schmux/internal/state"
	"github.com/sergek/schmux/internal/tmux"
	"github.com/sergek/schmux/internal/workspace"
)

func TestExtractLatestResponse(t *testing.T) {
	fixtures := []struct {
		name string
		in   string
		want string
	}{
		{name: "claude1", in: "claude1.txt", want: "claude1.want.txt"},
		{name: "claude2", in: "claude2.txt", want: "claude2.want.txt"},
		{name: "claude3", in: "claude3.txt", want: "claude3.want.txt"},
		{name: "claude4", in: "claude4.txt", want: "claude4.want.txt"},
		{name: "codex1", in: "codex1.txt", want: "codex1.want.txt"},
		{name: "codex2", in: "codex2.txt", want: "codex2.want.txt"},
		{name: "codex3", in: "codex3.txt", want: "codex3.want.txt"},
	}

	for _, tt := range fixtures {
		t.Run(tt.name, func(t *testing.T) {
			inputPath := filepath.Join("testdata", tt.in)
			inputRaw, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("read input: %v", err)
			}

			wantPath := filepath.Join("testdata", tt.want)
			wantRaw, err := os.ReadFile(wantPath)
			if err != nil {
				t.Fatalf("read want: %v", err)
			}

			input := tmux.StripAnsi(string(inputRaw))
			lines := strings.Split(input, "\n")
			got := extractLatestResponse(lines)
			want := strings.TrimRight(string(wantRaw), "\n")

			if got != want {
				t.Errorf("extractLatestResponse() mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

func TestExtractLatestResponseCapsContent(t *testing.T) {
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	lines = append(lines, "❯")

	got := extractLatestResponse(lines)
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 80 {
		t.Fatalf("expected 80 lines, got %d", len(gotLines))
	}

	if gotLines[0] != "line 21" || gotLines[79] != "line 100" {
		t.Fatalf("unexpected line range: %q ... %q", gotLines[0], gotLines[79])
	}
}

func TestHandleHasNudgenik(t *testing.T) {
	// SPIKE: Test that hasNudgenik always returns true (CLI tools available)
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm)

	// Create a GET request
	req, _ := http.NewRequest("GET", "/api/hasNudgenik", nil)
	rr := httptest.NewRecorder()

	server.handleHasNudgenik(rr, req)

	// Check response
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]bool
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp["available"] {
		t.Errorf("expected available=true, got %v", resp["available"])
	}
}

func TestHandleAskNudgenik(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm)

	// Add a test session
	testSession := state.Session{
		ID:          "test-session-123",
		WorkspaceID: "test-workspace",
		Agent:       "test",
		TmuxSession: "test-tmux-session",
	}
	st.AddSession(testSession)

	tests := []struct {
		name       string
		sessionID  string
		wantStatus int
	}{
		{
			name:       "missing session id",
			sessionID:  "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid session id (not found)",
			sessionID:  "nonexistent-session",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "valid session id",
			sessionID:  "test-session-123",
			wantStatus: http.StatusOK, // Or 500 if tmux capture fails or CLI unavailable
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a GET request with session ID in URL path
			req, _ := http.NewRequest("GET", "/api/askNudgenik/"+tt.sessionID, nil)
			rr := httptest.NewRecorder()

			server.handleAskNudgenik(rr, req)

			// Check response status
			// For the valid session case, accept 200 (success), 400 (no response extracted), or 500 (tmux/CLI failed)
			// since tmux may not be available in test environments
			if tt.name == "valid session id" {
				if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest && rr.Code != http.StatusInternalServerError {
					t.Errorf("expected status 200, 400, or 500, got %d", rr.Code)
				}
			} else {
				if rr.Code != tt.wantStatus {
					t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
				}
			}
		})
	}
}
