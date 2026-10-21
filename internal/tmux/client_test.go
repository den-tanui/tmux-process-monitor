package tmux

import (
	"fmt"
	"strings"
	"testing"
)

// TestListSessions_parsing verifies that multi-line output is parsed correctly.
func TestListSessions_parsing(t *testing.T) {
	lines := "main\nwork\nside\n"
	got := nonEmpty(strings.Split(lines, "\n"))
	want := []string{"main", "work", "side"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q want %q", i, got[i], want[i])
		}
	}
}

// TestListAllWindows_parsing verifies the tab separated output of
// "tmux list-windows -a".
func TestListAllWindows_parsing(t *testing.T) {
	out := "main\t0\teditor\nmain\t1\tmy:window\nwork\t3\tshell\n"
	windows, order := parseAllWindows(out)

	wantOrder := []string{"main", "work"}
	if len(order) != len(wantOrder) {
		t.Fatalf("got order %v, want %v", order, wantOrder)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Errorf("order[%d] got %q want %q", i, order[i], wantOrder[i])
		}
	}
	if len(windows["main"]) != 2 {
		t.Fatalf("expected 2 windows for main, got %d", len(windows["main"]))
	}
	if windows["main"][1].Index != 1 || windows["main"][1].Name != "my:window" {
		t.Errorf("got %+v, want index 1 name 'my:window'", windows["main"][1])
	}
	if len(windows["work"]) != 1 || windows["work"][0].Index != 3 {
		t.Errorf("unexpected windows for work: %+v", windows["work"])
	}
}

func TestListAllWindows_parsing_garbage(t *testing.T) {
	windows, order := parseAllWindows("garbage\nmain\tx\tname\nmain\t1\tok\n")
	if len(order) != 1 {
		t.Errorf("expected 1 session, got %v", order)
	}
	if len(windows["main"]) != 1 {
		t.Errorf("expected 1 valid window, got %+v", windows["main"])
	}
}

// TestListAllPanes_parsing verifies the tab separated output of
// "tmux list-panes -a".
func TestListAllPanes_parsing(t *testing.T) {
	out := "main\t0\t0\t1234\nmain\t0\t1\t5678\nmain\t2\t0\t9999\n"
	panes := parseAllPanes(out)

	if len(panes) != 2 {
		t.Fatalf("expected 2 window keys, got %d", len(panes))
	}
	got := panes["main:0"]
	if len(got) != 2 {
		t.Fatalf("expected 2 panes for main:0, got %d", len(got))
	}
	if got[0].Index != 0 || got[0].PID != 1234 {
		t.Errorf("got %+v, want index 0 pid 1234", got[0])
	}
	if got[1].Index != 1 || got[1].PID != 5678 {
		t.Errorf("got %+v, want index 1 pid 5678", got[1])
	}
	if len(panes["main:2"]) != 1 {
		t.Errorf("expected 1 pane for main:2, got %d", len(panes["main:2"]))
	}
}

func TestListAllPanes_parsing_garbage(t *testing.T) {
	panes := parseAllPanes("garbage\nmain\t0\t0\nmain\t0\tx\t1234\nmain\t0\t1\t5678\n")
	if len(panes) != 1 {
		t.Fatalf("expected 1 window key, got %d", len(panes))
	}
	if len(panes["main:0"]) != 1 {
		t.Errorf("expected 1 valid pane, got %+v", panes["main:0"])
	}
}

// TestListWindows_parsing verifies index:name parsing.
func TestListWindows_parsing(t *testing.T) {
	cases := []struct {
		line      string
		wantIdx   int
		wantName  string
		wantError bool
	}{
		{"0:editor", 0, "editor", false},
		{"1:my:window", 1, "my:window", false}, // colon in name
		{"bad", 0, "", true},
		{"x:editor", 0, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			parts := strings.SplitN(tc.line, ":", 2)
			if len(parts) != 2 {
				if !tc.wantError {
					t.Fatalf("unexpected parse error for %q", tc.line)
				}
				return
			}
			idx := 0
			_, err := fmt.Sscan(parts[0], &idx)
			if err != nil {
				if !tc.wantError {
					t.Fatalf("unexpected index parse error: %v", err)
				}
				return
			}
			if idx != tc.wantIdx || parts[1] != tc.wantName {
				t.Errorf("got (%d, %q) want (%d, %q)", idx, parts[1], tc.wantIdx, tc.wantName)
			}
		})
	}
}
