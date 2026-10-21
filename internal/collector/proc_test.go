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

// ── parseStat ─────────────────────────────────────────────────────

func TestParseStat_ourPID(t *testing.T) {
	skipIfNoProc(t)
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		t.Fatalf("read stat: %v", err)
	}
	ps := parseStat(data)
	if !ps.ok {
		t.Fatal("expected the stat file to parse")
	}
	if ps.ppid <= 0 {
		t.Errorf("expected a non-zero parent pid, got %d", ps.ppid)
	}
	if ps.state == "" {
		t.Error("expected a non-empty state")
	}
	// A very fast process may legitimately report 0 ticks, so only the
	// successful parse is asserted here.
	t.Logf("parseStat(self) = ppid %d, state %q, ticks %d", ps.ppid, ps.state, ps.ticks)
}

func TestParseStat_commWithSpaces(t *testing.T) {
	// The comm field is parenthesised and may contain spaces, so the parser
	// has to scan for the last ')'.
	raw := "1234 (weird ) name) S 42 0 0 -1 0 0 0 0 0 0 77 13 extra fields\n"
	ps := parseStat([]byte(raw))
	if !ps.ok {
		t.Fatal("expected the stat to parse")
	}
	if ps.ppid != 42 {
		t.Errorf("expected ppid 42, got %d", ps.ppid)
	}
	if ps.state != "sleeping" {
		t.Errorf("expected 'sleeping', got %q", ps.state)
	}
	if ps.ticks != 90 {
		t.Errorf("expected 90 ticks, got %d", ps.ticks)
	}
}

func TestParseStat_truncated(t *testing.T) {
	if parseStat([]byte("1234 (short) S 1")).ok {
		t.Error("expected a truncated stat file not to parse")
	}
	if parseStat([]byte("garbage")).ok {
		t.Error("expected garbage not to parse")
	}
}

func TestParseStat_stateNames(t *testing.T) {
	cases := map[string]string{
		"R": "running", "S": "sleeping", "D": "disk sleep", "Z": "zombie",
		"T": "stopped", "t": "tracing stop", "I": "idle", "X": "dead",
	}
	for code, want := range cases {
		if got := stateName([]byte(code)); got != want {
			t.Errorf("stateName(%q) = %q, want %q", code, got, want)
		}
	}
}

// ── parseStatus ───────────────────────────────────────────────────

func TestParseStatus_ourPID(t *testing.T) {
	skipIfNoProc(t)
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	ppid, rss := parseStatus(data)
	if ppid <= 0 {
		t.Errorf("expected a non-zero parent pid, got %d", ppid)
	}
	if rss == 0 {
		t.Error("expected a non-zero RSS")
	}
	if rss < 4096 {
		t.Errorf("RSS seems too small: %d bytes", rss)
	}
}

func TestParseStatus_missing(t *testing.T) {
	ppid, rss := parseStatus([]byte("Name:\tfoo\n"))
	if ppid != 0 || rss != 0 {
		t.Errorf("expected zero values, got ppid=%d rss=%d", ppid, rss)
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
