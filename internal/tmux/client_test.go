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
