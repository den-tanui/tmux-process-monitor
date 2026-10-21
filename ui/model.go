// Package ui contains the bubbletea TUI model and all view renderers.
package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/den-tanui/tmux-process-monitor/internal/collector"
)

// ──────────────────────────────────────────────────────────────────────────────
// View modes

// ViewMode identifies which top-level view is displayed.
type ViewMode int

// ViewMode values for the main display mode.
const (
	ViewMain     ViewMode = iota // process list + tabs
	ViewDetail                   // full-screen process detail
	ViewOverview                 // all-sessions table
	ViewHelp                     // help overlay
)

// InputMode identifies an active text input prompt.
type InputMode int

const (
	// InputNone is the default state — no input prompt active.
	InputNone InputMode = iota
	// InputSignal is active when the user is typing a signal number.
	InputSignal
	// InputSearch is active when the user is typing a search query.
	InputSearch
)

// String constants for key matching.
const (
	keyEnter     = "enter"
	keyEsc       = "esc"
	keyDown      = "down"
	keyUp        = "up"
	keyTab       = "tab"
	keyBackspace = "backspace"
	keyHelp      = "?"
	keyCtrlA     = "ctrl+a"
	keyCtrlD     = "ctrl+d"
	keyCtrlE     = "ctrl+e"
	keyCtrlH     = "ctrl+h"
	keyCtrlP     = "ctrl+p"
	keyCtrlS     = "ctrl+s"
	keyCtrlT     = "ctrl+t"
	keyCtrlU     = "ctrl+u"
)

// Colour constants shared by the style definitions below.
const (
	colorGreen  = "#00ff87"
	colorAmber  = "#ffaf00"
	colorRed    = "#ff5f5f"
	colorPurple = "#af87ff"
)

// ──────────────────────────────────────────────────────────────────────────────
// Messages

type tickMsg time.Time

type sessionDataMsg struct{ sessions []collector.SessionData }
type statusMsg string // flash message
type errMsg struct{ err error }
type witrMsg string
type sidebarMsg struct {
	pid     int
	content string
}

type searchResult struct {
	sessionIdx int
	windowIdx  int
	procIdx    int
	proc       collector.Process
	isChild    bool
}

// ──────────────────────────────────────────────────────────────────────────────
// Model

// Model is the root bubbletea model.
type Model struct {
	// ── Configuration ──────────────────────────────────────────────────
	session     string
	refreshRate time.Duration

	// ── Data ───────────────────────────────────────────────────────────
	windows  []collector.WindowData
	sessions []collector.SessionData

	// ── Collector ──────────────────────────────────────────────────────
	coll *collector.Collector

	// ── Navigation ─────────────────────────────────────────────────────
	mode            ViewMode
	prevMode        ViewMode
	currentTab      int
	selectedProc    int
	hScrollOff      int // horizontal scroll for long cmdlines
	browsingProcs   bool
	browsingSession bool
	selectedSession int

	// ── Input ──────────────────────────────────────────────────────────
	inputMode   InputMode
	inputBuffer string

	// ── Window filter ──────────────────────────────────────────────────
	initialWindow      string // navigate to this window on first load (name fallback)
	initialWindowIndex int    // navigate to this window on first load (index, -1 = unset)
	initialPaneIndex   int    // navigate to first process in this pane (-1 = unset)

	// ── Status flash ───────────────────────────────────────────────────
	statusMsg    string
	statusExpiry time.Time

	// ── Terminal size ──────────────────────────────────────────────────
	width  int
	height int

	// ── Error ──────────────────────────────────────────────────────────
	lastErr error

	// ── Detail Viewport ────────────────────────────────────────────────
	detailView  viewport.Model
	detailReady bool

	// ── Help scroll ────────────────────────────────────────────────────
	helpScroll int

	// ── Sidebar ────────────────────────────────────────────────────────
	sidebarOpen         bool
	sidebarContent      string
	sidebarPID          int
	sidebarScrollOffset int
	frozen              bool

	// ── Search ────────────────────────────────────────────────────────
	searchQuery        string
	searchResults      []searchResult
	searchSelected     int
	searchAllSessions  bool
	searchShowChildren bool

	// ── Multi-select ──────────────────────────────────────────────────
	selectedPIDs       map[int]struct{}
	showingMultiSelect bool
	sidebarCache       map[int]string // PID → witr -t output (multi-select)

	// ── PID index ─────────────────────────────────────────────────────
	// pidIndex maps a PID to its process, rebuilt on every data refresh so
	// the sidebar does not have to scan the whole process tree per lookup.
	pidIndex map[int]collector.Process
}

// New constructs and returns an initialised Model.
func New(
	coll *collector.Collector,
	session string,
	initialWindow string,
	initialWindowIndex int,
	initialPaneIndex int,
	refreshRate time.Duration,
) Model {
	vp := viewport.New(80, 24)
	return Model{
		coll:               coll,
		session:            session,
		initialWindow:      initialWindow,
		initialWindowIndex: initialWindowIndex,
		initialPaneIndex:   initialPaneIndex,
		refreshRate:        refreshRate,
		width:              80,
		height:             24,
		detailView:         vp,
		sidebarOpen:        true,
		selectedPIDs:       make(map[int]struct{}),
		sidebarCache:       make(map[int]string),
		pidIndex:           make(map[int]collector.Process),
	}
}

// SetMode allows callers (e.g. main.go) to pre-select the initial view mode.
func (m *Model) SetMode(mode ViewMode) { m.mode = mode }

// ──────────────────────────────────────────────────────────────────────────────
// bubbletea interface

// Init satisfies tea.Model and returns the initial tick command.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		collectSessionsCmd(m.coll),
		tickCmd(m.refreshRate),
	)
}

func collectSessionsCmd(coll *collector.Collector) tea.Cmd {
	return func() tea.Msg {
		sessions, err := coll.CollectAllSessions()
		if err != nil {
			return errMsg{err}
		}
		return sessionDataMsg{sessions}
	}
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ──────────────────────────────────────────────────────────────────────────────
// Update

// Update satisfies tea.Model and handles all messages and key events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.detailView.Width = msg.Width
		// 3 chrome lines in detail view (header + sep + footer)
		m.detailView.Height = msg.Height - 3
		return m, nil

	case tickMsg:
		if m.frozen {
			return m, tickCmd(m.refreshRate)
		}
		return m, tea.Batch(tickCmd(m.refreshRate), collectSessionsCmd(m.coll))

	case sessionDataMsg:
		m.sessions = msg.sessions

		var activeWindows []collector.WindowData
		for _, s := range m.sessions {
			if s.Name == m.session {
				activeWindows = s.Windows
				break
			}
		}
		m = m.applyWindowData(activeWindows)

		// Rebuild the PID lookup table for the sidebar.
		m.rebuildPIDIndex()

		// Auto-select first entry in overview when entering/viewing overview.
		if m.mode == ViewOverview {
			if len(m.sessions) > 0 {
				m.browsingSession = true
				if m.selectedSession < 0 || m.selectedSession >= len(m.sessions) {
					m.selectedSession = 0
				}
			} else {
				m.browsingSession = false
				m.selectedSession = 0
			}
		}

		// Auto-select first process in main view.
		if m.mode == ViewMain {
			m.ensureProcessSelection()
		}

		// Clean up dead PIDs from selection set.
		m = m.cleanupDeadPIDSelections()

		// Rebuild search results if search is active.
		if m.inputMode == InputSearch {
			m.searchResults = m.rebuildSearchResults()
			if m.searchSelected >= len(m.searchResults) {
				m.searchSelected = max0(len(m.searchResults) - 1)
			}
		}

		var cmd tea.Cmd
		if m.sidebarOpen && !m.frozen {
			m, cmd = m.triggerSidebar(true) // force refresh
		}
		return m, cmd

	case statusMsg:
		m.statusMsg = string(msg)
		m.statusExpiry = time.Now().Add(2 * time.Second)
		return m, nil

	case errMsg:
		m.lastErr = msg.err
		return m, nil

	case sidebarMsg:
		if m.frozen {
			return m, nil
		}
		if m.sidebarOpen {
			if m.showingMultiSelect {
				if _, ok := m.selectedPIDs[msg.pid]; ok {
					m.sidebarCache[msg.pid] = msg.content
				}
			} else if msg.pid == m.sidebarPID {
				m.sidebarContent = msg.content
			}
		}
		return m, nil

	case witrMsg:
		m.detailView.SetContent(string(msg))
		m.detailReady = true
		return m, nil

	case tea.MouseMsg:
		if m.mode == ViewHelp {
			//nolint:staticcheck
			if msg.Type == tea.MouseWheelUp {
				if m.helpScroll > 0 {
					m.helpScroll--
				}
			} else if msg.Type == tea.MouseWheelDown { //nolint:staticcheck
				m.helpScroll++
			}
			return m, nil
		}
		if m.mode == ViewDetail {
			var cmd tea.Cmd
			m.detailView, cmd = m.detailView.Update(msg)
			return m, cmd
		}
		if m.sidebarOpen && msg.X >= m.width-m.getSidebarWidth() && msg.Y < m.height-3 {
			//nolint:staticcheck // msg.Type deprecated but MouseAction API not yet stable
			if msg.Type == tea.MouseWheelUp {
				m.sidebarScrollOffset--
				if m.sidebarScrollOffset < 0 {
					m.sidebarScrollOffset = 0
				}
			} else if msg.Type == tea.MouseWheelDown { //nolint:staticcheck
				m.sidebarScrollOffset++
			}
			return m, nil
		}

	case tea.KeyMsg:
		if m.mode == ViewHelp {
			switch msg.String() {
			case "j", keyDown:
				m.helpScroll++
			case "k", keyUp:
				if m.helpScroll > 0 {
					m.helpScroll--
				}
			default:
				m.mode = m.prevMode
			}
			return m, nil
		}
		if m.mode == ViewDetail {
			if msg.String() == "q" || msg.String() == keyEsc || msg.String() == keyEnter {
				m.mode = ViewMain
				return m, nil
			}
			var cmd tea.Cmd
			m.detailView, cmd = m.detailView.Update(msg)
			return m, cmd
		}
		newModel, cmd := m.handleKey(msg)
		if model, ok := newModel.(Model); ok {
			var sidebarCmd tea.Cmd
			model, sidebarCmd = model.triggerSidebar(false)
			return model, tea.Batch(cmd, sidebarCmd)
		}
		return newModel, cmd
	}

	return m, nil
}

// applyWindowData updates window data while preserving the current tab selection.
func (m Model) applyWindowData(windows []collector.WindowData) Model {
	if len(windows) == 0 {
		m.windows = windows
		return m
	}

	// First load: navigate to initial window (index preferred, name fallback).
	if len(m.windows) == 0 {
		if m.initialWindowIndex >= 0 {
			for i, w := range windows {
				if w.Index == m.initialWindowIndex {
					m.currentTab = i
					break
				}
			}
		} else if m.initialWindow != "" {
			for i, w := range windows {
				if w.Name == m.initialWindow {
					m.currentTab = i
					break
				}
			}
		}
		m.initialWindowIndex = -1
		m.initialWindow = ""
	} else if len(m.windows) > 0 {
		// Preserve by index, then by name.
		oldName := ""
		oldIdx := -1
		if m.currentTab < len(m.windows) {
			oldName = m.windows[m.currentTab].Name
			oldIdx = m.windows[m.currentTab].Index
		}
		found := false
		if oldIdx >= 0 {
			for i, w := range windows {
				if w.Index == oldIdx {
					m.currentTab = i
					found = true
					break
				}
			}
		}
		if !found && oldName != "" {
			for i, w := range windows {
				if w.Name == oldName {
					m.currentTab = i
					found = true
					break
				}
			}
		}
		if !found {
			m.currentTab = clamp(m.currentTab, 0, len(windows)-1)
		}
	}

	m.windows = windows

	// First load: auto-select first process after the target pane's separator.
	if m.initialPaneIndex >= 0 && m.currentTab < len(m.windows) {
		procs := m.windows[m.currentTab].Processes
		targetCmd := fmt.Sprintf("── pane %d ──", m.initialPaneIndex)
		for i, p := range procs {
			if p.PID == 0 && p.Command == targetCmd {
				if j := firstRealProcessIndex(procs[i+1:]); j >= 0 {
					m.selectedProc = i + 1 + j
					m.browsingProcs = true
				}
				break
			}
		}
		m.initialPaneIndex = -1
	}

	// Clamp selected process.
	if m.currentTab < len(m.windows) {
		procs := m.windows[m.currentTab].Processes
		m.selectedProc = clamp(m.selectedProc, 0, max0(len(procs)-1))
	}
	return m
}

// ensureProcessSelection keeps the process cursor on a real process row.
//
// When nothing has been selected yet (browsingProcs is false) it auto-selects
// the first real process of the current window, so entering a view always
// highlights an entry. Once the user has navigated, the existing index is
// preserved and only nudged to the nearest real process if it landed on a
// pane separator row.
func (m *Model) ensureProcessSelection() {
	if m.currentTab >= len(m.windows) {
		m.browsingProcs = false
		return
	}
	procs := m.windows[m.currentTab].Processes
	if len(procs) == 0 {
		m.browsingProcs = false
		return
	}

	if !m.browsingProcs {
		if i := firstRealProcessIndex(procs); i >= 0 {
			m.selectedProc = i
			m.browsingProcs = true
			return
		}
		m.browsingProcs = false
		return
	}

	m.selectedProc = clamp(m.selectedProc, 0, len(procs)-1)
	if procs[m.selectedProc].PID != 0 {
		return
	}
	// The cursor landed on a separator: move to the nearest real process,
	// preferring the one below it so the view does not jump to the top.
	for i := m.selectedProc + 1; i < len(procs); i++ {
		if procs[i].PID != 0 {
			m.selectedProc = i
			return
		}
	}
	for i := m.selectedProc - 1; i >= 0; i-- {
		if procs[i].PID != 0 {
			m.selectedProc = i
			return
		}
	}
	m.browsingProcs = false
}

// ──────────────────────────────────────────────────────────────────────────────
// Search

// rebuildSearchResults rebuilds the search results slice based on the current
// query, scope, and children toggle.
func (m Model) rebuildSearchResults() []searchResult {
	q := strings.ToLower(strings.TrimSpace(m.searchQuery))
	if q == "" {
		return nil
	}

	type match struct {
		sr   searchResult
		tier int // 0=exact, 1=prefix, 2=substring
	}

	raw := make([]match, 0, 256)

	// Determine scope: current windows or all sessions.
	type winRef struct {
		sessionIdx int
		windowIdx  int
		procs      []collector.Process
	}
	var refs []winRef
	if m.searchAllSessions {
		for si, sd := range m.sessions {
			for wi, w := range sd.Windows {
				refs = append(refs, winRef{sessionIdx: si, windowIdx: wi, procs: w.Processes})
			}
		}
	} else if m.currentTab < len(m.windows) {
		refs = append(refs, winRef{sessionIdx: -1, windowIdx: m.currentTab, procs: m.windows[m.currentTab].Processes})
	} else {
		// The monitored session can disappear, leaving no windows to search.
		refs = append(refs, winRef{sessionIdx: -1, windowIdx: 0})
	}

	for _, ref := range refs {
		for pi, p := range ref.procs {
			if p.PID == 0 {
				continue
			}
			tier := matchField(q, p.Command)
			if tier < 0 {
				tier = matchField(q, p.FullCmdline)
			}
			if tier < 0 {
				continue
			}
			raw = append(raw, match{
				sr:   searchResult{sessionIdx: ref.sessionIdx, windowIdx: ref.windowIdx, procIdx: pi, proc: p},
				tier: tier,
			})
		}
	}

	// Sort: tier ascending, then CPU% descending. The sort is stable so
	// that processes of the same tier keep their tree order.
	sort.SliceStable(raw, func(i, j int) bool {
		if raw[i].tier != raw[j].tier {
			return raw[i].tier < raw[j].tier
		}
		return raw[i].sr.proc.CPUPercent > raw[j].sr.proc.CPUPercent
	})

	results := make([]searchResult, 0, len(raw))
	for _, m := range raw {
		results = append(results, m.sr)
	}

	// Expand children if toggle is on.
	if m.searchShowChildren {
		// Build a lookup: (sessionIdx, windowIdx) → processes
		type procListKey struct{ si, wi int }
		procLists := make(map[procListKey][]collector.Process)
		for _, ref := range refs {
			procLists[procListKey{ref.sessionIdx, ref.windowIdx}] = ref.procs
		}

		// Build set of directly-matched PIDs to avoid duplicates during expansion.
		directPIDs := make(map[int]bool)
		for _, sr := range results {
			if !sr.isChild {
				directPIDs[sr.proc.PID] = true
			}
		}

		expanded := make([]searchResult, 0, len(results))
		for _, sr := range results {
			expanded = append(expanded, sr)
			if sr.isChild {
				continue
			}
			key := procListKey{sr.sessionIdx, sr.windowIdx}
			procs := procLists[key]
			rootDepth := sr.proc.Depth
			for ri := sr.procIdx + 1; ri < len(procs); ri++ {
				cp := procs[ri]
				if cp.PID == 0 || cp.Depth <= rootDepth {
					break
				}
				if directPIDs[cp.PID] {
					continue
				}
				expanded = append(expanded, searchResult{
					sessionIdx: sr.sessionIdx,
					windowIdx:  sr.windowIdx,
					procIdx:    ri,
					proc:       cp,
					isChild:    true,
				})
			}
		}
		results = expanded
	}

	return results
}

// ──────────────────────────────────────────────────────────────────────────────
// Search input handling

func (m Model) handleSearchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		m.inputMode = InputNone
		m.searchQuery = ""
		m.searchResults = nil
		m.searchSelected = 0
		m.selectedPIDs = make(map[int]struct{})
		m.sidebarCache = make(map[int]string)
		m.showingMultiSelect = false
		if m.mode == ViewMain || m.mode == ViewOverview {
			m.mode = m.prevMode
		}
		return m, nil

	case keyEnter:
		if len(m.searchResults) > 0 && m.searchSelected < len(m.searchResults) {
			return m.commitSearch()
		}
		return m, nil

	case keyBackspace:
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
		}
		m.searchResults = m.rebuildSearchResults()
		if m.searchSelected >= len(m.searchResults) {
			m.searchSelected = max0(len(m.searchResults) - 1)
		}
		return m, nil

	case keyDown, "j":
		if len(m.searchResults) > 0 {
			m.searchSelected = (m.searchSelected + 1) % len(m.searchResults)
		}
		return m, nil

	case keyUp, "k":
		if len(m.searchResults) > 0 {
			if m.searchSelected > 0 {
				m.searchSelected--
			} else {
				m.searchSelected = len(m.searchResults) - 1
			}
		}
		return m, nil

	case keyTab:
		if len(m.searchResults) > 0 && m.searchSelected < len(m.searchResults) {
			sr := m.searchResults[m.searchSelected]
			pid := sr.proc.PID
			if _, ok := m.selectedPIDs[pid]; ok {
				delete(m.selectedPIDs, pid)
				delete(m.sidebarCache, pid)
				if len(m.selectedPIDs) == 0 {
					m.showingMultiSelect = false
				}
				return m, nil
			}
			m.selectedPIDs[pid] = struct{}{}
			m.showingMultiSelect = true
			if !m.sidebarOpen {
				m.sidebarOpen = true
			}
			return m, m.searchWitrCmd(pid)
		}
		return m, nil

	case keyCtrlA:
		for _, sr := range m.searchResults {
			m.selectedPIDs[sr.proc.PID] = struct{}{}
		}
		if len(m.searchResults) > 0 {
			m.showingMultiSelect = true
			if !m.sidebarOpen {
				m.sidebarOpen = true
			}
		}
		return m, nil

	case keyCtrlD:
		m.selectedPIDs = make(map[int]struct{})
		m.sidebarCache = make(map[int]string)
		m.showingMultiSelect = false
		return m, nil

	case keyCtrlU:
		// Invert: build set of all result PIDs, flip each.
		pidSet := make(map[int]bool)
		for _, sr := range m.searchResults {
			_, sel := m.selectedPIDs[sr.proc.PID]
			pidSet[sr.proc.PID] = sel
		}
		for pid, sel := range pidSet {
			if sel {
				delete(m.selectedPIDs, pid)
			} else {
				m.selectedPIDs[pid] = struct{}{}
			}
		}
		if len(m.selectedPIDs) > 0 {
			m.showingMultiSelect = true
			if !m.sidebarOpen {
				m.sidebarOpen = true
			}
		} else {
			m.showingMultiSelect = false
		}
		return m, nil

	case keyCtrlE:
		if len(m.searchResults) > 0 && m.searchSelected < len(m.searchResults) {
			sr := m.searchResults[m.searchSelected]
			return m, m.toggleDescendants(sr)
		}
		return m, nil

	case keyCtrlS:
		m.searchAllSessions = !m.searchAllSessions
		m.searchResults = m.rebuildSearchResults()
		if m.searchSelected >= len(m.searchResults) {
			m.searchSelected = max0(len(m.searchResults) - 1)
		}
		return m, nil

	case keyCtrlT:
		m.searchShowChildren = !m.searchShowChildren
		m.searchResults = m.rebuildSearchResults()
		if m.searchSelected >= len(m.searchResults) {
			m.searchSelected = max0(len(m.searchResults) - 1)
		}
		return m, nil

	case keyCtrlH:
		m.inputMode = InputNone
		m.searchQuery = ""
		m.searchResults = nil
		m.searchSelected = 0
		m.selectedPIDs = make(map[int]struct{})
		m.sidebarCache = make(map[int]string)
		m.showingMultiSelect = false
		m.mode = ViewHelp
		m.helpScroll = 0
		return m, nil

	case keyCtrlP:
		m.sidebarOpen = !m.sidebarOpen
		return m, nil

	default:
		// Append printable character to query.
		s := msg.String()
		if len(s) == 1 && s[0] >= 32 && s[0] <= 126 {
			m.searchQuery += s
			m.searchResults = m.rebuildSearchResults()
			if m.searchSelected >= len(m.searchResults) {
				m.searchSelected = max0(len(m.searchResults) - 1)
			}
		}
		return m, nil
	}
}

func (m Model) commitSearch() (tea.Model, tea.Cmd) {
	if m.searchSelected >= len(m.searchResults) {
		return m, nil
	}
	sr := m.searchResults[m.searchSelected]

	// Switch to the correct session if needed.
	if sr.sessionIdx >= 0 && sr.sessionIdx < len(m.sessions) {
		m.session = m.sessions[sr.sessionIdx].Name
		m.coll.SetSession(m.session)
	}

	// Update windows from the target session.
	for _, s := range m.sessions {
		if s.Name == m.session {
			m = m.applyWindowData(s.Windows)
			break
		}
	}

	// Navigate to the correct window and process.
	m.currentTab = sr.windowIdx
	m.selectedProc = sr.procIdx
	m.browsingProcs = true

	// Exit search mode.
	m.inputMode = InputNone
	m.searchQuery = ""
	m.searchResults = nil
	m.searchSelected = 0

	// Open detail view.
	m.mode = ViewDetail
	m.detailReady = false
	return m, m.runWitrCmd(sr.proc)
}

func (m Model) searchWitrCmd(pid int) tea.Cmd {
	return func() tea.Msg {
		if pid <= 0 {
			// Fire for all selected PIDs.
			return nil
		}
		cmd := exec.Command("script", "-qec", fmt.Sprintf("witr -t --pid %d", pid), "/dev/null")
		cmd.Env = append(os.Environ(), "CLICOLOR_FORCE=1", "FORCE_COLOR=1", "COLORTERM=truecolor", "TERM=xterm-256color")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return sidebarMsg{pid: pid, content: fmt.Sprintf("(witr error: %v)", err)}
		}
		return sidebarMsg{pid: pid, content: string(out)}
	}
}

// toggleDescendants toggles selection of the search result's process
// plus all its descendants.
func (m Model) toggleDescendants(sr searchResult) tea.Cmd {
	// Gather PIDs to toggle: the process itself + descendants.
	procs := m.getProcessList(sr)
	if procs == nil {
		return nil
	}
	rootDepth := sr.proc.Depth
	pids := []int{sr.proc.PID}
	for ri := sr.procIdx + 1; ri < len(procs); ri++ {
		cp := procs[ri]
		if cp.PID == 0 || cp.Depth <= rootDepth {
			break
		}
		pids = append(pids, cp.PID)
	}

	// Check if they're all currently selected.
	allSel := true
	for _, pid := range pids {
		if _, ok := m.selectedPIDs[pid]; !ok {
			allSel = false
			break
		}
	}

	var cmds []tea.Cmd
	for _, pid := range pids {
		if allSel {
			delete(m.selectedPIDs, pid)
			delete(m.sidebarCache, pid)
		} else {
			m.selectedPIDs[pid] = struct{}{}
			cmds = append(cmds, m.searchWitrCmd(pid))
		}
	}
	if len(m.selectedPIDs) > 0 {
		m.showingMultiSelect = true
		if !m.sidebarOpen {
			m.sidebarOpen = true
		}
	} else {
		m.showingMultiSelect = false
	}
	return tea.Batch(cmds...)
}

func (m Model) getProcessList(sr searchResult) []collector.Process {
	if sr.sessionIdx >= 0 && sr.sessionIdx < len(m.sessions) {
		sd := m.sessions[sr.sessionIdx]
		if sr.windowIdx < len(sd.Windows) {
			return sd.Windows[sr.windowIdx].Processes
		}
		return nil
	}
	if sr.windowIdx < len(m.windows) {
		return m.windows[sr.windowIdx].Processes
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Key handling

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Signal input mode swallows most keys.
	if m.inputMode == InputSignal {
		return m.handleSignalInput(msg)
	}
	// Search input mode has its own handler.
	if m.inputMode == InputSearch {
		return m.handleSearchInput(msg)
	}

	switch {
	case msg.String() == "ctrl+c", msg.String() == "q", msg.String() == "Q":
		return m, tea.Quit
	}

	// Concern-based dispatch. The four groups own disjoint key sets, so
	// evaluation order cannot change behaviour.
	if nm, cmd, ok := m.handleViewToggleKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.handleMultiSelectKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.handleNavigationKey(msg); ok {
		return nm, cmd
	}
	if nm, cmd, ok := m.handleProcessActionKey(msg); ok {
		return nm, cmd
	}
	return m, nil
}

// handleViewToggleKey handles keys that switch views or toggle global UI
// state: search entry, help overlay, overview, sidebar, freeze and esc.
// It reports whether the key was one of its bindings.
func (m Model) handleViewToggleKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch {
	case msg.String() == "/":
		if m.mode == ViewMain || m.mode == ViewOverview {
			m.inputMode = InputSearch
			m.searchQuery = ""
			m.searchResults = nil
			m.searchSelected = 0
			if m.mode == ViewOverview {
				m.searchAllSessions = true
			} else {
				m.searchAllSessions = false
			}
			m.prevMode = m.mode
		}
		return m, nil, true

	case msg.String() == keyHelp, msg.String() == keyCtrlH:
		if m.mode == ViewHelp {
			m.mode = m.prevMode
		} else {
			m.prevMode = m.mode
			m.mode = ViewHelp
			m.helpScroll = 0
		}
		return m, nil, true

	case msg.String() == "o":
		if m.mode == ViewOverview {
			m.mode = ViewMain
			m.ensureProcessSelection()
			return m, nil, true
		}
		m.mode = ViewOverview
		m.browsingSession = true
		if len(m.sessions) > 0 {
			if m.selectedSession < 0 || m.selectedSession >= len(m.sessions) {
				m.selectedSession = 0
			}
		} else {
			m.selectedSession = 0
		}
		return m, collectSessionsCmd(m.coll), true

	case msg.String() == keyCtrlP:
		m.sidebarOpen = !m.sidebarOpen
		return m, nil, true

	case msg.String() == " ":
		m.frozen = !m.frozen
		if !m.frozen {
			return m, collectSessionsCmd(m.coll), true
		}
		return m, nil, true

	case msg.String() == keyEsc:
		m.selectedPIDs = make(map[int]struct{})
		m.sidebarCache = make(map[int]string)
		m.showingMultiSelect = false
		switch m.mode {
		case ViewHelp:
			m.mode = m.prevMode
		case ViewOverview:
			m.mode = ViewMain
		}
		return m, nil, true
	}
	return m, nil, false
}

// handleMultiSelectKey handles the multi-select bindings (ctrl+a/d/u/e and
// tab), most of them gated on the main view.
func (m Model) handleMultiSelectKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch {
	case msg.String() == keyCtrlA:
		if m.mode == ViewMain && m.currentTab < len(m.windows) {
			procs := m.windows[m.currentTab].Processes
			for _, p := range procs {
				if p.PID != 0 {
					m.selectedPIDs[p.PID] = struct{}{}
				}
			}
			if len(m.selectedPIDs) > 0 {
				m.showingMultiSelect = true
			}
		}
		return m, nil, true

	case msg.String() == keyCtrlD:
		m.selectedPIDs = make(map[int]struct{})
		m.sidebarCache = make(map[int]string)
		m.showingMultiSelect = false
		return m, nil, true

	case msg.String() == keyCtrlU:
		if m.mode == ViewMain && m.currentTab < len(m.windows) {
			procs := m.windows[m.currentTab].Processes
			for _, p := range procs {
				if p.PID == 0 {
					continue
				}
				if _, ok := m.selectedPIDs[p.PID]; ok {
					delete(m.selectedPIDs, p.PID)
					delete(m.sidebarCache, p.PID)
				} else {
					m.selectedPIDs[p.PID] = struct{}{}
				}
			}
			if len(m.selectedPIDs) > 0 {
				m.showingMultiSelect = true
			} else {
				m.showingMultiSelect = false
			}
		}
		return m, nil, true

	case msg.String() == keyCtrlE:
		if m.mode == ViewMain && m.currentTab < len(m.windows) {
			procs := m.windows[m.currentTab].Processes
			if m.selectedProc < len(procs) {
				p := procs[m.selectedProc]
				if p.PID == 0 {
					return m, nil, true
				}
				// Build descendant list.
				descPIDs := []int{p.PID}
				rootDepth := p.Depth
				for ri := m.selectedProc + 1; ri < len(procs); ri++ {
					cp := procs[ri]
					if cp.PID == 0 || cp.Depth <= rootDepth {
						break
					}
					descPIDs = append(descPIDs, cp.PID)
				}
				// Check if all already selected.
				allSel := true
				for _, pid := range descPIDs {
					if _, ok := m.selectedPIDs[pid]; !ok {
						allSel = false
						break
					}
				}
				for _, pid := range descPIDs {
					if allSel {
						delete(m.selectedPIDs, pid)
						delete(m.sidebarCache, pid)
					} else {
						m.selectedPIDs[pid] = struct{}{}
					}
				}
				if len(m.selectedPIDs) > 0 {
					m.showingMultiSelect = true
				} else {
					m.showingMultiSelect = false
				}
			}
		}
		return m, nil, true

	case msg.String() == keyTab:
		if m.mode == ViewMain && m.currentTab < len(m.windows) {
			procs := m.windows[m.currentTab].Processes
			if m.selectedProc < len(procs) {
				pid := procs[m.selectedProc].PID
				if pid == 0 {
					return m, nil, true
				}
				if _, ok := m.selectedPIDs[pid]; ok {
					delete(m.selectedPIDs, pid)
					delete(m.sidebarCache, pid)
					if len(m.selectedPIDs) == 0 {
						m.showingMultiSelect = false
					}
				} else {
					m.selectedPIDs[pid] = struct{}{}
					m.showingMultiSelect = true
				}
			}
		}
		return m, nil, true
	}
	return m, nil, false
}

// handleNavigationKey handles tab navigation, process/session cursor movement
// and enter (open detail / select session). Overview-specific behaviour stays
// behind the same mode checks the original switch used.
func (m Model) handleNavigationKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch {
	case msg.String() == "left", msg.String() == "h", msg.String() == "H":
		if m.mode != ViewOverview {
			m.prevTab()
		}
		return m, nil, true

	case msg.String() == "right", msg.String() == "l", msg.String() == "L":
		if m.mode != ViewOverview {
			m.nextTab()
		}
		return m, nil, true

	case msg.String() == keyDown, msg.String() == "j":
		if m.mode == ViewOverview {
			m.browsingSession = true
			m.selectedSession = (m.selectedSession + 1) % max1(len(m.sessions))
		} else {
			m.browsingProcs = true
			if m.currentTab < len(m.windows) {
				procs := m.windows[m.currentTab].Processes
				if len(procs) > 0 {
					start := m.selectedProc
					for {
						m.selectedProc = (m.selectedProc + 1) % len(procs)
						if procs[m.selectedProc].PID != 0 {
							break
						}
						if m.selectedProc == start {
							break
						}
					}
				}
			}
		}
		return m, nil, true

	case msg.String() == keyUp, msg.String() == "k":
		if m.mode == ViewOverview {
			m.browsingSession = true
			if m.selectedSession > 0 {
				m.selectedSession--
			} else {
				m.selectedSession = max0(len(m.sessions) - 1)
			}
		} else if m.browsingProcs {
			if m.currentTab < len(m.windows) {
				procs := m.windows[m.currentTab].Processes
				if len(procs) > 0 {
					start := m.selectedProc
					for {
						if m.selectedProc > 0 {
							m.selectedProc--
						} else {
							m.selectedProc = len(procs) - 1
						}
						if procs[m.selectedProc].PID != 0 {
							break
						}
						if m.selectedProc == start {
							break
						}
					}
				}
			}
		}
		return m, nil, true

	case msg.String() == keyEnter:
		switch m.mode {
		case ViewOverview:
			if m.browsingSession && m.selectedSession < len(m.sessions) {
				m.session = m.sessions[m.selectedSession].Name
				m.coll.SetSession(m.session)
				m.mode = ViewMain
				m.currentTab = 0
				m.browsingSession = false
				return m, collectSessionsCmd(m.coll), true
			}
		case ViewMain:
			if m.currentTab < len(m.windows) {
				procs := m.windows[m.currentTab].Processes
				if m.selectedProc < len(procs) {
					p := procs[m.selectedProc]
					if p.HasChildren {
						// Toggle tree collapse — for now just reset scroll.
						m.hScrollOff = 0
					}
					m.mode = ViewDetail
					m.detailReady = false
					return m, m.runWitrCmd(p), true
				}
			}
		}
		return m, nil, true
	}
	return m, nil, false
}

// handleProcessActionKey handles signals, clipboard copy and horizontal scroll.
func (m Model) handleProcessActionKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch {
	case msg.String() == "x", msg.String() == "X":
		if m.browsingProcs {
			return m, m.sigTermCmd(), true
		}
		return m, nil, true

	case msg.String() == "s":
		if m.browsingProcs {
			m.inputMode = InputSignal
			m.inputBuffer = ""
		}
		return m, nil, true

	case msg.String() == "y":
		if m.browsingProcs {
			if cmd := m.selectedCommand(); cmd != "" {
				return m, copyToClipboard(cmd), true
			}
		}
		return m, nil, true

	case msg.String() == "Y":
		if m.browsingProcs {
			if pid := m.selectedPID(); pid > 0 {
				return m, copyToClipboard(fmt.Sprintf("%d", pid)), true
			}
		}
		return m, nil, true

	case msg.String() == "alt+left", msg.String() == "alt+h":
		if m.hScrollOff > 0 {
			m.hScrollOff -= 10
		}
		return m, nil, true

	case msg.String() == "alt+right", msg.String() == "alt+l":
		m.hScrollOff += 10
		return m, nil, true
	}
	return m, nil, false
}

func (m *Model) nextTab() {
	if len(m.windows) > 0 {
		m.currentTab = (m.currentTab + 1) % len(m.windows)
		m.skipSeparator()
	}
}

func (m *Model) prevTab() {
	if len(m.windows) > 0 {
		m.currentTab = (m.currentTab - 1 + len(m.windows)) % len(m.windows)
		m.skipSeparator()
	}
}

func (m *Model) skipSeparator() {
	m.selectedProc = 0
	m.browsingProcs = false
	if m.currentTab >= len(m.windows) {
		return
	}
	procs := m.windows[m.currentTab].Processes
	if i := firstRealProcessIndex(procs); i >= 0 {
		m.selectedProc = i
		m.browsingProcs = true
		return
	}
	m.selectedProc = 0
}

func (m Model) selectedProcess() *collector.Process {
	if m.currentTab >= len(m.windows) {
		return nil
	}
	procs := m.windows[m.currentTab].Processes
	if m.selectedProc >= len(procs) {
		return nil
	}
	p := procs[m.selectedProc]
	return &p
}

func (m Model) selectedCommand() string {
	p := m.selectedProcess()
	if p == nil {
		return ""
	}
	return p.FullCmdline
}

func (m Model) selectedPID() int {
	p := m.selectedProcess()
	if p == nil {
		return 0
	}
	return p.PID
}

// ──────────────────────────────────────────────────────────────────────────────
// Signal input

func (m Model) handleSignalInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEnter:
		sigNum := 0
		if _, err := fmt.Sscan(m.inputBuffer, &sigNum); err == nil && sigNum > 0 {
			m.inputMode = InputNone
			m.inputBuffer = ""
			return m, m.sendSignalCmd(sigNum)
		}
		m.inputMode = InputNone
		m.inputBuffer = ""
	case keyEsc:
		m.inputMode = InputNone
		m.inputBuffer = ""
	case keyBackspace:
		if len(m.inputBuffer) > 0 {
			m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
		}
	default:
		// Accept digits only.
		if s := msg.String(); len(s) == 1 && s[0] >= '0' && s[0] <= '9' {
			m.inputBuffer += s
		}
	}
	return m, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Commands (async side effects)

// witrFailure renders the message shown when a witr invocation fails.
//
// witr runs under `script`, so a missing witr binary surfaces as a shell
// exit status of 127, not as "executable file not found". A missing `script`
// binary is a different failure again and is reported separately.
func witrFailure(err error, out []byte, p collector.Process) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 127 {
		return fmt.Sprintf(
			"witr is not installed. Showing basic process information.\n"+
				"(Install 'witr' and add it to your PATH for advanced diagnostics.)\n\n"+
				"Command: %s\n"+
				"Full Cmdline: %s\n\n"+
				"PID: %d\n"+
				"PPID: %d\n\n"+
				"CPU Usage: %.1f%%\n"+
				"Memory RSS: %d MB (%.1f%%)\n",
			p.Command, p.FullCmdline, p.PID, p.PPID, p.CPUPercent, p.MemRSS/1024/1024, p.MemPercent,
		)
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(err, exec.ErrNotFound) {
		return fmt.Sprintf(
			"'script' is not installed, so witr cannot be run.\n"+
				"(Install 'util-linux' for script, or install witr and add it to your PATH.)\n\n"+
				"Command: %s\n"+
				"Full Cmdline: %s\n\n"+
				"PID: %d\n",
			p.Command, p.FullCmdline, p.PID,
		)
	}
	return fmt.Sprintf("witr error: %v\n%s", err, string(out))
}

// killStatusMessage renders the status line for a signal attempt.
func killStatusMessage(err error, signal string, pid int) string {
	if err != nil {
		return fmt.Sprintf("Failed to send %s to PID %d: %v", signal, pid, err)
	}
	return fmt.Sprintf("Sent %s to PID %d", signal, pid)
}

func (m Model) runWitrCmd(p collector.Process) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("script", "-qec", fmt.Sprintf("witr --pid %d --verbose", p.PID), "/dev/null")
		cmd.Env = append(os.Environ(), "CLICOLOR_FORCE=1", "FORCE_COLOR=1", "COLORTERM=truecolor", "TERM=xterm-256color")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return witrMsg(witrFailure(err, out, p))
		}
		return witrMsg(string(out))
	}
}

func (m Model) sigTermCmd() tea.Cmd {
	pid := m.selectedPID()
	if pid <= 0 {
		return nil
	}
	return func() tea.Msg {
		err := exec.Command("kill", "-15", strconv.Itoa(pid)).Run()
		return statusMsg(killStatusMessage(err, "SIGTERM", pid))
	}
}

func (m Model) sendSignalCmd(sig int) tea.Cmd {
	pid := m.selectedPID()
	if pid <= 0 {
		return nil
	}
	return func() tea.Msg {
		err := exec.Command("kill", "-"+strconv.Itoa(sig), strconv.Itoa(pid)).Run()
		return statusMsg(killStatusMessage(err, "signal "+strconv.Itoa(sig), pid))
	}
}

func copyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		tools := [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
		for _, tool := range tools {
			cmd := exec.Command(tool[0], tool[1:]...)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				continue
			}
			if err := cmd.Start(); err != nil {
				continue
			}
			stdin.Write([]byte(text)) //nolint:errcheck
			_ = stdin.Close()
			if err := cmd.Wait(); err == nil {
				return statusMsg("Copied to clipboard")
			}
		}
		return statusMsg("Clipboard: no tool available (wl-copy/xclip/xsel)")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Styles (shared across views)

var (
	// StyleHeader is the bold blue style used for header lines.
	StyleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#87d7ff"))
	// StyleLabel is the gold style used for labels in the detail view.
	StyleLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd700"))
	// StyleValue is the green style used for values in the detail view.
	StyleValue = lipgloss.NewStyle().Foreground(lipgloss.Color(colorGreen))
	// StyleSep is the dim purple style used for separators.
	StyleSep = lipgloss.NewStyle().Foreground(lipgloss.Color("#444488"))
	// StyleSelected is the reversed bold style for selected items.
	StyleSelected = lipgloss.NewStyle().Reverse(true).Bold(true)
	// StyleTree is the cyan bold style for tree connectors.
	StyleTree = lipgloss.NewStyle().Foreground(lipgloss.Color("#5fd7ff")).Bold(true)
	// StyleHigh is the red style for high-usage values.
	StyleHigh = lipgloss.NewStyle().Foreground(lipgloss.Color(colorRed))
	// StyleFooter is the muted style for the footer line.
	StyleFooter = lipgloss.NewStyle().Foreground(lipgloss.Color("#555577"))
	// StyleStatus is the green bold style for status messages.
	StyleStatus = lipgloss.NewStyle().Foreground(lipgloss.Color(colorGreen)).Bold(true)
	// StyleErr is the red bold style for error messages.
	StyleErr = lipgloss.NewStyle().Foreground(lipgloss.Color(colorRed)).Bold(true)
	// StyleSearchMatch is the yellow bold style for matched text in search results.
	StyleSearchMatch = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd700")).Bold(true)
	// StyleStopped is the amber style for stopped/tracing-stop processes.
	StyleStopped = lipgloss.NewStyle().Foreground(lipgloss.Color(colorAmber))
	// StyleDiskSleep is the purple style for disk-sleep processes.
	StyleDiskSleep = lipgloss.NewStyle().Foreground(lipgloss.Color(colorPurple))
)

// statusStyle maps a process status string to its display style. Shared by
// renderProcessRow and renderSearchResultRow.
func statusStyle(status string) lipgloss.Style {
	switch status {
	case "running":
		return StyleStatus
	case "sleeping", "idle":
		return StyleValue
	case "stopped", "tracing stop":
		return StyleStopped
	case "zombie", "dead":
		return StyleErr
	case "disk sleep":
		return StyleDiskSleep
	default:
		return StyleValue
	}
}

// firstRealProcessIndex returns the index of the first process that is not a
// pane separator (PID 0), or -1 when procs holds no real process.
func firstRealProcessIndex(procs []collector.Process) int {
	for i, p := range procs {
		if p.PID != 0 {
			return i
		}
	}
	return -1
}

// ──────────────────────────────────────────────────────────────────────────────
// View dispatch

// View satisfies tea.Model and renders the current UI state.
func (m Model) View() string {
	switch m.mode {
	case ViewHelp:
		return m.viewHelp()
	case ViewOverview:
		return m.viewOverview()
	case ViewDetail:
		return m.viewDetail()
	}

	if m.sidebarOpen {
		sidebarWidth := m.getSidebarWidth()
		if sidebarWidth > 0 {
			leftModel := m
			leftModel.width = m.width - sidebarWidth - 1

			leftView := leftModel.viewMain()

			leftViewLines := strings.Split(leftView, "\n")
			leftViewNoFooter := strings.Join(leftViewLines[:len(leftViewLines)-3], "\n")

			rightView := m.renderSidebar(sidebarWidth, m.height-3)

			var sepLines []string
			for i := 0; i < m.height-3; i++ {
				sepLines = append(sepLines, StyleSep.Render("│"))
			}
			sepStr := strings.Join(sepLines, "\n")

			mainArea := lipgloss.JoinHorizontal(lipgloss.Top, leftViewNoFooter, sepStr, rightView)
			return mainArea + "\n" + m.renderSeparator() + "\n" + m.renderStatsLine() + "\n" + m.renderFooter()
		}
	}

	return m.viewMain()
}

// ──────────────────────────────────────────────────────────────────────────────
// Shared rendering helpers

// renderHeader returns the session summary header (2 lines).
func (m Model) renderHeader() string {
	var cpuTotal float64
	var memTotal int64
	for _, w := range m.windows {
		cpuTotal += w.CPUTotal
		memTotal += w.MemTotal
	}
	memMB := memTotal / 1024 / 1024

	summary := fmt.Sprintf("%s %s  %s  %s %s  %s  %s %s  %s  %s %s",
		StyleLabel.Render("Session:"), StyleValue.Render(m.session),
		StyleSep.Render("|"),
		StyleLabel.Render("Windows:"), StyleValue.Render(fmt.Sprintf("%d", len(m.windows))),
		StyleSep.Render("|"),
		StyleLabel.Render("CPU:"), StyleValue.Render(fmt.Sprintf("%.1f%%", cpuTotal)),
		StyleSep.Render("|"),
		StyleLabel.Render("MEM:"), StyleValue.Render(fmt.Sprintf("%dMB", memMB)),
	)
	return center(summary, m.width)
}

// renderTabs returns the window tab bar (1 line), scrollable horizontally
// when many windows exceed the available width.
func (m Model) renderTabs() string {
	if len(m.windows) == 0 {
		return ""
	}

	type tabItem struct {
		style lipgloss.Style
		label string
		width int
	}
	sep := StyleSep.Render(" │ ")
	sepWidth := lipgloss.Width(sep)

	tabs := make([]tabItem, len(m.windows))
	for i, w := range m.windows {
		label := fmt.Sprintf("%d.%s", w.Index, truncate(w.Name, 10))
		var style lipgloss.Style
		if i == m.currentTab {
			style = StyleSelected
		} else if w.CPUTotal >= 20.0 {
			style = StyleHigh
		} else if w.CPUTotal >= 5.0 {
			style = StyleLabel
		} else {
			style = StyleValue
		}
		tabs[i] = tabItem{
			style: style,
			label: label,
			width: lipgloss.Width(style.Render("[" + label + "]")),
		}
	}

	prefix := "Windows: "
	counter := fmt.Sprintf(" (%d/%d)", m.currentTab+1, len(m.windows))
	avail := m.width - lipgloss.Width(StyleLabel.Render(prefix)) - lipgloss.Width(StyleFooter.Render(counter))

	// Quick path: all tabs fit.
	total := tabs[0].width
	for i := 1; i < len(tabs); i++ {
		total += sepWidth + tabs[i].width
	}
	if total <= avail {
		var parts []string
		for _, t := range tabs {
			parts = append(parts, t.style.Render("["+t.label+"]"))
		}
		return StyleLabel.Render(prefix) + strings.Join(parts, sep) + StyleFooter.Render(counter)
	}

	// Scrollable: anchor at currentTab and expand outward.
	start := m.currentTab
	end := m.currentTab + 1
	used := tabs[m.currentTab].width

	for {
		extended := false
		if end < len(tabs) {
			w := sepWidth + tabs[end].width
			if used+w <= avail || end-start < 2 {
				used += w
				end++
				extended = true
			}
		}
		if start > 0 {
			w := sepWidth + tabs[start-1].width
			if used+w <= avail || end-start < 2 {
				used += w
				start--
				extended = true
			}
		}
		if !extended {
			break
		}
	}

	var parts []string
	for i := start; i < end; i++ {
		parts = append(parts, tabs[i].style.Render("["+tabs[i].label+"]"))
	}
	result := strings.Join(parts, sep)

	if start > 0 {
		result = "«" + result
	}
	if end < len(tabs) {
		result = result + "»"
	}

	return StyleLabel.Render(prefix) + result + StyleFooter.Render(counter)
}

// renderSeparator returns a horizontal rule of width w.
func (m Model) renderSeparator() string {
	return StyleSep.Render(strings.Repeat("─", m.width))
}

// renderFooter returns the key-hint line (1 line).
func (m Model) renderFooter() string {
	if m.inputMode == InputSignal {
		pid := m.selectedPID()
		return StyleStatus.Render(fmt.Sprintf("Send signal to PID %d: [ %s ]", pid, m.inputBuffer))
	}
	if m.inputMode == InputSearch {
		childrenLabel := "show-children"
		if m.searchShowChildren {
			childrenLabel = "hide-children"
		}
		scopeLabel := "all"
		if !m.searchAllSessions {
			scopeLabel = "this-session"
		}
		shortcuts := []string{
			"j/k:nav", "tab:select", "ctrl+t:" + childrenLabel, "ctrl+s:" + scopeLabel,
			"esc:cancel", "ctrl+h:help",
		}
		joined := strings.Join(shortcuts, "  │  ")
		return StyleFooter.Render(center(joined, m.width))
	}
	if m.statusMsg != "" && time.Now().Before(m.statusExpiry) {
		return StyleStatus.Render(m.statusMsg)
	}
	if m.lastErr != nil {
		return StyleErr.Render(center(fmt.Sprintf("error: %v", m.lastErr), m.width))
	}

	shortcuts := []string{
		"h/l:win", "j/k:nav", "space:freeze", "/:search", "ctrl+h:help",
	}
	joined := strings.Join(shortcuts, "  │  ")
	return StyleFooter.Render(center(joined, m.width))
}

// renderStatsLine returns the system stats line.
func (m Model) renderStatsLine() string {
	var cpuTotal float64
	var memTotal int64
	var procTotal int
	for _, s := range m.sessions {
		cpuTotal += s.CPUTotal
		memTotal += s.MemTotal
		procTotal += s.ProcessCount
	}
	memMB := memTotal / 1024 / 1024

	stats := fmt.Sprintf("System CPU: %.1f%%   │   System Memory: %dMB   │   Total Processes: %d", cpuTotal, memMB, procTotal)
	if m.frozen {
		stats = stats + "   │   " + lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00")).Bold(true).Render("❄ FREEZE")
	}
	return center(StyleValue.Render(stats), m.width)
}

// renderSearchBar returns the search bar line (replaces tabs/stats in search mode).
func (m Model) renderSearchBar() string {
	scope := m.session
	if m.searchAllSessions {
		scope = "all sessions"
	}
	query := m.searchQuery
	if query == "" {
		query = "█"
	} else {
		query += "█"
	}
	bar := fmt.Sprintf("Search: [%s]  │  %s", query, scope)
	return center(StyleStatus.Render(bar), m.width)
}

// renderSearchInfoLine returns the search information line (items count + scope).
func (m Model) renderSearchInfoLine() string {
	scope := m.session
	if m.searchAllSessions {
		scope = "all sessions"
	}
	count := len(m.searchResults)
	return center(StyleLabel.Render(fmt.Sprintf("%d items in %s", count, scope)), m.width)
}

// renderProcessList renders the process table for the current window, returning
// (lines, count) where lines are ready to join with \n.
func (m Model) renderProcessList(availLines int) []string {
	if m.currentTab >= len(m.windows) {
		return nil
	}
	w := m.windows[m.currentTab]
	if len(w.Processes) == 0 {
		return []string{StyleSep.Render("  (no processes)")}
	}

	// Header row.
	hdr := StyleTree.Render(fmt.Sprintf("%8s %6s %15s %-10s  %s", "PID", "CPU%", "MEM", "STATUS", "COMMAND"))
	lines := []string{hdr, m.renderSeparator()}

	// Scroll to keep selected in view.
	firstVisible := 0
	maxProc := availLines - 2 // header + separator already consumed
	if m.browsingProcs && m.selectedProc >= maxProc {
		firstVisible = m.selectedProc - maxProc + 1
	}

	for i, proc := range w.Processes {
		if i < firstVisible {
			continue
		}
		if len(lines) >= availLines {
			break
		}
		lines = append(lines, m.renderProcessRow(proc, i, w.Processes))
	}
	return lines
}

// renderProcessRow renders one process row.
func (m Model) renderProcessRow(proc collector.Process, idx int, all []collector.Process) string {
	if proc.PID == 0 {
		return StyleSep.Render(proc.Command)
	}

	prefix := treePrefix(proc, idx, all)
	selMarker := ""
	if _, ok := m.selectedPIDs[proc.PID]; ok {
		selMarker = "● "
	}
	memMB := proc.MemRSS / 1024 / 1024
	baseLeft := fmt.Sprintf("%8d %5.1f%% %7dMB(%4.1f%%) ", proc.PID, proc.CPUPercent, memMB, proc.MemPercent)

	statusStr := fmt.Sprintf("%-10s", proc.Status)
	if len(statusStr) > 10 {
		statusStr = statusStr[:10]
	}
	styledStatus := statusStyle(proc.Status).Render(statusStr)

	uncoloredBase := baseLeft + statusStr + " "
	maxCmdLen := m.width - len(uncoloredBase) - len(prefix) - len(selMarker) - 2
	if maxCmdLen < 4 {
		maxCmdLen = 4
	}

	cmd := proc.Command

	isSelected := m.browsingProcs && idx == m.selectedProc
	if isSelected {
		// Horizontal scroll for the selected row.
		if len(cmd) > maxCmdLen {
			origLen := len(cmd)
			start := m.hScrollOff
			if start > origLen-maxCmdLen {
				start = origLen - maxCmdLen
			}
			if start < 0 {
				start = 0
			}
			end := start + maxCmdLen
			if end > origLen {
				end = origLen
			}
			cmd = cmd[start:end]
			if start > 0 {
				cmd = "«" + cmd[1:]
			}
			if end < origLen && len(cmd) > 0 {
				// The marker replaces the last visible character so the
				// row keeps the same width.
				cmd = cmd[:len(cmd)-1] + "»"
			}
		}
	} else {
		cmd = truncate(cmd, maxCmdLen)
	}

	var cmdStyle lipgloss.Style
	switch {
	case proc.CPUPercent >= 20.0:
		cmdStyle = StyleHigh
	case proc.CPUPercent >= 5.0:
		cmdStyle = StyleLabel
	default:
		cmdStyle = StyleValue
	}

	row := fmt.Sprintf("%s%s%s %s%s",
		selMarker,
		baseLeft,
		styledStatus,
		StyleTree.Render(prefix),
		cmdStyle.Render(cmd),
	)
	if isSelected {
		return StyleSelected.Render(row)
	}
	return row
}

// treePrefix is a direct port of the Python get_tree_prefix() algorithm.
// It computes the ├──/└──/│   /     prefix for a process at a given index.
func treePrefix(proc collector.Process, idx int, all []collector.Process) string {
	depth := proc.Depth
	if depth == 0 {
		return ""
	}
	parts := make([]string, 0, depth)
	for level := 0; level < depth; level++ {
		if level == depth-1 {
			if proc.IsLastChild {
				parts = append(parts, "└──")
			} else {
				parts = append(parts, "├──")
			}
		} else {
			// Check if the ancestor at this level has a sibling after the subtree.
			hasSibling := false
			// Walk back to find ancestor at 'level'.
			ancestorIdx := -1
			for ci := idx; ci >= 0; ci-- {
				if all[ci].Depth == level {
					ancestorIdx = ci
					break
				}
			}
			if ancestorIdx >= 0 {
				subtreeEnd := ancestorIdx
				for si := ancestorIdx + 1; si < len(all); si++ {
					if all[si].Depth > level {
						subtreeEnd = si
					} else {
						break
					}
				}
				for si := subtreeEnd + 1; si < len(all); si++ {
					if all[si].Depth == level {
						hasSibling = true
						break
					}
				}
			}
			if hasSibling {
				parts = append(parts, "│   ")
			} else {
				parts = append(parts, "    ")
			}
		}
	}
	return strings.Join(parts, "")
}

// ──────────────────────────────────────────────────────────────────────────────
// Utility

func center(s string, w int) string {
	vis := lipgloss.Width(s)
	if vis >= w {
		return s
	}
	pad := (w - vis) / 2
	return strings.Repeat(" ", pad) + s
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// matchField returns the match tier (0=exact, 1=prefix, 2=substring, -1=no match)
// for a case-insensitive query against a field value.
//
// It compares without lowercasing the field, which would copy every command
// line on every keystroke.
func matchField(q, field string) int {
	switch {
	case q == "":
		return 0
	case len(field) == len(q) && strings.EqualFold(field, q):
		return 0
	case hasPrefixFold(field, q):
		return 1
	case containsFold(field, q):
		return 2
	}
	return -1
}

// hasPrefixFold reports whether s starts with prefix, ignoring case.
//
// The prefix is walked rune by rune so a multi-byte character is never
// split in the middle.
func hasPrefixFold(s, prefix string) bool {
	if prefix == "" {
		return true
	}
	if len(s) < len(prefix) {
		return false
	}
	for _, r := range prefix {
		if s == "" {
			return false
		}
		rs, size := utf8.DecodeRuneInString(s)
		if unicode.ToLower(rs) != unicode.ToLower(r) {
			return false
		}
		s = s[size:]
	}
	return true
}

// containsFold reports whether substr occurs in s, ignoring case.
func containsFold(s, substr string) bool {
	if substr == "" {
		return true
	}
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if hasPrefixFold(s[i:], substr) {
			return true
		}
	}
	return false
}

// cleanupDeadPIDSelections removes any selected PID that is no longer
// present in the current process data.
func (m Model) cleanupDeadPIDSelections() Model {
	if len(m.selectedPIDs) == 0 {
		return m
	}
	// Collect all currently live PIDs.
	live := make(map[int]bool)
	for _, w := range m.windows {
		for _, p := range w.Processes {
			if p.PID != 0 {
				live[p.PID] = true
			}
		}
	}
	for _, sd := range m.sessions {
		for _, w := range sd.Windows {
			for _, p := range w.Processes {
				if p.PID != 0 {
					live[p.PID] = true
				}
			}
		}
	}
	for pid := range m.selectedPIDs {
		if !live[pid] {
			delete(m.selectedPIDs, pid)
			delete(m.sidebarCache, pid)
		}
	}
	if len(m.selectedPIDs) == 0 {
		m.showingMultiSelect = false
	}
	return m
}

// ── Sidebar Helpers ──────────────────────────────────────────────────

func (m Model) getSelectedProcess() *collector.Process {
	if m.inputMode == InputSearch && len(m.searchResults) > 0 {
		if m.searchSelected < len(m.searchResults) {
			return &m.searchResults[m.searchSelected].proc
		}
		return nil
	}
	if m.currentTab < len(m.windows) {
		procs := m.windows[m.currentTab].Processes
		if m.selectedProc < len(procs) {
			return &procs[m.selectedProc]
		}
	}
	return nil
}

func (m Model) runWitrSidebarCmd(p collector.Process) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("script", "-qec", fmt.Sprintf("witr --pid %d --verbose", p.PID), "/dev/null")
		cmd.Env = append(os.Environ(), "CLICOLOR_FORCE=1", "FORCE_COLOR=1", "COLORTERM=truecolor", "TERM=xterm-256color")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return sidebarMsg{pid: p.PID, content: witrFailure(err, out, p)}
		}
		return sidebarMsg{pid: p.PID, content: string(out)}
	}
}

func (m Model) triggerSidebar(force bool) (Model, tea.Cmd) {
	if !m.sidebarOpen {
		return m, nil
	}

	if m.showingMultiSelect {
		var cmds []tea.Cmd
		for pid := range m.selectedPIDs {
			if _, ok := m.sidebarCache[pid]; !ok || force {
				cmds = append(cmds, m.searchWitrCmd(pid))
			}
		}
		return m, tea.Batch(cmds...)
	}

	p := m.getSelectedProcess()
	if p == nil {
		m.sidebarContent = ""
		m.sidebarPID = 0
		m.sidebarScrollOffset = 0
		return m, nil
	}
	if !force && m.sidebarPID == p.PID && m.sidebarContent != "" && m.sidebarContent != "Loading witr info..." {
		return m, nil
	}
	if m.sidebarPID != p.PID {
		// Only a PID change resets the user's scroll position; a forced
		// refresh (2s tick) of the same process preserves it.
		m.sidebarScrollOffset = 0
	}
	m.sidebarPID = p.PID
	if !force {
		m.sidebarContent = "Loading witr info..."
	}
	return m, m.runWitrSidebarCmd(*p)
}

// getSidebarWidth returns the width of the diagnostics sidebar, or 0 when the
// terminal is too narrow to hold both the sidebar and the main pane.
func (m Model) getSidebarWidth() int {
	const (
		minSidebarWidth = 35
		minMainWidth    = 40
		maxSidebarWidth = 50
	)
	if m.width < minSidebarWidth+minMainWidth {
		return 0
	}
	w := m.width / 3
	if w < minSidebarWidth {
		w = minSidebarWidth
	}
	if w > maxSidebarWidth {
		w = maxSidebarWidth
	}
	if w > m.width-minMainWidth {
		w = m.width - minMainWidth
	}
	if w < 0 {
		w = 0
	}
	return w
}

func (m Model) renderSidebar(width, height int) string {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	var lines []string

	if m.showingMultiSelect && len(m.selectedPIDs) > 0 {
		// Section 1: compact selected list.
		header := StyleHeader.Render(center(fmt.Sprintf("Selected (%d)", len(m.selectedPIDs)), width))
		lines = append(lines, header, StyleSep.Render(strings.Repeat("─", width)))
		for pid := range m.selectedPIDs {
			name := m.pidToName(pid)
			line := fmt.Sprintf("● %s (%d)", name, pid)
			if len(lines) >= height {
				break
			}
			// Prefixed with two spaces: wrap to width-2 so no line exceeds
			// the sidebar width (overflow would skew the joined frame).
			for _, wl := range wrapANSI(line, max0(width-2)) {
				if len(lines) >= height {
					break
				}
				lines = append(lines, "  "+wl)
			}
		}

		if len(lines) < height {
			lines = append(lines, "")
		}

		// Section 2: per-process witr -t views.
		for pid := range m.selectedPIDs {
			if len(lines) >= height {
				break
			}
			name := m.pidToName(pid)
			subHeader := StyleSep.Render(fmt.Sprintf("❯ %s (%d)", name, pid))
			lines = append(lines, subHeader)

			// Full cmdline.
			cmdline := m.pidToCmdline(pid)
			if cmdline != "" {
				wrapped := wrapANSI(cmdline, width)
				for _, wl := range wrapped {
					if len(lines) >= height {
						break
					}
					lines = append(lines, wl)
				}
			}

			// witr -t output from cache.
			content, ok := m.sidebarCache[pid]
			if !ok {
				lines = append(lines, StyleSep.Render("  (loading witr...)"))
			} else {
				contentLines := strings.Split(content, "\n")
				for _, cl := range contentLines {
					if len(lines) >= height {
						break
					}
					// Prefixed with two spaces: wrap to width-2 so no line
					// exceeds the sidebar width.
					wrapped := wrapANSI(cl, max0(width-2))
					for _, wl := range wrapped {
						if len(lines) >= height {
							break
						}
						lines = append(lines, "  "+wl)
					}
				}
			}

			if len(lines) < height {
				lines = append(lines, "")
			}
		}
	} else {
		sidebarHeader := StyleHeader.Render(center("Process Diagnostics", width))
		lines = append(lines, sidebarHeader, StyleSep.Render(strings.Repeat("─", width)))

		contentLines := strings.Split(m.sidebarContent, "\n")
		totalContentLines := len(contentLines)
		availLines := height - 2

		maxOffset := totalContentLines - availLines
		if maxOffset < 0 {
			maxOffset = 0
		}
		if m.sidebarScrollOffset > maxOffset {
			m.sidebarScrollOffset = maxOffset
		}
		if m.sidebarScrollOffset < 0 {
			m.sidebarScrollOffset = 0
		}

		if m.sidebarScrollOffset < len(contentLines) {
			contentLines = contentLines[m.sidebarScrollOffset:]
		} else {
			contentLines = nil
		}

		for _, cl := range contentLines {
			if len(lines) >= height {
				break
			}
			wrapped := wrapANSI(cl, width)
			for _, wl := range wrapped {
				if len(lines) >= height {
					break
				}
				lines = append(lines, wl)
			}
		}
	}

	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}

	return strings.Join(lines[:min(len(lines), height)], "\n")
}

// pidToName returns the short command name for a PID.
func (m Model) pidToName(pid int) string {
	if p, ok := m.pidIndex[pid]; ok {
		return p.Command
	}
	return "?"
}

// pidToCmdline returns the full command line for a PID.
func (m Model) pidToCmdline(pid int) string {
	if p, ok := m.pidIndex[pid]; ok {
		return p.FullCmdline
	}
	return ""
}

// rebuildPIDIndex rebuilds the PID → process lookup table. It covers the
// current window as well as every window of every session, which is what the
// multi-select sidebar needs.
func (m *Model) rebuildPIDIndex() {
	idx := make(map[int]collector.Process, len(m.pidIndex)+64)
	add := func(procs []collector.Process) {
		for _, p := range procs {
			if p.PID != 0 {
				idx[p.PID] = p
			}
		}
	}
	for _, w := range m.windows {
		add(w.Processes)
	}
	for _, sd := range m.sessions {
		for _, w := range sd.Windows {
			add(w.Processes)
		}
	}
	m.pidIndex = idx
}

// stripControl removes C0 control characters (CR, BS, BEL, …) that corrupt
// terminal layout when content is embedded in the frame joined with the
// process list: an embedded \r sends the cursor to column 0 and the padding
// spaces then wipe the process row's left edge. witr output is produced under
// `script -qec`, whose pty emits CRLF line endings. ANSI escape sequences are
// preserved.
func stripControl(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool {
		return r < 0x20 && r != '\x1b' && r != '\n'
	}) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\x1b' && r != '\n' {
			return -1
		}
		return r
	}, s)
}

// wrapANSI wraps an ANSI-styled string to fit within the given width,
// preserving ANSI codes and adding a "↳ " wrap marker on continuation lines.
func wrapANSI(s string, width int) []string {
	s = stripControl(s)
	if lipgloss.Width(s) <= width {
		return []string{padded(s, width)}
	}

	const marker = "\u21b3 "
	mw := lipgloss.Width(marker)
	contWidth := width - mw
	if contWidth < 2 {
		contWidth = 2
	}

	var chunks []string
	var buf strings.Builder
	var openStyle []string // active ANSI style codes to reopen after splits
	chunkWidth := width

	r := strings.NewReader(s)
	for {
		ch, _, err := r.ReadRune()
		if err != nil {
			break
		}

		// ANSI escape sequence
		if ch == '\x1b' {
			seq := string(ch)
			for {
				next, _, err := r.ReadRune()
				if err != nil {
					break
				}
				seq += string(next)
				if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') {
					break
				}
			}
			// Track SGR codes (ending in 'm') for proper reopening.
			if strings.HasSuffix(seq, "m") && len(seq) > 2 {
				code := seq[2 : len(seq)-1]
				if code == "0" || code == "" {
					openStyle = nil // full reset
				} else {
					// Avoid duplicate style codes.
					has := false
					for _, s := range openStyle {
						if s == seq {
							has = true
							break
						}
					}
					if !has {
						openStyle = append(openStyle, seq)
					}
				}
			}
			buf.WriteString(seq)
			continue
		}

		// Check if adding this rune would exceed the current chunk width.
		proposed := lipgloss.Width(buf.String()) + lipgloss.Width(string(ch))
		if proposed > chunkWidth {
			// Close active styles and save chunk.
			if len(openStyle) > 0 {
				buf.WriteString("\x1b[0m")
			}
			chunks = append(chunks, buf.String())
			buf.Reset()
			chunkWidth = contWidth

			// Reopen active styles on the new chunk.
			for _, style := range openStyle {
				buf.WriteString(style)
			}
		}

		buf.WriteRune(ch)
	}

	// Flush remaining content.
	if buf.Len() > 0 {
		if len(openStyle) > 0 {
			buf.WriteString("\x1b[0m")
		}
		chunks = append(chunks, buf.String())
	}

	// Build final result with padding and wrap markers.
	result := make([]string, 0, len(chunks))
	for i, c := range chunks {
		if i == 0 {
			result = append(result, padded(c, width))
		} else {
			result = append(result, marker+padded(c, contWidth))
		}
	}
	return result
}

// padded right-pads an ANSI-styled string to the given visible width.
func padded(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
