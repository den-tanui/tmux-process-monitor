// Package tmux provides a thin wrapper around the tmux CLI for querying
// sessions, windows, and pane PIDs.
package tmux

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ErrNotRunning is returned when tmux is not running or the socket is unavailable.
var ErrNotRunning = errors.New("tmux is not running")

// Window holds the index and name of a tmux window.
type Window struct {
	Index int
	Name  string
}

// PaneInfo describes one tmux pane within a window.
type PaneInfo struct {
	Index int
	PID   int
}

// Client wraps tmux CLI commands. It is safe to use concurrently.
type Client struct{}

// New returns a ready-to-use Client.
func New() *Client { return &Client{} }

// IsRunning returns true if tmux is reachable (socket exists and responds).
func (c *Client) IsRunning() bool {
	err := exec.Command("tmux", "info").Run()
	return err == nil
}

// ListSessions returns the names of all active tmux sessions.
func (c *Client) ListSessions() ([]string, error) {
	out, err := run("tmux", "list-sessions", "-F", "#{session_name}")
	if err != nil {
		if isNoServerErr(err) {
			return nil, ErrNotRunning
		}
		return nil, fmt.Errorf("list-sessions: %w", err)
	}
	return nonEmpty(strings.Split(out, "\n")), nil
}

// ListAllWindows returns every window of every session in a single tmux
// call, keyed by session name. The second return value lists the session
// names in the order tmux reported them.
//
// A TAB separates the fields: tmux window names may contain a colon.
func (c *Client) ListAllWindows() (map[string][]Window, []string, error) {
	out, err := run("tmux", "list-windows", "-a", "-F",
		"#{session_name}\t#{window_index}\t#{window_name}")
	if err != nil {
		if isNoServerErr(err) {
			return nil, nil, ErrNotRunning
		}
		return nil, nil, fmt.Errorf("list-windows -a: %w", err)
	}
	windows, order := parseAllWindows(out)
	return windows, order, nil
}

// parseAllWindows parses the output of "tmux list-windows -a" into a map of
// session name to its windows, plus the session names in the order they
// appeared.
func parseAllWindows(out string) (map[string][]Window, []string) {
	windows := make(map[string][]Window)
	var order []string
	for _, line := range nonEmpty(strings.Split(out, "\n")) {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		idx, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		name := parts[0]
		if _, ok := windows[name]; !ok {
			order = append(order, name)
		}
		windows[name] = append(windows[name], Window{Index: idx, Name: parts[2]})
	}
	return windows, order
}

// ListAllPanes returns every pane of every window in a single tmux call,
// keyed by "session:windowIndex".
func (c *Client) ListAllPanes() (map[string][]PaneInfo, error) {
	out, err := run("tmux", "list-panes", "-a", "-F",
		"#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_pid}")
	if err != nil {
		if isNoServerErr(err) {
			return nil, ErrNotRunning
		}
		return nil, fmt.Errorf("list-panes -a: %w", err)
	}
	return parseAllPanes(out), nil
}

// parseAllPanes parses the output of "tmux list-panes -a" into a map of
// "session:windowIndex" to its panes.
func parseAllPanes(out string) map[string][]PaneInfo {
	panes := make(map[string][]PaneInfo)
	for _, line := range nonEmpty(strings.Split(out, "\n")) {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			continue
		}
		idx, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(parts[3])
		if err != nil {
			continue
		}
		key := parts[0] + ":" + parts[1]
		panes[key] = append(panes[key], PaneInfo{Index: idx, PID: pid})
	}
	return panes
}

// CurrentSession returns the name of the session that owns the calling pane.
func (c *Client) CurrentSession() (string, error) {
	out, err := run("tmux", "display-message", "-p", "#{session_name}")
	if err != nil {
		return "", fmt.Errorf("display-message session_name: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// ──────────────────────────────────────────────────────────────────────────────
// internal helpers

func run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

func nonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func isNoServerErr(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return strings.Contains(string(exitErr.Stderr), "no server running") ||
			strings.Contains(string(exitErr.Stderr), "error connecting to")
	}
	return false
}
