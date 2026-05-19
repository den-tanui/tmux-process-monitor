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

// ErrSessionNotFound is returned when the requested session does not exist.
type ErrSessionNotFound struct{ Name string }

func (e ErrSessionNotFound) Error() string {
	return fmt.Sprintf("tmux session %q not found", e.Name)
}

// Window holds the index and name of a tmux window.
type Window struct {
	Index int
	Name  string
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

// ListWindows returns all windows in the given session.
func (c *Client) ListWindows(session string) ([]Window, error) {
	out, err := run("tmux", "list-windows", "-t", session,
		"-F", "#{window_index}:#{window_name}")
	if err != nil {
		if isNoServerErr(err) {
			return nil, ErrNotRunning
		}
		return nil, fmt.Errorf("list-windows(%s): %w", session, err)
	}
	var windows []Window
	for _, line := range nonEmpty(strings.Split(out, "\n")) {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		idx, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		windows = append(windows, Window{Index: idx, Name: parts[1]})
	}
	return windows, nil
}

// ListPanePIDs returns the shell PIDs for all panes in session:windowIndex.
func (c *Client) ListPanePIDs(session string, windowIndex int) ([]int, error) {
	target := fmt.Sprintf("%s:%d", session, windowIndex)
	out, err := run("tmux", "list-panes", "-t", target, "-F", "#{pane_pid}")
	if err != nil {
		if isNoServerErr(err) {
			return nil, ErrNotRunning
		}
		return nil, fmt.Errorf("list-panes(%s): %w", target, err)
	}
	var pids []int
	for _, s := range nonEmpty(strings.Split(out, "\n")) {
		pid, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// CurrentSession returns the name of the session that owns the calling pane.
func (c *Client) CurrentSession() (string, error) {
	out, err := run("tmux", "display-message", "-p", "#{session_name}")
	if err != nil {
		return "", fmt.Errorf("display-message session_name: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// CurrentWindow returns the name of the active window in the calling pane.
func (c *Client) CurrentWindow() (string, error) {
	out, err := run("tmux", "display-message", "-p", "#{window_name}")
	if err != nil {
		return "", fmt.Errorf("display-message window_name: %w", err)
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
