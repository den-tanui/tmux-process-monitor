package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	if !strings.Contains(view, "Help") && !strings.Contains(view, "help") {
		t.Error("expected help content in view when in help mode")
	}
}
