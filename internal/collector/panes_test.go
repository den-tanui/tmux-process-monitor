package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	itx "github.com/den-tanui/tmux-process-monitor/internal/tmux"
)

// fakeTmux installs a stub `tmux` executable at the front of PATH.
//
// The stub appends every invocation to a calls file and prints the canned
// output registered for the subcommand (the first argument). It returns the
// path of the calls file so tests can count the invocations.
func fakeTmux(t *testing.T, responses map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	callsFile := filepath.Join(dir, "calls")

	script := "#!/bin/sh\n" +
		"echo \"$*\" >> \"" + callsFile + "\"\n" +
		"f=\"" + dir + "/out_$1\"\n" +
		"if [ -f \"$f\" ]; then cat \"$f\"; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	for sub, out := range responses {
		if err := os.WriteFile(filepath.Join(dir, "out_"+sub), []byte(out), 0o644); err != nil {
			t.Fatalf("write fake tmux response: %v", err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return callsFile
}

// TestCollectWindowForSession_panes is a characterisation test for the pane
// data path: the window must carry its pane list and the process list must
// start with the pane separator row.
func TestCollectWindowForSession_panes(t *testing.T) {
	fakeTmux(t, map[string]string{
		"list-panes": "0:999999\n",
	})

	c := New(itx.New(), "sess")
	wd := c.collectWindowForSession("sess", itx.Window{Index: 0, Name: "win"})

	if len(wd.Panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(wd.Panes))
	}
	if wd.Panes[0].PID != 999999 {
		t.Errorf("expected pane PID 999999, got %d", wd.Panes[0].PID)
	}
	if len(wd.Processes) == 0 {
		t.Fatal("expected at least the pane separator row")
	}
	sep := wd.Processes[0]
	if sep.PID != 0 {
		t.Errorf("expected the first row to be the pane separator, got PID %d", sep.PID)
	}
	if !strings.Contains(sep.Command, "pane 0") {
		t.Errorf("expected a pane separator row, got %q", sep.Command)
	}
}

// TestCollectAllSessions_paneData verifies that a session's windows carry
// their pane data after a refresh.
func TestCollectAllSessions_paneData(t *testing.T) {
	fakeTmux(t, map[string]string{
		"list-sessions": "sess\n",
		"list-windows":  "0:win\n",
		"list-panes":    "0:999999\n",
	})

	c := New(itx.New(), "sess")
	sessions, err := c.CollectAllSessions()
	if err != nil {
		t.Fatalf("CollectAllSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if len(sessions[0].Windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(sessions[0].Windows))
	}
	if len(sessions[0].Windows[0].Panes) != 1 {
		t.Errorf("expected 1 pane, got %d", len(sessions[0].Windows[0].Panes))
	}
}
