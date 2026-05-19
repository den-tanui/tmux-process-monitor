package ui

import (
	"strings"
)

// viewMain renders the main process list view.
func (m Model) viewMain() string {
	// Reserve lines: header(1) + sep(1) + tabs(1) + sep(1) + sep(1) + stats(1) + footer(1) = 7
	const chrome = 7
	availProc := m.height - chrome
	if availProc < 1 {
		availProc = 1
	}

	sections := []string{
		m.renderHeader(),
		m.renderSeparator(),
		m.renderTabs(),
		m.renderSeparator(),
	}

	procLines := m.renderProcessList(availProc)
	sections = append(sections, procLines...)

	// Pad to fill height before separator, stats line and footer.
	for len(sections) < m.height-3 {
		sections = append(sections, "")
	}
	
	sections = append(sections, m.renderSeparator())
	sections = append(sections, m.renderStatsLine())
	sections = append(sections, m.renderFooter())

	return strings.Join(sections, "\n")
}
