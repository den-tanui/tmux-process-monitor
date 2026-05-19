package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// viewOverview renders the all-sessions summary table.
func (m Model) viewOverview() string {
	lines := []string{
		center(StyleHeader.Render(fmt.Sprintf("System Overview — All Sessions (Current: %s)", m.session)), m.width),
		m.renderSeparator(),
	}

	// System stats header (CPU + MEM totals across sessions).
	var totalCPU float64
	var totalMem int64
	var totalProcs int
	for _, s := range m.sessions {
		totalCPU += s.CPUTotal
		totalMem += s.MemTotal
		totalProcs += s.ProcessCount
	}
	totalMemMB := totalMem / 1024 / 1024
	lines = append(lines,
		fmt.Sprintf("%s %s   %s %s   %s %s",
			StyleLabel.Render("Total CPU:"), StyleValue.Render(fmt.Sprintf("%.1f%%", totalCPU)),
			StyleLabel.Render("MEM:"), StyleValue.Render(fmt.Sprintf("%dMB", totalMemMB)),
			StyleLabel.Render("Procs:"), StyleValue.Render(fmt.Sprintf("%d", totalProcs)),
		),
		m.renderSeparator(),
	)

	// Table header.
	hdr := StyleTree.Render(
		fmt.Sprintf("%-20s %8s %10s %7s %6s", "Session", "CPU%", "MEM", "Procs", "Wins"),
	)
	lines = append(lines, hdr, m.renderSeparator())

	// Table rows.
	const chrome = 7 // lines above
	availRows := m.height - chrome - 1
	firstVisible := 0
	if m.browsingSession && m.selectedSession >= availRows {
		firstVisible = m.selectedSession - availRows + 1
	}

	for i, s := range m.sessions {
		if i < firstVisible {
			continue
		}
		if len(lines) >= m.height-1 {
			break
		}
		memMB := s.MemTotal / 1024 / 1024
		isSelected := m.browsingSession && i == m.selectedSession

		prefix := "  "
		if isSelected {
			prefix = StyleValue.Render("» ")
		}

		row := fmt.Sprintf("%s%-20s %7.1f%% %8dMB %7d %6d",
			prefix,
			truncate(s.Name, 20),
			s.CPUTotal,
			memMB,
			s.ProcessCount,
			s.WindowCount,
		)

		switch {
		case isSelected:
			lines = append(lines, StyleSelected.Render(row))
		case s.CPUTotal >= 20.0:
			lines = append(lines, StyleHigh.Render(row))
		case s.CPUTotal >= 5.0:
			lines = append(lines, StyleLabel.Render(row))
		default:
			lines = append(lines, StyleValue.Render(row))
		}
	}

	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	lines = append(lines, StyleFooter.Render("j/k=browse  Enter=open session  o/Esc=back  q=quit"))
	return strings.Join(lines, "\n")
}

// viewHelp renders the keyboard shortcut reference overlay as a beautiful centered popup.
func (m Model) viewHelp() string {
	contentLines := []string{
		center(StyleHeader.Render("tmux-process-monitor — Keyboard Reference"), 52),
		"",
		StyleLabel.Render("Navigation"),
		"  " + StyleValue.Render("h / l  ←/→") + "     Switch windows",
		"  " + StyleValue.Render("j / k  ↑/↓") + "     Browse processes",
		"  " + StyleValue.Render("Enter") + "          Toggle tree / open detail",
		"  " + StyleValue.Render("Esc") + "            Close detail / overlay",
		"",
		StyleLabel.Render("Views"),
		"  " + StyleValue.Render("g") + "              Toggle graph view",
		"  " + StyleValue.Render("o") + "              Toggle overview (all sessions)",
		"  " + StyleValue.Render("tab") + "            Toggle diagnostics sidebar",
		"  " + StyleValue.Render("space") + "          Freeze / unfreeze updates",
		"  " + StyleValue.Render("?") + "              Show / hide this help",
		"",
		StyleLabel.Render("Process Actions"),
		"  " + StyleValue.Render("x") + "              Send SIGTERM to selected process",
		"  " + StyleValue.Render("s") + "              Send custom signal (enter number)",
		"  " + StyleValue.Render("y") + "              Yank full cmdline to clipboard",
		"  " + StyleValue.Render("Y") + "              Yank PID to clipboard",
		"  " + StyleValue.Render("Alt+h/l") + "        Scroll long command lines",
		"",
		StyleLabel.Render("General"),
		"  " + StyleValue.Render("q / Q / Ctrl+C") + "  Quit",
		"",
		center(StyleFooter.Render("Press any key to return…"), 52),
	}

	joined := strings.Join(contentLines, "\n")

	// Create the popup box using lipgloss.
	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#87d7ff")).
		Padding(1, 2)

	popup := popupStyle.Render(joined)
	popupLines := strings.Split(popup, "\n")
	popupHeight := len(popupLines)
	popupWidth := lipgloss.Width(popupLines[0])

	// Center the popup inside the available height & width.
	startY := (m.height - popupHeight) / 2
	if startY < 0 {
		startY = 0
	}

	startX := (m.width - popupWidth) / 2
	if startX < 0 {
		startX = 0
	}

	var sections []string
	for i := 0; i < startY; i++ {
		sections = append(sections, "")
	}

	indent := strings.Repeat(" ", startX)
	for _, pl := range popupLines {
		sections = append(sections, indent+pl)
	}

	for len(sections) < m.height {
		sections = append(sections, "")
	}

	return strings.Join(sections[:m.height], "\n")
}
