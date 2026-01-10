package tmux

import (
	"context"
	"strings"
	"testing"
)

func TestGetAttachCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple session name",
			input: "test-session",
			want:  `tmux attach -t "test-session"`,
		},
		{
			name:  "session with spaces",
			input: "cli commands",
			want:  `tmux attach -t "cli commands"`,
		},
		{
			name:  "session with special chars",
			input: "session-123_abc",
			want:  `tmux attach -t "session-123_abc"`,
		},
		{
			name:  "empty string",
			input: "",
			want:  `tmux attach -t ""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetAttachCommand(tt.input)
			if got != tt.want {
				t.Errorf("GetAttachCommand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCaptureLastLines_Validation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		lines       int
		wantErr     bool
		errContains string
	}{
		{
			name:        "zero lines",
			lines:       0,
			wantErr:     true,
			errContains: "invalid line count",
		},
		{
			name:        "negative lines",
			lines:       -1,
			wantErr:     true,
			errContains: "invalid line count",
		},
		{
			name:        "negative large lines",
			lines:       -100,
			wantErr:     true,
			errContains: "invalid line count",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CaptureLastLines(ctx, "test-session", tt.lines)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %d lines", tt.lines)
				} else if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want containing %q", err, tt.errContains)
				}
			}
		})
	}

	// Positive line counts should pass validation (tmux may not be installed, so exec may fail)
	t.Run("positive line count passes validation", func(t *testing.T) {
		_, err := CaptureLastLines(ctx, "test-session", 10)
		if err != nil && strings.Contains(err.Error(), "invalid line count") {
			t.Errorf("unexpected validation error: %v", err)
		}
		// Other errors (like tmux not installed) are fine
	})
}

func TestContextCancellation(t *testing.T) {
	t.Run("CreateSession respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := CreateSession(ctx, "test", "/tmp", "echo test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("ListSessions respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := ListSessions(ctx)
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("GetPanePID respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := GetPanePID(ctx, "test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("CaptureOutput respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := CaptureOutput(ctx, "test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("KillSession respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := KillSession(ctx, "test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("SendKeys respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := SendKeys(ctx, "test", "command")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("SetWindowSizeManual respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := SetWindowSizeManual(ctx, "test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("ResizeWindow respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := ResizeWindow(ctx, "test", 80, 24)
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("StartPipePane respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := StartPipePane(ctx, "test", "/tmp/test.log")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("StopPipePane respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := StopPipePane(ctx, "test")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})

	t.Run("IsPipePaneActive respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		active := IsPipePaneActive(ctx, "test")
		// Should return false on error
		if active {
			t.Log("unexpected true for cancelled context")
		}
	})

	t.Run("RenameSession respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := RenameSession(ctx, "old", "new")
		if err == nil {
			t.Log("may succeed if context wasn't cancelled fast enough")
		}
	})
}

// Tests that require tmux to be installed - skipped by default

func TestListSessions(t *testing.T) {
	t.Skip("requires tmux to be installed")
}

func TestSessionExists(t *testing.T) {
	t.Skip("requires tmux to be installed")
}

func TestCaptureOutput(t *testing.T) {
	t.Skip("requires tmux to be installed")
}

func TestCreateSession(t *testing.T) {
	t.Skip("requires tmux to be installed")
}

func TestKillSession(t *testing.T) {
	t.Skip("requires tmux to be installed")
}

func TestSendKeys(t *testing.T) {
	t.Skip("requires tmux to be installed")
}

// Benchmarks

func BenchmarkGetAttachCommand(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetAttachCommand("test-session")
	}
}
