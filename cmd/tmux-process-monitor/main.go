// tmux-process-monitor — CLI entry point
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/den-tanui/tmux-process-monitor/internal/collector"
	"github.com/den-tanui/tmux-process-monitor/internal/config"
	itx "github.com/den-tanui/tmux-process-monitor/internal/tmux"
	"github.com/den-tanui/tmux-process-monitor/ui"
)

func main() {
	var (
		window      = flag.String("w", "", "Start on this window name")
		refreshRate = flag.Float64("r", 0, "Refresh interval in seconds (default: tmux option or 2.0)")
		overview    = flag.Bool("overview", false, "Open in overview (all-sessions) mode")
	)
	flag.Parse()

	// Load config — CLI flags override tmux options.
	cfg := config.Load()
	if *refreshRate > 0 {
		cfg.RefreshRate = *refreshRate
	}
	rate := time.Duration(float64(time.Second) * cfg.RefreshRate)

	// Resolve session name: positional arg > current tmux session.
	tmuxClient := itx.New()

	if !tmuxClient.IsRunning() {
		fmt.Fprintln(os.Stderr, "tmux is not running")
		os.Exit(1)
	}

	session := flag.Arg(0)
	if session == "" {
		var err error
		session, err = tmuxClient.CurrentSession()
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not determine current tmux session: %v\n", err)
			os.Exit(1)
		}
	}

	// Validate session.
	sessions, err := tmuxClient.ListSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing sessions: %v\n", err)
		os.Exit(1)
	}
	found := false
	for _, s := range sessions {
		if s == session {
			found = true
			break
		}
	}
	if !found {
		fmt.Fprintf(os.Stderr, "session %q not found. Available: %v\n", session, sessions)
		os.Exit(1)
	}

	// Build collector.
	coll := collector.New(tmuxClient, session)

	// Construct the model.
	m := ui.New(coll, session, *window, rate)

	// Start overview mode if requested.
	if *overview {
		m.SetMode(ui.ViewOverview)
	}

	// Warm up CPU baselines in the background.
	coll.WarmUp()

	// Run bubbletea.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}
