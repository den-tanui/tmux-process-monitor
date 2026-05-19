// Package collector queries tmux for sessions/windows/panes and then walks the
// Linux /proc filesystem to build a process tree with CPU% and memory stats.
package collector

import "time"

// ──────────────────────────────────────────────────────────────────────────────
// Domain types

// Process represents one process node in a window's process tree.
type Process struct {
	PID         int
	PPID        int
	Command     string  // short display: basename(argv[0]) + args
	FullCmdline string  // raw cmdline joined with spaces (for detail view)
	CPUPercent  float64 // % of one CPU core, updated between ticks
	MemRSS      int64   // resident set size in bytes
	MemPercent  float64 // MemRSS as % of total RAM
	Depth       int     // tree depth (0 = pane shell)
	IsLastChild bool    // used by tree-prefix renderer
	HasChildren bool
	IsPlugin    bool // detected as tmux plugin process
	Status      string // running, sleeping, etc.
}

// WindowData aggregates all processes for one tmux window.
type WindowData struct {
	Name         string
	Index        int
	PanePIDs     []int
	Processes    []Process
	CPUTotal     float64
	MemTotal     int64
	ProcessCount int
}

// SessionData aggregates all windows for one tmux session.
type SessionData struct {
	Name         string
	Windows      []WindowData
	CPUTotal     float64
	MemTotal     int64
	ProcessCount int
	WindowCount  int
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal helpers

type cpuSample struct {
	ts         time.Time
	totalTicks uint64
}

type paneCacheEntry struct {
	ts   time.Time
	pids []int
}
