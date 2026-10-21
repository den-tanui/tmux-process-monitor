package ui

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/den-tanui/tmux-process-monitor/internal/collector"
)

// ── witr failure classification ─────────────────────────────────

func TestWitrFailure_missingBinary(t *testing.T) {
	// `script` runs the command in a shell: a missing binary makes the
	// shell exit 127, not "executable file not found".
	err := exec.Command("sh", "-c", "exit 127").Run()
	if err == nil {
		t.Fatal("expected the probe command to fail")
	}
	p := collector.Process{PID: 42, Command: "nvim", FullCmdline: "nvim main.go"}

	msg := witrFailure(err, nil, p)
	if !strings.Contains(msg, "not installed") {
		t.Errorf("expected the 'not installed' fallback, got:\n%s", msg)
	}
	if strings.Contains(msg, "witr error") {
		t.Errorf("expected the fallback, not the generic error, got:\n%s", msg)
	}
}

func TestWitrFailure_scriptMissing(t *testing.T) {
	// exec.Command fails with *exec.Error wrapping exec.ErrNotFound when
	// the binary itself does not exist.
	err := exec.Command("definitely-not-a-real-binary-xyz").Run()
	if err == nil {
		t.Fatal("expected the probe command to fail")
	}
	p := collector.Process{PID: 42, Command: "nvim"}

	msg := witrFailure(err, nil, p)
	if !strings.Contains(msg, "script") {
		t.Errorf("expected a message naming 'script', got:\n%s", msg)
	}
}

func TestWitrFailure_generic(t *testing.T) {
	p := collector.Process{PID: 42, Command: "nvim"}
	msg := witrFailure(errors.New("boom"), []byte("partial output"), p)
	if !strings.Contains(msg, "witr error") {
		t.Errorf("expected the generic error message, got:\n%s", msg)
	}
	if !strings.Contains(msg, "partial output") {
		t.Errorf("expected the captured output in the message, got:\n%s", msg)
	}
}

// ── signal status messages ───────────────────────────────────────

func TestKillStatusMessage_success(t *testing.T) {
	msg := killStatusMessage(nil, "SIGTERM", 1234)
	if !strings.Contains(msg, "Sent SIGTERM to PID 1234") {
		t.Errorf("unexpected success message: %q", msg)
	}
}

func TestKillStatusMessage_failure(t *testing.T) {
	msg := killStatusMessage(errors.New("no such process"), "SIGTERM", 1234)
	if !strings.Contains(msg, "no such process") {
		t.Errorf("expected the failure reason in the message, got: %q", msg)
	}
	if strings.Contains(msg, "Sent SIGTERM") {
		t.Errorf("must not claim success when the kill failed, got: %q", msg)
	}
}

// ── error surfacing ──────────────────────────────────────────────

func TestFooter_showsLastError(t *testing.T) {
	m := testModel()
	m.lastErr = errors.New("tmux is not running")
	footer := m.renderFooter()
	if !strings.Contains(footer, "tmux is not running") {
		t.Errorf("expected the last error in the footer, got:\n%s", footer)
	}
}

// ── horizontal scroll marker ─────────────────────────────────────

func TestRenderProcessRow_scrollMarker(t *testing.T) {
	m := testModel()
	m.width = 60
	procs := []collector.Process{
		{PID: 1, Command: strings.Repeat("x", 200), Status: "running", Depth: 0},
	}
	m.windows[0].Processes = procs
	m.currentTab = 0
	m.selectedProc = 0
	m.browsingProcs = true

	row := m.renderProcessRow(procs[0], 0, procs)
	if !strings.Contains(row, "»") {
		t.Errorf("expected the '»' marker on a scrolled row, got:\n%s", row)
	}
}

func TestRenderProcessRow_noScrollMarker(t *testing.T) {
	m := testModel()
	m.width = 120
	procs := []collector.Process{
		{PID: 1, Command: "nvim", Status: "running", Depth: 0},
	}
	m.windows[0].Processes = procs
	m.currentTab = 0
	m.selectedProc = 0
	m.browsingProcs = true

	row := m.renderProcessRow(procs[0], 0, procs)
	if strings.Contains(row, "»") {
		t.Errorf("expected no '»' marker on a short row, got:\n%s", row)
	}
}
