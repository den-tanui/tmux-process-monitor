package collector

import (
	"os"
	"strconv"
	"sync"
	"time"

	itx "github.com/den-tanui/tmux-process-monitor/internal/tmux"
)

const paneCacheTTL = 500 * time.Millisecond

// Collector gathers process data for one tmux session.
type Collector struct {
	tmux    *itx.Client
	session string

	mu          sync.RWMutex
	cpuBaseline map[int]cpuSample
	paneCache   map[string]paneCacheEntry

	totalRAM int64 // bytes
}

// New returns an initialised Collector for the given session.
func New(tmuxClient *itx.Client, session string) *Collector {
	return &Collector{
		tmux:        tmuxClient,
		session:     session,
		cpuBaseline: make(map[int]cpuSample),
		paneCache:   make(map[string]paneCacheEntry),
		totalRAM:    totalRAMBytes(),
	}
}

// SetSession changes the active session without creating a new Collector.
func (c *Collector) SetSession(session string) {
	c.mu.Lock()
	c.session = session
	c.mu.Unlock()
}

// WarmUp establishes CPU baselines for all pane processes asynchronously.
// Call once after construction; first Collect() will return 0% CPU until
// the warmup completes (~150ms later).
func (c *Collector) WarmUp() {
	go func() {
		time.Sleep(100 * time.Millisecond)
		windows, err := c.tmux.ListWindows(c.session)
		if err != nil {
			return
		}
		for _, w := range windows {
			pids := c.cachedPanePIDs(w.Index)
			for _, pid := range pids {
				c.sampleTree(pid)
			}
		}
	}()
}

// Collect returns the latest window data for the configured session.
// It is safe to call from the bubbletea update goroutine.
func (c *Collector) Collect() ([]WindowData, error) {
	windows, err := c.tmux.ListWindows(c.session)
	if err != nil {
		return nil, err
	}
	result := make([]WindowData, 0, len(windows))
	for _, w := range windows {
		wd := c.collectWindow(w)
		result = append(result, wd)
	}
	return result, nil
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

func (c *Collector) collectWindow(w itx.Window) WindowData {
	return c.collectWindowForSession(c.session, w)
}

func (c *Collector) collectWindowForSession(session string, w itx.Window) WindowData {
	wd := WindowData{
		Name:     w.Name,
		Index:    w.Index,
		PanePIDs: c.cachedPanePIDsFor(session, w.Index),
	}
	for _, pid := range wd.PanePIDs {
		procs := c.buildTree(pid, 0, -1, nil)
		for _, p := range procs {
			wd.CPUTotal += p.CPUPercent
			wd.MemTotal += p.MemRSS
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
		IsPlugin:    isPluginProcess(pid),
		Status:      readState(pid),
		PluginName:  getPluginName(pid, full),
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

func (c *Collector) MemPercent(rss int64) float64 {
	if c.totalRAM == 0 {
		return 0
	}
	return float64(rss) / float64(c.totalRAM) * 100.0
}

// ──────────────────────────────────────────────────────────────────────────────
// Pane PID cache

func (c *Collector) cachedPanePIDs(windowIndex int) []int {
	return c.cachedPanePIDsFor(c.session, windowIndex)
}

func (c *Collector) cachedPanePIDsFor(session string, windowIndex int) []int {
	key := session + ":" + itoa(windowIndex)

	c.mu.RLock()
	entry, ok := c.paneCache[key]
	c.mu.RUnlock()

	if ok && time.Since(entry.ts) < paneCacheTTL {
		return entry.pids
	}

	pids, err := c.tmux.ListPanePIDs(session, windowIndex)
	if err != nil {
		return nil
	}

	c.mu.Lock()
	c.paneCache[key] = paneCacheEntry{ts: nowTS(), pids: pids}
	c.mu.Unlock()
	return pids
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// CollectSystemPlugins scans /proc to comprehensively detect all active tmux plugins.
// It returns a flattened tree slice of all plugin processes on the system.
func (c *Collector) CollectSystemPlugins() []Process {
	files, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	var pluginRootPIDs []int
	for _, f := range files {
		if !f.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(f.Name())
		if err != nil {
			continue
		}
		if isPluginProcess(pid) {
			pluginRootPIDs = append(pluginRootPIDs, pid)
		}
	}

	// Filter out descendants so we only keep root plugin processes.
	var roots []int
	for _, pid := range pluginRootPIDs {
		ppid := readPPID(pid)
		isRoot := true
		for _, p := range pluginRootPIDs {
			if p == ppid {
				isRoot = false
				break
			}
		}
		if isRoot {
			roots = append(roots, pid)
		}
	}

	var allProcs []Process
	seen := make(map[int]bool)
	for _, rootPID := range roots {
		procs := c.buildTree(rootPID, 0, -1, nil)
		for _, p := range procs {
			if !seen[p.PID] {
				seen[p.PID] = true
				allProcs = append(allProcs, p)
			}
		}
	}

	return allProcs
}

