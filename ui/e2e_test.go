package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/den-tanui/tmux-process-monitor/internal/collector"
)

// testModel builds a Model with pre-seeded process data.
// No tmux, no collector, no external dependencies.
//
// Important: selectedProc starts at 0 (Go zero value) but browsingProcs
// is false. First "j" sets browsingProcs=true and increments selectedProc
// to 1. To test selection at index 0, use the initial state without "j".
func testModel() Model {
	m := New(nil, "test-session", "", -1, -1, 2*time.Second)
	m.width = 120
	m.height = 40
	m.windows = []collector.WindowData{
		{
			Name:         "editor",
			Index:        0,
			CPUTotal:     85.0,
			MemTotal:     350 * 1024 * 1024,
			ProcessCount: 3,
			Processes: []collector.Process{
				{PID: 1001, Command: "nvim", FullCmdline: "/usr/bin/nvim main.go", CPUPercent: 5.0, MemRSS: 50 * 1024 * 1024, Status: "running", Depth: 0, IsLastChild: false, HasChildren: false},
				{PID: 1002, Command: "go build", FullCmdline: "go build ./...", CPUPercent: 80.0, MemRSS: 200 * 1024 * 1024, Status: "running", Depth: 0, IsLastChild: false, HasChildren: false},
				{PID: 1003, Command: "node server.js", FullCmdline: "node server.js --port 8080", CPUPercent: 2.5, MemRSS: 100 * 1024 * 1024, Status: "sleeping", Depth: 0, IsLastChild: true, HasChildren: false},
			},
		},
		{
			Name:         "shell",
			Index:        1,
			CPUTotal:     0.5,
			MemTotal:     10 * 1024 * 1024,
			ProcessCount: 1,
			Processes: []collector.Process{
				{PID: 2001, Command: "bash", FullCmdline: "-bash", CPUPercent: 0.5, MemRSS: 10 * 1024 * 1024, Status: "sleeping", Depth: 0, IsLastChild: true, HasChildren: false},
			},
		},
	}
	m.sessions = []collector.SessionData{
		{
			Name:         "test-session",
			CPUTotal:     85.5,
			MemTotal:     360 * 1024 * 1024,
			ProcessCount: 4,
			Windows:      m.windows,
		},
	}
	m.detailView.Width = 120
	m.detailView.Height = 37
	return m
}

// ── Key navigation ──────────────────────────────────────────────

func TestKeyNav_jk(t *testing.T) {
	m := testModel()

	// selectedProc starts at 0 (Go zero), browsingProcs=false
	// First "j" sets browsingProcs and increments to (0+1)%3 = 1
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = result.(Model)
	if !m.browsingProcs {
		t.Error("expected browsingProcs=true after j")
	}
	if m.selectedProc != 1 {
		t.Errorf("expected selectedProc=1 after first j, got %d", m.selectedProc)
	}

	// Second "j": (1+1)%3 = 2
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = result.(Model)
	if m.selectedProc != 2 {
		t.Errorf("expected selectedProc=2 after second j, got %d", m.selectedProc)
	}

	// Navigate back up: 2 → 1
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = result.(Model)
	if m.selectedProc != 1 {
		t.Errorf("expected selectedProc=1 after k, got %d", m.selectedProc)
	}
}

func TestKeyNav_jk_wrapsAtEnd(t *testing.T) {
	m := testModel()
	procCount := len(m.windows[m.currentTab].Processes) // 3

	// Each "j" increments: 0→1→2→0→1→...
	// After 3 "j"s (procCount), wraps back to 0
	for i := 0; i < procCount; i++ {
		r, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = r.(Model)
	}
	if m.selectedProc != 0 {
		t.Errorf("expected selectedProc=0 after wrap, got %d", m.selectedProc)
	}

	// One more "j": 0→1
	r, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = r.(Model)
	if m.selectedProc != 1 {
		t.Errorf("expected selectedProc=1 after next j, got %d", m.selectedProc)
	}
}

// ── Pane awareness ─────────────────────────────────────────────

func TestPaneNav_jk_skipsSeparators(t *testing.T) {
	m := testModel()
	m.windows[0].Panes = nil
	m.windows[0].Processes = []collector.Process{
		{PID: 0, Command: "── pane 0 ──", Depth: -1},
		{PID: 10, Command: "zsh", Depth: 0},
		{PID: 11, Command: "vim", Depth: 1},
		{PID: 0, Command: "── pane 1 ──", Depth: -1},
		{PID: 20, Command: "bash", Depth: 0},
	}
	m.currentTab = 0
	m.selectedProc = 0
	m.browsingProcs = true

	// j from separator (idx 0): skips it, lands on zsh (idx 1).
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = result.(Model)
	if m.selectedProc != 1 || m.windows[0].Processes[m.selectedProc].PID != 10 {
		t.Errorf("after j: expected selectedProc=1 (PID=10), got selectedProc=%d (PID=%d)", m.selectedProc, m.windows[0].Processes[m.selectedProc].PID)
	}

	// j: zsh → vim
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = result.(Model)
	if m.selectedProc != 2 || m.windows[0].Processes[m.selectedProc].PID != 11 {
		t.Errorf("after j: expected selectedProc=2 (PID=11), got selectedProc=%d (PID=%d)", m.selectedProc, m.windows[0].Processes[m.selectedProc].PID)
	}

	// j: vim → pane 1 separator → skip to bash
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = result.(Model)
	if m.selectedProc != 4 || m.windows[0].Processes[m.selectedProc].PID != 20 {
		t.Errorf("after j: expected selectedProc=4 (PID=20), got selectedProc=%d (PID=%d)", m.selectedProc, m.windows[0].Processes[m.selectedProc].PID)
	}

	// k: bash → pane 1 separator → skip to vim
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = result.(Model)
	if m.selectedProc != 2 || m.windows[0].Processes[m.selectedProc].PID != 11 {
		t.Errorf("after k: expected selectedProc=2 (PID=11), got selectedProc=%d (PID=%d)", m.selectedProc, m.windows[0].Processes[m.selectedProc].PID)
	}
}

func TestPaneSeparator_renders(t *testing.T) {
	m := testModel()
	m.windows[0].Processes = []collector.Process{
		{PID: 0, Command: "── pane 0 ──", Depth: -1},
		{PID: 1, Command: "zsh", Depth: 0},
		{PID: 0, Command: "── pane 1 ──", Depth: -1},
		{PID: 2, Command: "nvim", Depth: 0},
	}
	m.currentTab = 0

	view := m.View()
	if !strings.Contains(view, "── pane 0 ──") {
		t.Errorf("expected pane 0 separator in view, got:\n%s", view)
	}
	if !strings.Contains(view, "── pane 1 ──") {
		t.Errorf("expected pane 1 separator in view, got:\n%s", view)
	}
}

func TestPaneNav_initialPaneIndex(t *testing.T) {
	m := New(nil, "test-session", "", -1, 1, 2*time.Second)
	m.width = 120
	m.height = 40

	sessions := []collector.SessionData{
		{
			Name: "test-session",
			Windows: []collector.WindowData{
				{
					Name:  "testwin",
					Index: 0,
					Panes: nil,
					Processes: []collector.Process{
						{PID: 0, Command: "── pane 0 ──", Depth: -1},
						{PID: 10, Command: "zsh", Depth: 0},
						{PID: 0, Command: "── pane 1 ──", Depth: -1},
						{PID: 20, Command: "bash", Depth: 0},
					},
				},
			},
		},
	}
	result, _ := m.Update(sessionDataMsg{sessions})
	m = result.(Model)

	if !m.browsingProcs {
		t.Error("expected browsingProcs=true after initialPaneIndex auto-select")
	}
	if m.selectedProc != 3 {
		t.Errorf("expected selectedProc=3 (first process in pane 1), got %d", m.selectedProc)
	}
	if m.windows[0].Processes[m.selectedProc].PID != 20 {
		t.Errorf("expected PID=20 at selectedProc, got PID=%d", m.windows[0].Processes[m.selectedProc].PID)
	}
}

// ── Tab navigation ─────────────────────────────────────────────

func TestTabNav_initialWindowIndex(t *testing.T) {
	// Start with initialWindowIndex=1 (should navigate to the window with Index=1).
	m := New(nil, "test-session", "", 1, -1, 2*time.Second)
	m.width = 120
	m.height = 40

	// Simulate first sessionDataMsg arriving via Update.
	sessions := []collector.SessionData{
		{
			Name: "test-session",
			Windows: []collector.WindowData{
				{Name: "editor", Index: 0},
				{Name: "shell", Index: 1},
				{Name: "logs", Index: 2},
			},
		},
	}
	result, _ := m.Update(sessionDataMsg{sessions})
	m = result.(Model)

	if m.currentTab != 1 {
		t.Errorf("expected currentTab=1 for window Index=1, got %d", m.currentTab)
	}
	if m.windows[m.currentTab].Name != "shell" {
		t.Errorf("expected window name 'shell' at tab 1, got %q", m.windows[m.currentTab].Name)
	}

	// Second update should preserve tab by index.
	result, _ = m.Update(sessionDataMsg{sessions})
	m = result.(Model)
	if m.currentTab != 1 {
		t.Errorf("expected currentTab=1 preserved on second update, got %d", m.currentTab)
	}
}

func TestTabNav_initialWindowIndex_baseIndex1(t *testing.T) {
	// Reproduce the user's scenario: base-index=1 (indices start at 1, not 0).
	// User is in window 2 (opencode), passes -i 2.
	m := New(nil, "tmux-process-monitor", "", 2, -1, 2*time.Second)
	m.width = 120
	m.height = 40

	sessions := []collector.SessionData{
		{
			Name: "tmux-process-monitor",
			Windows: []collector.WindowData{
				{Name: "nvim", Index: 1},
				{Name: "opencode", Index: 2},
				{Name: "zsh", Index: 3},
				{Name: "zsh", Index: 4},
			},
		},
	}
	result, _ := m.Update(sessionDataMsg{sessions})
	m = result.(Model)

	if m.currentTab != 1 {
		t.Errorf("expected currentTab=1 (opencode at Index=2), got %d", m.currentTab)
	}
	if m.windows[m.currentTab].Name != "opencode" {
		t.Errorf("expected window 'opencode' at tab, got %q", m.windows[m.currentTab].Name)
	}
}

func TestTabNav_hl(t *testing.T) {
	m := testModel()
	originalTab := m.currentTab

	// Navigate right once: tab 0 → 1
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = result.(Model)
	expected := (originalTab + 1) % len(m.windows)
	if m.currentTab != expected {
		t.Errorf("expected currentTab=%d after l, got %d", expected, m.currentTab)
	}
	if m.selectedProc != 0 {
		t.Errorf("expected selectedProc reset to 0 after tab switch, got %d", m.selectedProc)
	}

	// Navigate left twice: 1 → 0 → 1 (wraps around both ways)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = result.(Model)
	if m.currentTab != 0 {
		t.Errorf("expected currentTab=0 after first h, got %d", m.currentTab)
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = result.(Model)
	if m.currentTab != 1 {
		t.Errorf("expected currentTab=1 after second h (wrap), got %d", m.currentTab)
	}
}

func TestTabNav_noWindows(t *testing.T) {
	m := testModel()
	m.windows = nil

	// Should not panic
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if _, ok := result.(Model); !ok {
		t.Fatal("expected Model after tab nav with no windows")
	}
}

// ── Mode switching ─────────────────────────────────────────────

func TestModeToggle_help(t *testing.T) {
	m := testModel()
	if m.mode != ViewMain {
		t.Fatalf("expected ViewMain initially, got %v", m.mode)
	}

	// Open help
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = result.(Model)
	if m.mode != ViewHelp {
		t.Errorf("expected ViewHelp, got %v", m.mode)
	}

	// Close help with esc
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	m = result.(Model)
	if m.mode != ViewMain {
		t.Errorf("expected ViewMain after esc, got %v", m.mode)
	}
}

func TestModeToggle_overview(t *testing.T) {
	m := testModel()

	// Open overview
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = result.(Model)
	if m.mode != ViewOverview {
		t.Errorf("expected ViewOverview, got %v", m.mode)
	}

	// Close overview
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = result.(Model)
	if m.mode != ViewMain {
		t.Errorf("expected ViewMain after second o, got %v", m.mode)
	}
}

func TestModeToggle_overviewThenEsc(t *testing.T) {
	m := testModel()

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = result.(Model)

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	m = result.(Model)
	if m.mode != ViewMain {
		t.Errorf("expected ViewMain after esc from overview, got %v", m.mode)
	}
}

// ── Freeze ─────────────────────────────────────────────────────

func TestFreezeToggle(t *testing.T) {
	m := testModel()
	if m.frozen {
		t.Fatal("expected frozen=false initially")
	}

	// Freeze
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = result.(Model)
	if !m.frozen {
		t.Error("expected frozen=true after space")
	}

	// View should contain "FREEZE" marker
	view := m.View()
	if !strings.Contains(view, "FREEZE") {
		t.Error("expected FREEZE marker in view when frozen")
	}

	// Unfreeze
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = result.(Model)
	if m.frozen {
		t.Error("expected frozen=false after second space")
	}
}

func TestFreeze_preventsTickUpdate(t *testing.T) {
	m := testModel()

	// Freeze
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = result.(Model)

	// Tick should be swallowed when frozen
	cb, cmd := m.Update(tickMsg{})
	m2 := cb.(Model)
	if !m2.frozen {
		t.Error("expected still frozen after tick")
	}
	if cmd == nil {
		t.Error("expected tick command to still reschedule even when frozen")
	}
}

// ── Tab navigation ignored in overview ─────────────────────────

func TestTabNavigationIgnoredInOverview(t *testing.T) {
	m := testModel()

	// Switch to overview
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = result.(Model)
	overviewTab := m.currentTab

	// Tab keys should be ignored in overview
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	result, _ = result.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = result.(Model)
	if m.currentTab != overviewTab {
		t.Errorf("expected currentTab to remain %d in overview, got %d", overviewTab, m.currentTab)
	}
}

// ── Signal input ───────────────────────────────────────────────

func TestSignalInput(t *testing.T) {
	m := testModel()

	// Select a process first
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = result.(Model)

	// Enter signal mode
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = result.(Model)
	if m.inputMode != InputSignal {
		t.Errorf("expected InputSignal mode, got %v", m.inputMode)
	}

	// Type a signal number
	for _, k := range "9" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = result.(Model)
	}
	if m.inputBuffer != "9" {
		t.Errorf("expected inputBuffer=9, got %q", m.inputBuffer)
	}

	// Press enter (triggers signal send, which calls kill — skipped since no command exec)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(Model)
	if m.inputMode != InputNone {
		t.Errorf("expected InputNone after enter, got %v", m.inputMode)
	}
}

func TestSignalInput_escCancels(t *testing.T) {
	m := testModel()
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = result.(Model)

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = result.(Model)
	if m.inputMode != InputSignal {
		t.Fatalf("expected InputSignal")
	}

	// Type a digit, then esc
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9")})
	m = result.(Model)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	m = result.(Model)

	if m.inputMode != InputNone {
		t.Errorf("expected InputNone after esc, got %v", m.inputMode)
	}
	if m.inputBuffer != "" {
		t.Errorf("expected inputBuffer cleared, got %q", m.inputBuffer)
	}
}

// ── Quit ───────────────────────────────────────────────────────

func TestQuit(t *testing.T) {
	m := testModel()

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	_ = result.(Model)
	if cmd == nil {
		t.Error("expected quit command (not nil) after pressing q")
	}
}

// ── View rendering ─────────────────────────────────────────────

func TestView_containsSessionInHeader(t *testing.T) {
	m := testModel()
	view := m.View()

	if !strings.Contains(view, "test-session") {
		t.Error("expected session name in rendered view")
	}
	if !strings.Contains(view, "editor") {
		t.Error("expected window 'editor' tab in rendered view")
	}
	if !strings.Contains(view, "shell") {
		t.Error("expected window 'shell' tab in rendered view")
	}
}

func TestView_helpMode(t *testing.T) {
	m := testModel()
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = result.(Model)

	view := m.View()
	if !strings.Contains(view, "Keyboard Reference") {
		t.Errorf("expected help content in view when in help mode, got:\n%s", view)
	}
}

// ── Search ─────────────────────────────────────────────────────

func TestSearch_activate(t *testing.T) {
	m := testModel()

	// Press / to activate search.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	if m.inputMode != InputSearch {
		t.Errorf("expected InputSearch, got %v", m.inputMode)
	}
	// Footer should show search keys.
	view := m.View()
	if !strings.Contains(view, "ctrl+t") {
		t.Errorf("expected search footer with ctrl+t, got:\n%s", view)
	}
	if !strings.Contains(view, "esc:cancel") {
		t.Errorf("expected search footer with esc:cancel, got:\n%s", view)
	}
}

func TestSearch_placeholder(t *testing.T) {
	m := testModel()

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	view := m.View()
	if !strings.Contains(view, "type to search") {
		t.Errorf("expected placeholder text, got:\n%s", view)
	}
}

func TestSearch_filterByCommand(t *testing.T) {
	m := testModel()

	// Activate search
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	// Type "nv"
	for _, k := range "nv" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = result.(Model)
	}

	if m.searchQuery != "nv" {
		t.Errorf("expected query 'nv', got %q", m.searchQuery)
	}
	if len(m.searchResults) != 1 {
		t.Fatalf("expected 1 search result for 'nv', got %d", len(m.searchResults))
	}
	if m.searchResults[0].proc.PID != 1001 {
		t.Errorf("expected PID 1001 (nvim), got PID %d", m.searchResults[0].proc.PID)
	}

	// Type more to narrow to no matches
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	m = result.(Model)

	if len(m.searchResults) != 0 {
		t.Errorf("expected 0 matches for 'nvX', got %d", len(m.searchResults))
	}

	view := m.View()
	if !strings.Contains(view, "no matches") {
		t.Errorf("expected no-matches message, got:\n%s", view)
	}
}

func TestSearch_navKeys(t *testing.T) {
	m := testModel()

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	// Type "go" — matches "go build" (PID 1002 via Command) and "nvim" (PID 1001 via FullCmdline "main.go")
	for _, k := range "go" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = result.(Model)
	}

	if len(m.searchResults) != 2 {
		t.Fatalf("expected 2 results for 'go', got %d", len(m.searchResults))
	}

	// j: 0→1
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = result.(Model)
	if m.searchSelected != 1 {
		t.Errorf("expected searchSelected=1 after j, got %d", m.searchSelected)
	}

	// j: 1→0 (wrap)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = result.(Model)
	if m.searchSelected != 0 {
		t.Errorf("expected searchSelected=0 after wrap, got %d", m.searchSelected)
	}

	// k: 0→1 (wrap)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = result.(Model)
	if m.searchSelected != 1 {
		t.Errorf("expected searchSelected=1 after k, got %d", m.searchSelected)
	}

	// Type "nv" instead for 2+ results from Command
	m2 := testModel()
	result2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m2 = result2.(Model)

	for _, k := range "n" {
		result2, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m2 = result2.(Model)
	}

	// "n" matches "nvim" (PID 1001) and "node " (PID 1003) — 2 results
	if len(m2.searchResults) != 2 {
		t.Fatalf("expected 2 results for 'n', got %d", len(m2.searchResults))
	}

	// j: 0→1
	result2, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m2 = result2.(Model)
	if m2.searchSelected != 1 {
		t.Errorf("expected searchSelected=1 after j, got %d", m2.searchSelected)
	}

	// j: 1→0 (wrap)
	result2, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m2 = result2.(Model)
	if m2.searchSelected != 0 {
		t.Errorf("expected searchSelected=0 after wrap, got %d", m2.searchSelected)
	}

	// k: 0→1 (wrap)
	result2, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m2 = result2.(Model)
	if m2.searchSelected != 1 {
		t.Errorf("expected searchSelected=1 after k, got %d", m2.searchSelected)
	}
}

func TestSearch_escCancels(t *testing.T) {
	m := testModel()

	// Activate search and type something.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)
	for _, k := range "nv" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = result.(Model)
	}

	// Select something.
	m.selectedPIDs[1001] = struct{}{}
	m.showingMultiSelect = true

	// Press esc.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	m = result.(Model)

	if m.inputMode != InputNone {
		t.Errorf("expected InputNone after esc, got %v", m.inputMode)
	}
	if m.searchQuery != "" {
		t.Errorf("expected searchQuery cleared, got %q", m.searchQuery)
	}
	if m.searchResults != nil {
		t.Errorf("expected searchResults nil, got %v", m.searchResults)
	}
	if len(m.selectedPIDs) != 0 {
		t.Errorf("expected selections cleared, got %d", len(m.selectedPIDs))
	}
	if m.showingMultiSelect {
		t.Error("expected showingMultiSelect=false after esc")
	}
}

func TestSearch_allSessionsToggle(t *testing.T) {
	m := testModel()

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	if m.searchAllSessions {
		t.Error("expected searchAllSessions=false initially for normal mode")
	}

	// Toggle with ctrl+s
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = result.(Model)
	if !m.searchAllSessions {
		t.Error("expected searchAllSessions=true after ctrl+s")
	}
}

func TestSearch_childrenToggle(t *testing.T) {
	m := testModel()

	// Add a child process to test children mode.
	m.windows[0].Processes = []collector.Process{
		{PID: 1001, Command: "nvim", CPUPercent: 5.0, MemRSS: 50 * 1024 * 1024, Status: "running", Depth: 0, IsLastChild: false},
		{PID: 1002, Command: "lsp", CPUPercent: 0.0, MemRSS: 10 * 1024 * 1024, Status: "sleeping", Depth: 1, IsLastChild: true},
	}

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	for _, k := range "nv" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = result.(Model)
	}

	// Without children: 1 match (nvim)
	if len(m.searchResults) != 1 {
		t.Fatalf("expected 1 result without children, got %d", len(m.searchResults))
	}

	// Toggle children with ctrl+t
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = result.(Model)
	if !m.searchShowChildren {
		t.Error("expected searchShowChildren=true after ctrl+t")
	}

	// With children: 2 results (nvim + lsp)
	if len(m.searchResults) != 2 {
		t.Fatalf("expected 2 results with children, got %d", len(m.searchResults))
	}
	if !m.searchResults[1].isChild {
		t.Error("expected second result to have isChild=true")
	}
}

func TestSearch_tabSelect(t *testing.T) {
	m := testModel()

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	// Type "go"
	for _, k := range "go" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = result.(Model)
	}

	// Tab to select the result
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)

	if _, ok := m.selectedPIDs[1002]; !ok {
		t.Error("expected PID 1002 to be selected after tab")
	}
	if !m.showingMultiSelect {
		t.Error("expected showingMultiSelect=true after tab select")
	}

	// Tab again to deselect
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)

	if _, ok := m.selectedPIDs[1002]; ok {
		t.Error("expected PID 1002 to be deselected after second tab")
	}
	if m.showingMultiSelect {
		t.Error("expected showingMultiSelect=false after deselecting last")
	}
}

func TestSearch_selectAllAndDeselect(t *testing.T) {
	m := testModel()

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	for _, k := range "n" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = result.(Model)
	}

	// ctrl+a — select all
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	m = result.(Model)

	if len(m.selectedPIDs) != 2 {
		t.Errorf("expected 2 selected PIDs, got %d", len(m.selectedPIDs))
	}

	// ctrl+d — deselect all
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = result.(Model)

	if len(m.selectedPIDs) != 0 {
		t.Errorf("expected 0 selected PIDs after ctrl+d, got %d", len(m.selectedPIDs))
	}
}

func TestSearch_invertSelection(t *testing.T) {
	m := testModel()

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	for _, k := range "n" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = result.(Model)
	}

	// Select PID 1001 with tab.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)

	if _, ok := m.selectedPIDs[1001]; !ok {
		t.Error("expected PID 1001 selected before invert")
	}
	if _, ok := m.selectedPIDs[1003]; ok {
		t.Error("expected PID 1003 not selected before invert")
	}

	// ctrl+u — invert selection (deselect 1001, select 1003)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = result.(Model)

	if _, ok := m.selectedPIDs[1001]; ok {
		t.Error("expected PID 1001 deselected after invert")
	}
	if _, ok := m.selectedPIDs[1003]; !ok {
		t.Error("expected PID 1003 selected after invert")
	}
}

func TestSearch_tIsSearchable(t *testing.T) {
	m := testModel()

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	// Type "t" — should be appended to query, not toggle children
	for _, k := range "t" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = result.(Model)
	}

	if m.searchQuery != "t" {
		t.Errorf("expected query 't', got %q", m.searchQuery)
	}
	if m.searchShowChildren {
		t.Error("expected searchShowChildren=false — t should not toggle children anymore")
	}
}

func TestSearch_ctrlH_fromSearch(t *testing.T) {
	m := testModel()

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	// ctrl+h should exit search and show help
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	m = result.(Model)

	if m.mode != ViewHelp {
		t.Errorf("expected ViewHelp after ctrl+h from search, got %v", m.mode)
	}
	if m.inputMode != InputNone {
		t.Errorf("expected InputNone after ctrl+h, got %v", m.inputMode)
	}
	if len(m.selectedPIDs) != 0 {
		t.Errorf("expected selections cleared after ctrl+h, got %d", len(m.selectedPIDs))
	}
}

func TestSearch_ctrlH_fromNormal(t *testing.T) {
	m := testModel()

	// ctrl+h should show help in normal mode
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	m = result.(Model)

	if m.mode != ViewHelp {
		t.Errorf("expected ViewHelp after ctrl+h in normal mode, got %v", m.mode)
	}

	// esc should return to ViewMain
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	m = result.(Model)

	if m.mode != ViewMain {
		t.Errorf("expected ViewMain after esc from help, got %v", m.mode)
	}
}

func TestEsc_clearsSelectionsInNormalMode(t *testing.T) {
	m := testModel()

	// Select a PID in normal mode
	m.selectedPIDs[1001] = struct{}{}
	m.showingMultiSelect = true

	// Press esc — should clear selections
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	m = result.(Model)

	if len(m.selectedPIDs) != 0 {
		t.Errorf("expected selections cleared, got %d", len(m.selectedPIDs))
	}
	if m.showingMultiSelect {
		t.Error("expected showingMultiSelect=false")
	}
}

func TestEsc_clearsSelectionsInOverview(t *testing.T) {
	m := testModel()

	// Switch to overview
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = result.(Model)

	// Select a PID
	m.selectedPIDs[1001] = struct{}{}

	// Press esc — should clear selections and exit overview
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")})
	m = result.(Model)

	if len(m.selectedPIDs) != 0 {
		t.Errorf("expected selections cleared, got %d", len(m.selectedPIDs))
	}
	if m.mode != ViewMain {
		t.Errorf("expected ViewMain after esc from overview, got %v", m.mode)
	}
}

func TestSearch_matchesFullCmdline(t *testing.T) {
	m := testModel()

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	// "main.go" only appears in FullCmdline, not Command
	for _, k := range "main.go" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = result.(Model)
	}

	if len(m.searchResults) != 1 {
		t.Fatalf("expected 1 result for 'main.go' (matching FullCmdline), got %d", len(m.searchResults))
	}
	if m.searchResults[0].proc.PID != 1001 {
		t.Errorf("expected PID 1001 (nvim), got PID %d", m.searchResults[0].proc.PID)
	}

	// "8080" only appears in FullCmdline of node server.js
	m2 := testModel()
	result2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m2 = result2.(Model)
	for _, k := range "8080" {
		result2, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m2 = result2.(Model)
	}

	if len(m2.searchResults) != 1 {
		t.Fatalf("expected 1 result for '8080', got %d", len(m2.searchResults))
	}
	if m2.searchResults[0].proc.PID != 1003 {
		t.Errorf("expected PID 1003 (node server.js), got PID %d", m2.searchResults[0].proc.PID)
	}
}

func TestSearch_treeViewConnectors(t *testing.T) {
	m := testModel()

	// Set up a parent/child hierarchy with depth=1
	m.windows[0].Processes = []collector.Process{
		{PID: 1001, Command: "nvim", Depth: 0, CPUPercent: 5.0, MemRSS: 50 * 1024 * 1024, Status: "running", IsLastChild: false},
		{PID: 1002, Command: "lsp", Depth: 1, CPUPercent: 0.0, MemRSS: 10 * 1024 * 1024, Status: "sleeping", IsLastChild: true},
	}

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = result.(Model)

	// Search for "lsp" — depth=1 process
	for _, k := range "lsp" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = result.(Model)
	}

	if len(m.searchResults) != 1 {
		t.Fatalf("expected 1 result, got %d", len(m.searchResults))
	}

	view := m.View()
	// Verify tree connector characters appear in search results view
	if !strings.Contains(view, "└──") && !strings.Contains(view, "├──") {
		t.Errorf("expected tree connectors in search result view, got:\n%s", view)
	}
}

func TestMultiSelect_normalMode(t *testing.T) {
	m := testModel()

	// Tab in normal mode on the default process (index 0, PID 1001).
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)

	if _, ok := m.selectedPIDs[1001]; !ok {
		t.Error("expected PID 1001 selected after tab in normal mode")
	}
	if !m.showingMultiSelect {
		t.Error("expected showingMultiSelect=true after tab in normal mode")
	}

	// Tab again deselects.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = result.(Model)
	if _, ok := m.selectedPIDs[1001]; ok {
		t.Error("expected PID 1001 deselected after second tab")
	}
	if m.showingMultiSelect {
		t.Error("expected showingMultiSelect=false after deselect")
	}
}

func TestMultiSelect_selectAllInWindow(t *testing.T) {
	m := testModel()

	// ctrl+a in normal mode selects all processes in current window (3 procs).
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	m = result.(Model)

	if len(m.selectedPIDs) != 3 {
		t.Errorf("expected 3 selected PIDs in window, got %d", len(m.selectedPIDs))
	}
}

func TestMultiSelect_selectChildrenInWindow(t *testing.T) {
	m := testModel()

	// Set up a parent/child hierarchy.
	m.windows[0].Processes = []collector.Process{
		{PID: 1001, Command: "nvim", Depth: 0, CPUPercent: 5.0, MemRSS: 50 * 1024 * 1024, Status: "running"},
		{PID: 1002, Command: "lsp", Depth: 1, CPUPercent: 0.0, MemRSS: 10 * 1024 * 1024, Status: "sleeping"},
		{PID: 1003, Command: "tsserver", Depth: 2, CPUPercent: 0.0, MemRSS: 5 * 1024 * 1024, Status: "sleeping"},
		{PID: 2001, Command: "bash", Depth: 0, CPUPercent: 0.5, MemRSS: 10 * 1024 * 1024, Status: "sleeping"},
	}

	// Navigate to first process (index 0, PID 1001).
	m.selectedProc = 0
	m.browsingProcs = true

	// ctrl+e — select nvim + descendants (PIDs 1001, 1002, 1003)
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	m = result.(Model)

	if len(m.selectedPIDs) != 3 {
		t.Errorf("expected 3 PIDs selected (nvim + children), got %d", len(m.selectedPIDs))
	}
	if _, ok := m.selectedPIDs[1001]; !ok {
		t.Error("expected PID 1001 (nvim) selected")
	}
	if _, ok := m.selectedPIDs[1002]; !ok {
		t.Error("expected PID 1002 (lsp) selected")
	}
	if _, ok := m.selectedPIDs[1003]; !ok {
		t.Error("expected PID 1003 (tsserver) selected")
	}
	if _, ok := m.selectedPIDs[2001]; ok {
		t.Error("expected PID 2001 (bash) NOT selected — different subtree")
	}

	// ctrl+e again — toggle off
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	m = result.(Model)

	if len(m.selectedPIDs) != 0 {
		t.Errorf("expected 0 PIDs after deselect, got %d", len(m.selectedPIDs))
	}
}

func TestMultiSelect_deadPIDCleanup(t *testing.T) {
	m := testModel()

	// Select PID 1001.
	m.selectedPIDs[1001] = struct{}{}
	m.showingMultiSelect = true

	// Simulate a refresh where PID 1001 is gone.
	sessions := []collector.SessionData{
		{
			Name: "test-session",
			Windows: []collector.WindowData{
				{
					Name:  "editor",
					Index: 0,
					Processes: []collector.Process{
						{PID: 2001, Command: "bash", CPUPercent: 0.5, MemRSS: 10 * 1024 * 1024, Status: "sleeping", Depth: 0},
					},
				},
			},
		},
	}
	result, _ := m.Update(sessionDataMsg{sessions})
	m = result.(Model)

	if _, ok := m.selectedPIDs[1001]; ok {
		t.Error("expected PID 1001 to be removed from selection after refresh")
	}
	if m.showingMultiSelect {
		t.Error("expected showingMultiSelect=false after all selections removed")
	}
}

// ── Auto-selection ──────────────────────────────────────────────

func TestAutoSelect_mainView_selectsFirstProcessOnRefresh(t *testing.T) {
	m := testModel()
	if m.browsingProcs {
		t.Fatal("expected browsingProcs=false before the first refresh")
	}

	result, _ := m.Update(sessionDataMsg{m.sessions})
	m = result.(Model)

	if !m.browsingProcs {
		t.Error("expected browsingProcs=true after the first refresh")
	}
	if m.selectedProc != 0 {
		t.Errorf("expected selectedProc=0 after the first refresh, got %d", m.selectedProc)
	}
	if pid := m.windows[m.currentTab].Processes[m.selectedProc].PID; pid != 1001 {
		t.Errorf("expected PID 1001 to be auto-selected, got %d", pid)
	}
}

func TestAutoSelect_mainView_skipsPaneSeparator(t *testing.T) {
	m := testModel()
	m.windows[0].Processes = []collector.Process{
		{PID: 0, Command: "── pane 0 ──", Depth: -1},
		{PID: 10, Command: "zsh", Depth: 0},
	}
	m.sessions[0].Windows = m.windows

	result, _ := m.Update(sessionDataMsg{m.sessions})
	m = result.(Model)

	if !m.browsingProcs {
		t.Fatal("expected browsingProcs=true after the first refresh")
	}
	if m.selectedProc != 1 {
		t.Errorf("expected selectedProc=1 (first real process), got %d", m.selectedProc)
	}
	if pid := m.windows[m.currentTab].Processes[m.selectedProc].PID; pid != 10 {
		t.Errorf("expected PID 10 to be auto-selected, got %d", pid)
	}
}

func TestAutoSelect_preservesUserSelectionAcrossRefresh(t *testing.T) {
	m := testModel()

	// The user navigates to the second process.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = result.(Model)
	if m.selectedProc != 1 {
		t.Fatalf("expected selectedProc=1 after j, got %d", m.selectedProc)
	}

	// A refresh must not move the cursor back to the first entry.
	result, _ = m.Update(sessionDataMsg{m.sessions})
	m = result.(Model)

	if m.selectedProc != 1 {
		t.Errorf("expected selectedProc=1 to survive the refresh, got %d", m.selectedProc)
	}
}

func TestAutoSelect_overview_selectsFirstSession(t *testing.T) {
	m := testModel()

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = result.(Model)

	if m.mode != ViewOverview {
		t.Fatalf("expected ViewOverview, got %v", m.mode)
	}
	if !m.browsingSession {
		t.Error("expected browsingSession=true after entering overview")
	}
	if m.selectedSession != 0 {
		t.Errorf("expected selectedSession=0 after entering overview, got %d", m.selectedSession)
	}
}

func TestAutoSelect_overviewOnLaunch_selectsFirstSession(t *testing.T) {
	m := testModel()
	m.SetMode(ViewOverview)

	result, _ := m.Update(sessionDataMsg{m.sessions})
	m = result.(Model)

	if !m.browsingSession {
		t.Error("expected browsingSession=true in overview mode")
	}
	if m.selectedSession != 0 {
		t.Errorf("expected selectedSession=0 in overview mode, got %d", m.selectedSession)
	}
}

func TestAutoSelect_overview_preservesUserSelectionAcrossRefresh(t *testing.T) {
	m := testModel()
	m.mode = ViewOverview
	// Two sessions so the cursor can actually move.
	m.sessions = append(m.sessions, collector.SessionData{Name: "other-session"})

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = result.(Model)
	if m.selectedSession != 1 {
		t.Fatalf("expected selectedSession=1 after j, got %d", m.selectedSession)
	}

	result, _ = m.Update(sessionDataMsg{m.sessions})
	m = result.(Model)

	if m.selectedSession != 1 {
		t.Errorf("expected selectedSession=1 to survive the refresh, got %d", m.selectedSession)
	}
}

// ── Panic regressions ───────────────────────────────────────────

// TestSearch_ordering pins the search result ordering: exact matches first,
// then prefixes, then substrings; within a tier the busiest process first.
func TestSearch_ordering(t *testing.T) {
	m := testModel()
	m.windows[0].Processes = []collector.Process{
		{PID: 1, Command: "alpha", Status: "sleeping", Depth: 0, CPUPercent: 1},
		{PID: 2, Command: "alphabet", Status: "sleeping", Depth: 0, CPUPercent: 50},
		{PID: 3, Command: "xalpha", Status: "sleeping", Depth: 0, CPUPercent: 5},
		{PID: 4, Command: "alpha-two", Status: "sleeping", Depth: 0, CPUPercent: 90},
	}
	m.sessions[0].Windows = m.windows
	m.inputMode = InputSearch
	m.searchQuery = "alpha"

	m.searchResults = m.rebuildSearchResults()

	want := []int{1, 4, 2, 3}
	if len(m.searchResults) != len(want) {
		t.Fatalf("expected %d results, got %d", len(want), len(m.searchResults))
	}
	for i, pid := range want {
		if m.searchResults[i].proc.PID != pid {
			t.Errorf("result %d: expected PID %d, got %d", i, pid, m.searchResults[i].proc.PID)
		}
	}
}

// A terminal narrower than 40 columns used to make getSidebarWidth return a
// negative width, and renderSidebar then called strings.Repeat with it.
func TestNoPanic_narrowTerminal(_ *testing.T) {
	m := testModel()
	m.width = 30
	m.height = 24
	_ = m.View()
}

func TestSidebarWidth_neverNegative(t *testing.T) {
	m := testModel()
	for w := 1; w <= 120; w++ {
		m.width = w
		if got := m.getSidebarWidth(); got < 0 {
			t.Fatalf("getSidebarWidth() = %d for width %d, want >= 0", got, w)
		}
	}
}

// The monitored session can disappear, or the user can search before the
// first data refresh lands, leaving m.windows empty.
func TestNoPanic_searchWithNoWindows(_ *testing.T) {
	m := testModel()
	m.windows = nil
	m.sessions = nil
	m.searchAllSessions = false
	m.searchQuery = "n"
	m.searchResults = m.rebuildSearchResults()
}

// A terminal 2 rows tall gives renderSidebar a negative height.
// ── Sidebar content sanitization (witr via `script -qec` emits CRLF) ─────────

// TestSidebar_noControlChars ensures CR characters from pty-wrapped witr
// output never reach the rendered frame: an embedded \r inside a line joined
// with the process list sends the terminal cursor back to column 0 and the
// padding spaces wipe the process row's left edge.
func TestSidebar_noControlChars(t *testing.T) {
	m := testModel()
	m.sidebarOpen = true
	m.sidebarContent = "witr 0.4\r\npid: 1234  state: S\r\nparent chain:\r\n  1 -> 100\r\n"

	out := m.renderSidebar(40, 10)
	for i, ln := range strings.Split(out, "\n") {
		if strings.ContainsAny(ln, "\r\b") {
			t.Errorf("sidebar line %d contains control chars: %q", i, ln)
		}
	}
	if !strings.Contains(out, "pid: 1234") {
		t.Error("expected sanitized content to still contain pid line")
	}
}

// TestSidebarCache_noControlChars: multi-select path stores witr -t output in
// sidebarCache; same CRLF corruption applies to the " "-prefixed render path.
func TestSidebarCache_noControlChars(t *testing.T) {
	m := testModel()
	m.sidebarOpen = true
	m.showingMultiSelect = true
	m.selectedPIDs = map[int]struct{}{1001: {}}
	m.sidebarCache = map[int]string{1001: "line one\r\nline two\r\n"}
	m.rebuildPIDIndex()

	out := m.renderSidebar(40, 12)
	for i, ln := range strings.Split(out, "\n") {
		if strings.ContainsAny(ln, "\r\b") {
			t.Errorf("multi-select sidebar line %d contains control chars: %q", i, ln)
		}
	}
}

// TestSidebarMultiSelect_linesFitWidth: wrapped content lines are prefixed
// with two spaces, so they must be wrapped to width-2. Lines wider than the
// sidebar skew lipgloss.JoinHorizontal column alignment for the whole frame.
func TestSidebarMultiSelect_linesFitWidth(t *testing.T) {
	m := testModel()
	m.sidebarOpen = true
	m.showingMultiSelect = true
	m.selectedPIDs = map[int]struct{}{1001: {}}
	m.sidebarCache = map[int]string{
		1001: "a long witr output line that certainly exceeds the sidebar width and must wrap",
	}
	m.rebuildPIDIndex()

	const width = 40
	out := m.renderSidebar(width, 16)
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w > width {
			t.Errorf("sidebar line %d visible width %d exceeds sidebar width %d: %q", i, w, width, ln)
		}
	}
}

func TestNoPanic_renderSidebarNegativeHeight(_ *testing.T) {
	m := testModel()
	_ = m.renderSidebar(40, -1)
	_ = m.renderSidebar(-5, 10)
}
