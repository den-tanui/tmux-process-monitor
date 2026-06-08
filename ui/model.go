// Package ui contains the bubbletea TUI model and all view renderers.
package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

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
)

// String constants for key matching.
const (
	keyEnter = "enter"
	keyEsc   = "esc"
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

	// ── Sidebar ────────────────────────────────────────────────────────
	sidebarOpen         bool
	sidebarContent      string
	sidebarPID          int
	sidebarScrollOffset int
	frozen              bool
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

		var cmd tea.Cmd
		if m.sidebarOpen {
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
		if m.sidebarOpen && msg.pid == m.sidebarPID {
			m.sidebarContent = msg.content
		}
		return m, nil

	case witrMsg:
		m.detailView.SetContent(string(msg))
		m.detailReady = true
		return m, nil

	case tea.MouseMsg:
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
			m.mode = m.prevMode
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
		found := false
		for i, p := range procs {
			if p.PID == 0 && p.Command == targetCmd {
				found = true
				continue
			}
			if found && p.PID != 0 {
				m.selectedProc = i
				m.browsingProcs = true
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

// ──────────────────────────────────────────────────────────────────────────────
// Key handling

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Signal input mode swallows most keys.
	if m.inputMode == InputSignal {
		return m.handleSignalInput(msg)
	}

	switch {
	case msg.String() == "ctrl+c", msg.String() == "q", msg.String() == "Q":
		return m, tea.Quit

	case msg.String() == "?":
		if m.mode == ViewHelp {
			m.mode = m.prevMode
		} else {
			m.prevMode = m.mode
			m.mode = ViewHelp
		}

	case msg.String() == "o":
		if m.mode == ViewOverview {
			m.mode = ViewMain
		} else {
			m.mode = ViewOverview
			return m, collectSessionsCmd(m.coll)
		}

	case msg.String() == "tab":
		m.sidebarOpen = !m.sidebarOpen

	case msg.String() == " ":
		m.frozen = !m.frozen
		if !m.frozen {
			return m, collectSessionsCmd(m.coll)
		}

	case msg.String() == keyEsc:
		switch m.mode {
		case ViewHelp:
			m.mode = m.prevMode
		case ViewOverview:
			m.mode = ViewMain
		}

	case msg.String() == "left", msg.String() == "h", msg.String() == "H":
		if m.mode == ViewOverview {
			break
		}
		m.prevTab()

	case msg.String() == "right", msg.String() == "l", msg.String() == "L":
		if m.mode == ViewOverview {
			break
		}
		m.nextTab()

	case msg.String() == "down", msg.String() == "j":
		if m.mode == ViewOverview {
			m.browsingSession = true
			m.selectedSession = (m.selectedSession + 1) % max1(len(m.sessions))
		} else {
			m.browsingProcs = true
			if m.currentTab < len(m.windows) {
				procs := m.windows[m.currentTab].Processes
				m.selectedProc = (m.selectedProc + 1) % max1(len(procs))
			}
		}

	case msg.String() == "up", msg.String() == "k":
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
				if m.selectedProc > 0 {
					m.selectedProc--
				} else {
					m.selectedProc = max0(len(procs) - 1)
				}
			}
		}

	case msg.String() == keyEnter:
		switch m.mode {
		case ViewOverview:
			if m.browsingSession && m.selectedSession < len(m.sessions) {
				m.session = m.sessions[m.selectedSession].Name
				m.coll.SetSession(m.session)
				m.mode = ViewMain
				m.currentTab = 0
				m.browsingSession = false
				return m, collectSessionsCmd(m.coll)
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
					return m, m.runWitrCmd(p)
				}
			}
		}

	case msg.String() == "x", msg.String() == "X":
		if m.browsingProcs {
			return m, m.sigTermCmd()
		}

	case msg.String() == "s":
		if m.browsingProcs {
			m.inputMode = InputSignal
			m.inputBuffer = ""
		}

	case msg.String() == "y":
		if m.browsingProcs {
			if cmd := m.selectedCommand(); cmd != "" {
				return m, copyToClipboard(cmd)
			}
		}

	case msg.String() == "Y":
		if m.browsingProcs {
			if pid := m.selectedPID(); pid > 0 {
				return m, copyToClipboard(fmt.Sprintf("%d", pid))
			}
		}

	case msg.String() == "alt+left", msg.String() == "alt+h":
		if m.hScrollOff > 0 {
			m.hScrollOff -= 10
		}

	case msg.String() == "alt+right", msg.String() == "alt+l":
		m.hScrollOff += 10
	}

	return m, nil
}

func (m *Model) nextTab() {
	if len(m.windows) > 0 {
		m.currentTab = (m.currentTab + 1) % len(m.windows)
		m.selectedProc = 0
	}
}

func (m *Model) prevTab() {
	if len(m.windows) > 0 {
		m.currentTab = (m.currentTab - 1 + len(m.windows)) % len(m.windows)
		m.selectedProc = 0
	}
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
	case "backspace":
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

func (m Model) runWitrCmd(p collector.Process) tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("witr", "--pid", fmt.Sprintf("%d", p.PID), "--verbose").CombinedOutput()
		if err != nil {
			if strings.Contains(err.Error(), "executable file not found") {
				fallback := fmt.Sprintf(
					"witr is not installed. Showing basic process information.\n"+
						"(Install 'witr' and add it to your PATH for advanced diagnostics.)\n\n"+
						"Command: %s\n"+
						"Full Cmdline: %s\n\n"+
						"PID: %d\n"+
						"PPID: %d\n\n"+
						"CPU Usage: %.1f%%\n"+
						"Memory RSS: %d MB (%.1f%%)\n"+
						p.Command, p.FullCmdline, p.PID, p.PPID, p.CPUPercent, p.MemRSS/1024/1024, p.MemPercent,
				)
				return witrMsg(fallback)
			}
			return witrMsg(fmt.Sprintf("witr error: %v\n%s", err, string(out)))
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
		exec.Command("kill", "-15", fmt.Sprintf("%d", pid)).Run() //nolint:errcheck
		return statusMsg(fmt.Sprintf("Sent SIGTERM to PID %d", pid))
	}
}

func (m Model) sendSignalCmd(sig int) tea.Cmd {
	pid := m.selectedPID()
	if pid <= 0 {
		return nil
	}
	return func() tea.Msg {
		exec.Command("kill", fmt.Sprintf("-%d", sig), fmt.Sprintf("%d", pid)).Run() //nolint:errcheck
		return statusMsg(fmt.Sprintf("Sent signal %d to PID %d", sig, pid))
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
	StyleValue = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff87"))
	// StyleSep is the dim purple style used for separators.
	StyleSep = lipgloss.NewStyle().Foreground(lipgloss.Color("#444488"))
	// StyleSelected is the reversed bold style for selected items.
	StyleSelected = lipgloss.NewStyle().Reverse(true).Bold(true)
	// StyleTree is the cyan bold style for tree connectors.
	StyleTree = lipgloss.NewStyle().Foreground(lipgloss.Color("#5fd7ff")).Bold(true)
	// StyleHigh is the red style for high-usage values.
	StyleHigh = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f5f"))
	// StyleFooter is the muted style for the footer line.
	StyleFooter = lipgloss.NewStyle().Foreground(lipgloss.Color("#555577"))
	// StyleStatus is the green bold style for status messages.
	StyleStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff87")).Bold(true)
	// StyleErr is the red bold style for error messages.
	StyleErr = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f5f")).Bold(true)
)

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

	return m.viewMain()
}

// ──────────────────────────────────────────────────────────────────────────────
// Shared rendering helpers

// renderHeader returns the session summary header (2 lines).
func (m Model) renderHeader() string {
	var cpuTotal float64
	var memTotal int64
	var procTotal int
	for _, w := range m.windows {
		cpuTotal += w.CPUTotal
		memTotal += w.MemTotal
		procTotal += w.ProcessCount
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
	if m.statusMsg != "" && time.Now().Before(m.statusExpiry) {
		return StyleStatus.Render(m.statusMsg)
	}

	shortcuts := []string{
		"h/l:win", "j/k:nav", "Ent:view", "tab:side", "space:freeze", "o:all", "x:kill", "s:sig", "?:help", "q:quit",
	}
	joined := strings.Join(shortcuts, "  │  ")
	return StyleFooter.Render(center(joined, m.width))
}

// renderStatsLine returns a beautiful full-width stats line (1 line).
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
	memMB := proc.MemRSS / 1024 / 1024
	baseLeft := fmt.Sprintf("%8d %5.1f%% %7dMB(%4.1f%%) ", proc.PID, proc.CPUPercent, memMB, proc.MemPercent)

	var statusStyle lipgloss.Style
	switch proc.Status {
	case "running":
		statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff87")).Bold(true)
	case "sleeping", "idle":
		statusStyle = StyleValue
	case "stopped", "tracing stop":
		statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00"))
	case "zombie", "dead":
		statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f5f")).Bold(true)
	case "disk sleep":
		statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#af87ff"))
	default:
		statusStyle = StyleValue
	}
	statusStr := fmt.Sprintf("%-10s", proc.Status)
	if len(statusStr) > 10 {
		statusStr = statusStr[:10]
	}
	styledStatus := statusStyle.Render(statusStr)

	uncoloredBase := baseLeft + statusStr + " "
	maxCmdLen := m.width - len(uncoloredBase) - len(prefix) - 2
	if maxCmdLen < 4 {
		maxCmdLen = 4
	}

	cmd := proc.Command

	isSelected := m.browsingProcs && idx == m.selectedProc
	if isSelected {
		// Horizontal scroll for the selected row.
		if len(cmd) > maxCmdLen {
			start := m.hScrollOff
			if start > len(cmd)-maxCmdLen {
				start = len(cmd) - maxCmdLen
			}
			end := start + maxCmdLen
			if end > len(cmd) {
				end = len(cmd)
			}
			cmd = cmd[start:end]
			if start > 0 {
				cmd = "«" + cmd[1:]
			}
			if start+maxCmdLen < len(cmd)+start {
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

	row := fmt.Sprintf("%s%s %s%s",
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

// ── Sidebar Helpers ──────────────────────────────────────────────────

func (m Model) getSelectedProcess() *collector.Process {
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
		out, err := exec.Command("witr", "--pid", fmt.Sprintf("%d", p.PID), "--verbose").CombinedOutput()
		if err != nil {
			if strings.Contains(err.Error(), "executable file not found") {
				fallback := fmt.Sprintf(
					"witr is not installed.\n"+
						"(Install 'witr' for advanced diagnostics.)\n\n"+
						"Command: %s\n"+
						"Full Cmdline: %s\n\n"+
						"PID: %d\n"+
						"PPID: %d\n\n"+
						"CPU Usage: %.1f%%\n"+
						"Memory RSS: %d MB (%.1f%%)\n"+
						p.Command, p.FullCmdline, p.PID, p.PPID, p.CPUPercent, p.MemRSS/1024/1024, p.MemPercent,
				)
				return sidebarMsg{pid: p.PID, content: fallback}
			}
			return sidebarMsg{pid: p.PID, content: fmt.Sprintf("witr error: %v\n%s", err, string(out))}
		}
		return sidebarMsg{pid: p.PID, content: string(out)}
	}
}

func (m Model) triggerSidebar(force bool) (Model, tea.Cmd) {
	if !m.sidebarOpen {
		return m, nil
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
	if m.sidebarPID != p.PID || force {
		m.sidebarScrollOffset = 0
	}
	m.sidebarPID = p.PID
	if !force {
		m.sidebarContent = "Loading witr info..."
	}
	return m, m.runWitrSidebarCmd(*p)
}

func (m Model) getSidebarWidth() int {
	w := m.width / 3
	if w < 35 {
		w = 35
	}
	if w > 50 {
		w = 50
	}
	if w > m.width-40 {
		w = m.width - 40
	}
	return w
}

func (m Model) renderSidebar(width, height int) string {
	var lines []string

	sidebarHeader := StyleHeader.Render(center("Process Diagnostics", width))
	lines = append(lines, sidebarHeader, StyleSep.Render(strings.Repeat("─", width)))

	contentLines := strings.Split(m.sidebarContent, "\n")
	totalContentLines := len(contentLines)
	availLines := height - 2 // header + separator

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

	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}

	return strings.Join(lines[:height], "\n")
}

// wrapANSI wraps an ANSI-styled string to fit within the given width,
// preserving ANSI codes and adding a "↳ " wrap marker on continuation lines.
func wrapANSI(s string, width int) []string {
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
