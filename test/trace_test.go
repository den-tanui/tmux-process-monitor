package test_test

import (
	"os"
	"runtime/trace"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/den-tanui/tmux-process-monitor/internal/collector"
	itx "github.com/den-tanui/tmux-process-monitor/internal/tmux"
	"github.com/den-tanui/tmux-process-monitor/ui"
)

func TestTickCycleTrace(t *testing.T) {
	if testing.Short() {
		t.Skip("trace test: manual only, skip with -short")
	}

	tmuxClient := itx.New()
	if !tmuxClient.IsRunning() {
		t.Skip("tmux is not running — trace test requires tmux")
	}

	session, err := tmuxClient.CurrentSession()
	if err != nil {
		t.Skip("could not determine session:", err)
	}

	traceFile := "trace.tee"
	f, err := os.Create(traceFile)
	if err != nil {
		t.Fatalf("could not create trace file: %v", err)
	}
	defer func() { _ = f.Close() }()

	if err := trace.Start(f); err != nil {
		t.Fatalf("could not start trace: %v", err)
	}
	defer trace.Stop()

	coll := collector.New(tmuxClient, session)
	coll.WarmUp()
	m := ui.New(coll, session, "", 2*time.Second)

	tm := teatest.NewTestModel(t, m)
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 40})
	time.Sleep(100 * time.Millisecond)

	// Let several tick cycles run
	time.Sleep(3 * time.Second)

	// Navigate while tracing
	for i := 0; i < 5; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		time.Sleep(200 * time.Millisecond)
	}

	// Toggle help
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	time.Sleep(500 * time.Millisecond)

	// Toggle overview
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	time.Sleep(500 * time.Millisecond)

	_ = tm.Quit()
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))

	t.Logf("Trace written to %s", traceFile)
	t.Logf("Analyze with: go tool trace %s", traceFile)
}
