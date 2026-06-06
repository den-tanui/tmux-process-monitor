// tmux_e2e_test.go — end-to-end tests that run the real binary inside a tmux pane.
//
// These tests REQUIRE tmux and a running terminal. They are skipped with -short
// and are intended for local validation only (not CI).
package test_test

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── Helpers ───────────────────────────────────────────────────────

func randSuffix() string {
	return fmt.Sprintf("%06d", rand.Intn(1000000))
}

// tmuxCapture returns plain-text pane content (ANSI stripped).
func tmuxCapture(session string) string {
	out, err := exec.Command("tmux", "capture-pane", "-pt", session).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// tmuxType sends literal text to the pane (e.g. "j", "?", "hello").
func tmuxType(session, text string) {
	exec.Command("tmux", "send-keys", "-l", "-t", session, text).Run() //nolint:errcheck
}

// tmuxKey sends a named key (Enter, Escape, etc.).
func tmuxKey(session, key string) {
	exec.Command("tmux", "send-keys", "-t", session, key).Run() //nolint:errcheck
}

func panePGID(session string) string {
	out, err := exec.Command("tmux", "list-panes", "-t", session,
		"-F", "#{pane_pid}").Output()
	if err != nil {
		return ""
	}
	pid := strings.TrimSpace(string(out))
	if pid == "" {
		return ""
	}
	pgid, err := exec.Command("ps", "-o", "pgid=", "-p", pid).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(pgid))
}

func buildBinary(t *testing.T, dest string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", dest, "../cmd/tmux-process-monitor/")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build failed: %v", err)
	}
}

// ── Tests ─────────────────────────────────────────────────────────

// TestTmuxSmoke verifies the binary starts, renders the process list,
// handles basic navigation, mode switching, help, and clean exit.
// It exercises paths that model-level e2e tests cannot: real process
// data, terminal rendering, and async command execution (witr).
func TestTmuxSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("tmux e2e: skip with -short")
	}

	// Prerequisites.
	for _, bin := range []string{"tmux", "go"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not found on PATH", bin)
		}
	}

	// Build the binary to a temp location.
	binPath := filepath.Join(t.TempDir(), "tpm")
	buildBinary(t, binPath)

	// Create a dedicated tmux session with a large enough terminal.
	session := "tpm-smoke-" + randSuffix()
	create := exec.Command("tmux", "new-session", "-d", "-s", session,
		"-x", "132", "-y", "43")
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session failed: %v\n%s", err, out)
	}

	// Record PGID for cleanup.
	pgid := panePGID(session)
	t.Logf("tmux session %s (PGID %s)", session, pgid)

	// Cleanup: attempt graceful quit, then force kill.
	defer func() {
		tmuxType(session, "q")
		time.Sleep(300 * time.Millisecond)
		tmuxType(session, "q")
		time.Sleep(300 * time.Millisecond)
		if pgid != "" {
			exec.Command("kill", "-TERM", "-"+pgid).Run() //nolint:errcheck
			time.Sleep(200 * time.Millisecond)
			exec.Command("kill", "-KILL", "-"+pgid).Run() //nolint:errcheck
		}
		exec.Command("tmux", "kill-session", "-t", session).Run() //nolint:errcheck
	}()

	time.Sleep(200 * time.Millisecond)

	// ── 1. Launch the binary ──────────────────────────────────
	exec.Command("tmux", "send-keys", "-t", session,
		binPath, "Space", "-r", "Space", "0.5", "Enter").Run() //nolint:errcheck

	// Wait for the TUI to render.
	var output string
	for i := 0; i < 15; i++ {
		time.Sleep(400 * time.Millisecond)
		output = tmuxCapture(session)
		if strings.Contains(output, "PID") && strings.Contains(output, "CPU%") {
			break
		}
	}
	if !strings.Contains(output, "PID") {
		t.Fatalf("TUI did not start (no process list). Last capture:\n%s", output)
	}
	t.Log("✓ TUI started with process list")

	// ── 2. Verify header contains session name ────────────────
	if !strings.Contains(output, "Session:") {
		t.Errorf("expected 'Session:' in header")
	}
	t.Log("✓ Header visible")

	// ── 3. Navigate with j ────────────────────────────────────
	first := output
	tmuxType(session, "j")
	time.Sleep(400 * time.Millisecond)
	output = tmuxCapture(session)
	if output == first {
		t.Log("  (j pressed, selection may rely on reverse video)")
	} else {
		t.Log("✓ j navigation changed output")
	}

	// ── 4. Press ? to open help ───────────────────────────────
	tmuxType(session, "?")
	time.Sleep(400 * time.Millisecond)
	output = tmuxCapture(session)
	if strings.Contains(output, "Keyboard Reference") {
		t.Log("✓ Help overlay opened")
	} else {
		t.Errorf("expected 'Keyboard Reference' in help view, got:\n%s", output)
	}

	// ── 5. Close help with ? toggle ───────────────────────────
	tmuxType(session, "?")
	time.Sleep(300 * time.Millisecond)
	output = tmuxCapture(session)
	if strings.Contains(output, "PID") && strings.Contains(output, "CPU%") {
		t.Log("✓ Help closed, process list visible again")
	}

	// ── 6. Open overview ──────────────────────────────────────
	tmuxType(session, "o")
	time.Sleep(400 * time.Millisecond)
	output = tmuxCapture(session)
	if strings.Contains(output, "System Overview") {
		t.Log("✓ Overview mode opened")
	} else {
		t.Errorf("expected 'System Overview' in overview, got:\n%s", output)
	}

	// ── 7. Navigate in overview with j ────────────────────────
	tmuxType(session, "j")
	time.Sleep(300 * time.Millisecond)
	output = tmuxCapture(session)
	if strings.Contains(output, "j/k=browse") {
		t.Log("✓ Overview navigation works")
	}

	// ── 8. Return from overview with Escape ───────────────────
	tmuxKey(session, "Escape")
	time.Sleep(300 * time.Millisecond)
	output = tmuxCapture(session)
	if strings.Contains(output, "PID") && strings.Contains(output, "CPU%") {
		t.Log("✓ Escape returned to main view from overview")
	} else {
		t.Log("  Escape from overview result:\n" + output)
	}

	// ── 9. Enter detail view for a process ────────────────────
	tmuxType(session, "j")
	time.Sleep(200 * time.Millisecond)
	tmuxKey(session, "Enter")
	time.Sleep(800 * time.Millisecond)
	output = tmuxCapture(session)
	if strings.Contains(output, "Process Detail") {
		t.Log("✓ Detail view opened via Enter")
	} else {
		t.Log("  Enter on process did not open detail (witr may not be available). Output:\n" + output)
	}

	// ── 10. Return from detail with Escape ────────────────────
	tmuxKey(session, "Escape")
	time.Sleep(300 * time.Millisecond)
	output = tmuxCapture(session)
	if strings.Contains(output, "PID") && strings.Contains(output, "CPU%") {
		t.Log("✓ Escape returned to main view from detail")
	}

	// ── 11. Quit via q ────────────────────────────────────────
	tmuxType(session, "q")
	time.Sleep(600 * time.Millisecond)

	output = tmuxCapture(session)
	if !strings.Contains(output, "PID") && !strings.Contains(output, "Process Detail") {
		t.Log("✓ Binary exited cleanly via q")
	} else {
		t.Log("  q sent but TUI content still visible (may need force kill)")
	}
}
