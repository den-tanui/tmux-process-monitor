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

// viewHelp renders the keyboard shortcut reference overlay as a scrollable centered popup.
func (m Model) viewHelp() string {
	contentLines := []string{
		center(StyleHeader.Render("tmux-process-monitor — Keyboard Reference"), 52),
		"",
		StyleLabel.Render("Navigation"),
		"  " + StyleValue.Render("h / l") + "           Switch windows",
		"  " + StyleValue.Render("j / k") + "           Browse processes",
		"  " + StyleValue.Render("Enter") + "           Open process detail view",
		"  " + StyleValue.Render("Esc") + "             Close detail / clear selections",
		"",
		StyleLabel.Render("Views"),
		"  " + StyleValue.Render("o") + "               Toggle overview (all sessions)",
		"  " + StyleValue.Render(keyCtrlP) + "         Toggle diagnostics sidebar",
		"  " + StyleValue.Render("space") + "           Freeze / unfreeze updates",
		"  " + StyleValue.Render("/") + "               Activate search mode",
		"  " + StyleValue.Render("? / ctrl+h") + "     Show / hide this help",
		"",
		StyleLabel.Render("Search Mode"),
		"  " + StyleValue.Render("j / k") + "           Navigate search results",
		"  " + StyleValue.Render(keyTab) + "             Toggle selection on cursor",
		"  " + StyleValue.Render(keyCtrlT) + "          Toggle child expansion",
		"  " + StyleValue.Render(keyCtrlS) + "          Toggle scope (session/all)",
		"  " + StyleValue.Render("Enter") + "           Open detail for selected result",
		"  " + StyleValue.Render("Esc") + "             Exit search (clears selections)",
		"",
		StyleLabel.Render("Multi-Select"),
		"  " + StyleValue.Render(keyTab) + "             Select / deselect cursor process",
		"  " + StyleValue.Render(keyCtrlA) + "          Select all in scope",
		"  " + StyleValue.Render(keyCtrlD) + "          Deselect all",
		"  " + StyleValue.Render(keyCtrlU) + "          Invert selection",
		"  " + StyleValue.Render(keyCtrlE) + "          Select / deselect process + children",
		"",
		StyleLabel.Render("Process Actions"),
		"  " + StyleValue.Render("x") + "               Send SIGTERM to selected process",
		"  " + StyleValue.Render("s") + "               Send custom signal (enter number)",
		"  " + StyleValue.Render("y") + "               Yank full cmdline to clipboard",
		"  " + StyleValue.Render("Y") + "               Yank PID to clipboard",
		"  " + StyleValue.Render("Alt+h/l") + "         Scroll long command lines",
		"",
		StyleLabel.Render("General"),
		"  " + StyleValue.Render("q / Ctrl+C") + "     Quit",
		"",
		center(StyleFooter.Render("Press any key to close (j/k to scroll)"), 52),
	}

	joined := strings.Join(contentLines, "\n")

	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#87d7ff")).
		Padding(1, 2)

	popup := popupStyle.Render(joined)
	popupLines := strings.Split(popup, "\n")
	popupHeight := len(popupLines)
	popupWidth := lipgloss.Width(popupLines[0])

	availableHeight := m.height

	startY := (availableHeight - popupHeight) / 2
	if startY < 0 {
		startY = 0
	}

	startX := (m.width - popupWidth) / 2
	if startX < 0 {
		startX = 0
	}

	// Clamp scroll offset.
	maxScroll := max0(popupHeight - availableHeight)
	if m.helpScroll > maxScroll {
		m.helpScroll = maxScroll
	}

	var sections []string
	for i := 0; i < startY; i++ {
		sections = append(sections, "")
	}

	// Slice popup lines based on scroll offset.
	visibleLines := popupLines
	if m.helpScroll > 0 {
		if m.helpScroll < len(popupLines) {
			visibleLines = popupLines[m.helpScroll:]
		} else {
			visibleLines = nil
		}
	}

	indent := strings.Repeat(" ", startX)
	for _, pl := range visibleLines {
		if len(sections) >= availableHeight {
			break
		}
		sections = append(sections, indent+pl)
	}

	for len(sections) < availableHeight {
		sections = append(sections, "")
	}

	return strings.Join(sections[:availableHeight], "\n")
}
