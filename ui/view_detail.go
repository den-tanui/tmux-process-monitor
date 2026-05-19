package ui

import (
	"fmt"
	"strings"
)

// viewDetail renders the full-screen process detail panel for the selected process.
func (m Model) viewDetail() string {
	p := m.selectedProcess()
	if p == nil {
		m.mode = ViewMain
		return m.viewMain()
	}

	lines := []string{
		StyleHeader.Render(fmt.Sprintf(" Process Detail — PID %d (Session: %s)", p.PID, m.session)),
		m.renderSeparator(),
	}

	if !m.detailReady {
		lines = append(lines, "", center("Loading...", m.width))
	} else {
		// The viewport handles its own height/width.
		lines = append(lines, strings.Split(m.detailView.View(), "\n")...)
	}

	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	lines = append(lines, StyleFooter.Render("j/k/up/down=scroll  Enter/q/Esc=back  x=kill  s=signal"))
	return strings.Join(lines, "\n")
}
