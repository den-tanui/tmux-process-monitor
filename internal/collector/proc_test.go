package collector

import (
	"os"
	"strings"
	"testing"
)

// skipIfNoProc skips the test when /proc is not available (non-Linux, containers, etc.).
func skipIfNoProc(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/proc/self/stat"); os.IsNotExist(err) {
		t.Skip("/proc not available")
	}
}

// ── readStatTicks ──────────────────────────────────────────────────

func TestReadStatTicks_ourPID(t *testing.T) {
	skipIfNoProc(t)
	ticks := readStatTicks(os.Getpid())
	// A very fast process may report 0 ticks — just verify the function ran.
	t.Logf("readStatTicks(%d) = %d", os.Getpid(), ticks)
}

func TestReadStatTicks_badPID(t *testing.T) {
	skipIfNoProc(t)
	ticks := readStatTicks(999999999)
	if ticks != 0 {
		t.Errorf("expected 0 for non-existent PID, got %d", ticks)
	}
}

func TestReadStatTicks_pid1(t *testing.T) {
	skipIfNoProc(t)
	ticks := readStatTicks(1)
	if ticks == 0 {
		t.Errorf("expected non-zero stat ticks for PID 1, got 0")
	}
}

// ── readMemRSS ─────────────────────────────────────────────────────

func TestReadMemRSS_ourPID(t *testing.T) {
	skipIfNoProc(t)
	rss := readMemRSS(os.Getpid())
	if rss == 0 {
		t.Errorf("expected non-zero RSS for PID %d, got 0", os.Getpid())
	}
	// Go test binaries are at least a few KB.
	if rss < 4096 {
		t.Errorf("RSS seems too small: %d bytes", rss)
	}
}

func TestReadMemRSS_badPID(t *testing.T) {
	skipIfNoProc(t)
	rss := readMemRSS(999999999)
	if rss != 0 {
		t.Errorf("expected 0 for non-existent PID, got %d", rss)
	}
}

// ── readPPID ───────────────────────────────────────────────────────

func TestReadPPID_ourPID(t *testing.T) {
	skipIfNoProc(t)
	ppid := readPPID(os.Getpid())
	if ppid == 0 {
		t.Errorf("expected non-zero parent PID for PID %d, got 0", os.Getpid())
	}
}

func TestReadPPID_badPID(t *testing.T) {
	skipIfNoProc(t)
	ppid := readPPID(999999999)
	if ppid != 0 {
		t.Errorf("expected 0 for non-existent PID, got %d", ppid)
	}
}

func TestReadPPID_pid1(t *testing.T) {
	skipIfNoProc(t)
	ppid := readPPID(1)
	// PID 1 has PPID 0 on most systems.
	if ppid < 0 || ppid > 1 {
		t.Errorf("expected PPID 0 or 1 for PID 1, got %d", ppid)
	}
}

// ── readState ──────────────────────────────────────────────────────

func TestReadState_ourPID(t *testing.T) {
	skipIfNoProc(t)
	state := readState(os.Getpid())
	if state != "running" && state != "sleeping" {
		t.Errorf("expected 'running' or 'sleeping' for our PID, got %q", state)
	}
}

func TestReadState_badPID(t *testing.T) {
	skipIfNoProc(t)
	state := readState(999999999)
	if state != "Unknown" {
		t.Errorf("expected 'Unknown' for non-existent PID, got %q", state)
	}
}

// ── totalRAMBytes ──────────────────────────────────────────────────

func TestTotalRAMBytes_sanity(t *testing.T) {
	skipIfNoProc(t)
	ram := totalRAMBytes()
	if ram == 0 {
		t.Errorf("expected non-zero total RAM, got 0")
	}
	if ram < 4*1024*1024 {
		t.Errorf("total RAM seems too low: %d bytes (%d MB)", ram, ram/1024/1024)
	}
}

// ── pidExists ──────────────────────────────────────────────────────

func TestPidExists_true(t *testing.T) {
	skipIfNoProc(t)
	if !pidExists(1) {
		t.Error("expected PID 1 to exist")
	}
}

func TestPidExists_ourPID(t *testing.T) {
	skipIfNoProc(t)
	if !pidExists(os.Getpid()) {
		t.Error("expected our own PID to exist")
	}
}

func TestPidExists_false(t *testing.T) {
	skipIfNoProc(t)
	if pidExists(999999999) {
		t.Error("expected PID 999999999 to not exist")
	}
}

// ── readChildren ───────────────────────────────────────────────────

func TestReadChildren_pid1(t *testing.T) {
	skipIfNoProc(t)
	children := readChildren(1)
	// PID 1 (init/systemd) should have at least some children on any Linux system.
	if len(children) == 0 {
		t.Log("PID 1 has no children — this is unusual but possible (e.g. containers)")
	}
	// Verify no negative or zero PIDs in result.
	for _, pid := range children {
		if pid <= 0 {
			t.Errorf("unexpected child PID %d from readChildren(1)", pid)
		}
	}
}

func TestReadChildren_badPID(t *testing.T) {
	skipIfNoProc(t)
	// A non-existent PID should not panic — nil or empty slice is fine.
	_ = readChildren(999999999)
}

// ── readCmdline supplements ────────────────────────────────────────

func TestReadCmdline_ourPID(t *testing.T) {
	skipIfNoProc(t)
	short, full := readCmdline(os.Getpid())
	if short == "" || full == "" {
		t.Errorf("expected non-empty cmdline for our PID, got short=%q full=%q", short, full)
	}
	// The first token of short should be basename (no path separator).
	first := strings.Fields(short)
	if len(first) > 0 && strings.Contains(first[0], "/") {
		t.Errorf("short cmdline command should be basename only, got %q", first[0])
	}
}

func TestReadCmdline_badPID(t *testing.T) {
	skipIfNoProc(t)
	short, full := readCmdline(999999999)
	if short == "" || full == "" {
		t.Errorf("expected bracketed comm for non-existent PID, got short=%q full=%q", short, full)
	}
}

// ── readCmdline kernel thread (existing) ───────────────────────────
// TestReadCmdline_kernelThread is defined in collector_test.go

// ── scanChildrenFallback ───────────────────────────────────────────
// This function is tested indirectly via readChildren when the kernel
// lacks CONFIG_PROC_CHILDREN. There's no reliable way to force the
// fallback path in a unit test without mocking /proc.
