package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	itx "github.com/den-tanui/tmux-process-monitor/internal/tmux"
)

// fakeTmux installs a stub `tmux` executable at the front of PATH.
//
// The stub appends every invocation to a calls file and prints the canned
// output registered for the invocation. Responses are keyed by the first two
// arguments ("list-windows -a"), falling back to the first argument alone
// ("list-sessions"). It returns the path of the calls file so tests can
// count the invocations.
func fakeTmux(t *testing.T, responses map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	callsFile := filepath.Join(dir, "calls")

	script := "#!/bin/sh\n" +
		"echo \"$1 $2\" >> \"" + callsFile + "\"\n" +
		"f=\"" + dir + "/out_$1_$2\"\n" +
		"if [ -f \"$f\" ]; then cat \"$f\"; exit 0; fi\n" +
		"f=\"" + dir + "/out_$1\"\n" +
		"if [ -f \"$f\" ]; then cat \"$f\"; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	for key, out := range responses {
		name := strings.ReplaceAll(key, " ", "_")
		if err := os.WriteFile(filepath.Join(dir, "out_"+name), []byte(out), 0o644); err != nil {
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
		"list-panes -t": "0:999999\n",
	})

	c := New(itx.New(), "sess")
	panes := map[string][]itx.PaneInfo{"sess:0": {{Index: 0, PID: 999999}}}
	wd := c.collectWindowForSession("sess", itx.Window{Index: 0, Name: "win"}, panes)

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

// TestCollectAllSessions_tmuxExecCount verifies that a collection cycle
// spawns a small, fixed number of tmux processes instead of one per window.
// Three sessions with four windows each used to cost 16 invocations.
func TestCollectAllSessions_tmuxExecCount(t *testing.T) {
	var sessions, allWindows, allPanes, oneWindow, onePane strings.Builder
	for s := 1; s <= 3; s++ {
		fmt.Fprintf(&sessions, "sess%d\n", s)
		for w := 0; w < 4; w++ {
			fmt.Fprintf(&allWindows, "sess%d\t%d\twin%d\n", s, w, w)
			fmt.Fprintf(&allPanes, "sess%d\t%d\t0\t999999\n", s, w)
		}
	}
	for w := 0; w < 4; w++ {
		fmt.Fprintf(&oneWindow, "%d:win%d\n", w, w)
		fmt.Fprintf(&onePane, "0:999999\n")
	}

	calls := fakeTmux(t, map[string]string{
		"list-sessions":   sessions.String(),
		"list-windows -a": allWindows.String(),
		"list-windows -t": oneWindow.String(),
		"list-panes -a":   allPanes.String(),
		"list-panes -t":   onePane.String(),
	})

	c := New(itx.New(), "sess1")
	if _, err := c.CollectAllSessions(); err != nil {
		t.Fatalf("CollectAllSessions: %v", err)
	}

	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	n := len(strings.Split(strings.TrimSpace(string(data)), "\n"))
	t.Logf("tmux invocations: %d", n)
	if n > 4 {
		t.Errorf("expected at most 4 tmux invocations per cycle, got %d", n)
	}
}

// TestCollectAllSessions_paneData verifies that a session's windows carry
// their pane data after a refresh.
func TestCollectAllSessions_paneData(t *testing.T) {
	fakeTmux(t, map[string]string{
		"list-sessions":   "sess\n",
		"list-windows -a": "sess\t0\twin\n",
		"list-windows -t": "0:win\n",
		"list-panes -a":   "sess\t0\t0\t999999\n",
		"list-panes -t":   "0:999999\n",
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
