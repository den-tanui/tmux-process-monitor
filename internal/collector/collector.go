package collector

import (
	"fmt"
	"sync"
	"time"

	itx "github.com/den-tanui/tmux-process-monitor/internal/tmux"
)

// Collector gathers process data for one tmux session.
type Collector struct {
	tmux    *itx.Client
	session string

	mu          sync.RWMutex
	cpuBaseline map[int]cpuSample

	totalRAM int64 // bytes
}

// New returns an initialised Collector for the given session.
func New(tmuxClient *itx.Client, session string) *Collector {
	return &Collector{
		tmux:        tmuxClient,
		session:     session,
		cpuBaseline: make(map[int]cpuSample),
		totalRAM:    totalRAMBytes(),
	}
}

// SetSession changes the active session without creating a new Collector.
func (c *Collector) SetSession(session string) {
	c.mu.Lock()
	c.session = session
	c.mu.Unlock()
}

// sessionName returns the active session name under the lock.
func (c *Collector) sessionName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.session
}

// WarmUp establishes CPU baselines for all pane processes asynchronously.
// Call once after construction; first Collect() will return 0% CPU until
// the warmup completes (~150ms later).
func (c *Collector) WarmUp() {
	go func() {
		time.Sleep(100 * time.Millisecond)
		windows, err := c.tmux.ListWindows(c.sessionName())
		if err != nil {
			return
		}
		for _, w := range windows {
			for _, pid := range c.panePIDs(c.sessionName(), w.Index) {
				c.sampleTree(pid)
			}
		}
	}()
}

// CollectAllSessions returns summary stats for every active tmux session.
func (c *Collector) CollectAllSessions() ([]SessionData, error) {
	sessions, err := c.tmux.ListSessions()
	if err != nil {
		return nil, err
	}
	result := make([]SessionData, 0, len(sessions))
	for _, name := range sessions {
		sd := c.collectSession(name)
		result = append(result, sd)
	}
	return result, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// internal

func (c *Collector) collectSession(name string) SessionData {
	windows, err := c.tmux.ListWindows(name)
	if err != nil {
		return SessionData{Name: name}
	}
	sd := SessionData{
		Name:        name,
		WindowCount: len(windows),
	}
	for _, w := range windows {
		wd := c.collectWindowForSession(name, w)
		sd.Windows = append(sd.Windows, wd)
		sd.CPUTotal += wd.CPUTotal
		sd.MemTotal += wd.MemTotal
		sd.ProcessCount += wd.ProcessCount
	}
	return sd
}

func (c *Collector) collectWindowForSession(session string, w itx.Window) WindowData {
	wd := WindowData{
		Name:  w.Name,
		Index: w.Index,
		Panes: c.listPanes(session, w.Index),
	}
	for _, p := range wd.Panes {
		wd.Processes = append(wd.Processes, Process{
			PID:     0,
			Command: fmt.Sprintf("── pane %d ──", p.Index),
			Depth:   -1,
		})
		procs := c.buildTree(p.PID, 0, -1, nil)
		for _, pp := range procs {
			wd.CPUTotal += pp.CPUPercent
			wd.MemTotal += pp.MemRSS
			wd.ProcessCount++
		}
		wd.Processes = append(wd.Processes, procs...)
	}
	return wd
}

// buildTree recursively builds the process tree rooted at pid.
func (c *Collector) buildTree(pid, depth, parentPID int, siblings []int) []Process {
	if !pidExists(pid) {
		return nil
	}

	short, full := readCmdline(pid)
	ppid := readPPID(pid)
	if parentPID >= 0 {
		ppid = parentPID
	}

	children := readChildren(pid)

	// Determine is_last_child from the sibling list passed by the parent.
	isLast := false
	if len(siblings) > 0 {
		isLast = siblings[len(siblings)-1] == pid
	} else if depth == 0 {
		// root of a pane — treat as last for prefix purposes
		isLast = true
	}

	proc := Process{
		PID:         pid,
		PPID:        ppid,
		Command:     short,
		FullCmdline: full,
		CPUPercent:  c.measureCPU(pid),
		MemRSS:      readMemRSS(pid),
		MemPercent:  c.MemPercent(readMemRSS(pid)),
		Depth:       depth,
		IsLastChild: isLast,
		HasChildren: len(children) > 0,
		Status:      readState(pid),
	}

	result := []Process{proc}
	for _, child := range children {
		result = append(result, c.buildTree(child, depth+1, pid, children)...)
	}
	return result
}

// measureCPU returns the CPU% for pid using delta of /proc/PID/stat ticks.
func (c *Collector) measureCPU(pid int) float64 {
	now := nowTS()
	ticks := readStatTicks(pid)
	curr := cpuSample{ts: now, totalTicks: ticks}

	c.mu.Lock()
	prev, ok := c.cpuBaseline[pid]
	c.cpuBaseline[pid] = curr
	c.mu.Unlock()

	if !ok {
		return 0
	}
	pct := cpuPercent(prev, curr)
	if pct < 0 {
		return 0
	}
	return pct
}

// sampleTree seeds the baseline for pid and all its children (used in WarmUp).
func (c *Collector) sampleTree(pid int) {
	if !pidExists(pid) {
		return
	}
	ticks := readStatTicks(pid)
	c.mu.Lock()
	c.cpuBaseline[pid] = cpuSample{ts: nowTS(), totalTicks: ticks}
	c.mu.Unlock()
	for _, child := range readChildren(pid) {
		c.sampleTree(child)
	}
}

// MemPercent returns the percentage of total system RAM that rss bytes represents.
func (c *Collector) MemPercent(rss int64) float64 {
	if c.totalRAM == 0 {
		return 0
	}
	return float64(rss) / float64(c.totalRAM) * 100.0
}

// ──────────────────────────────────────────────────────────────────────────────
// Panes

// listPanes returns the panes of a window. The result is not cached: each
// cache key is visited exactly once per collection cycle, and the cache TTL
// was shorter than the refresh interval, so it never produced a hit.
func (c *Collector) listPanes(session string, windowIndex int) []itx.PaneInfo {
	panes, err := c.tmux.ListPanes(session, windowIndex)
	if err != nil {
		return nil
	}
	return panes
}

// panePIDs returns the shell PIDs of every pane in a window.
func (c *Collector) panePIDs(session string, windowIndex int) []int {
	panes := c.listPanes(session, windowIndex)
	pids := make([]int, len(panes))
	for i, p := range panes {
		pids[i] = p.PID
	}
	return pids
}
