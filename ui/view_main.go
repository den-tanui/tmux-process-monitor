package ui

import (
	"fmt"
	"strings"
)

// viewMain renders the main process list view.
func (m Model) viewMain() string {
	if m.inputMode == InputSearch {
		return m.viewSearch()
	}
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

// viewSearch renders the search view (replaces normal view during search).
func (m Model) viewSearch() string {
	// Chrome: header(1) + sep(1) + searchBar(1) + infoLine(1) + sep(1) = 5 before content
	//         sep(1) + stats(1) + footer(1) = 3 after
	// Total chrome = 8
	const chrome = 8
	availProc := m.height - chrome
	if availProc < 1 {
		availProc = 1
	}

	sections := []string{
		m.renderHeader(),
		m.renderSeparator(),
		m.renderSearchBar(),
		m.renderSearchInfoLine(),
		m.renderSeparator(),
	}

	procLines := m.renderSearchResults(availProc)
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

// renderSearchResults returns the search results lines.
func (m Model) renderSearchResults(availLines int) []string {
	if len(m.searchQuery) == 0 {
		return []string{StyleSep.Render(center("(type to search processes)", m.width))}
	}
	if len(m.searchResults) == 0 {
		return []string{StyleSep.Render(center(fmt.Sprintf("(no matches for %q)", m.searchQuery), m.width))}
	}

	// Header.
	hdr := StyleTree.Render(fmt.Sprintf("%8s %6s %15s %-10s  %s", "PID", "CPU%", "MEM", "STATUS", "COMMAND"))
	lines := []string{hdr, m.renderSeparator()}

	// Scroll to keep selected in view.
	firstVisible := 0
	maxProc := availLines - 2 // header + separator
	if m.searchSelected >= maxProc {
		firstVisible = m.searchSelected - maxProc + 1
	}

	prevSession := -2
	prevWindow := -2
	for i, sr := range m.searchResults {
		if i < firstVisible {
			// Still track headers for scroll offset purposes.
			prevSession = sr.sessionIdx
			prevWindow = sr.windowIdx
			continue
		}
		if len(lines) >= availLines {
			break
		}
		// Insert window group header when window changes.
		if sr.windowIdx != prevWindow || sr.sessionIdx != prevSession {
			header := m.searchWindowLabel(sr)
			if header != "" {
				lines = append(lines, StyleSep.Render(header))
			}
			prevSession = sr.sessionIdx
			prevWindow = sr.windowIdx
		}
		lines = append(lines, m.renderSearchResultRow(sr, i))
	}
	return lines
}

// searchWindowLabel returns the window group header label for a search result.
func (m Model) searchWindowLabel(sr searchResult) string {
	if !m.searchAllSessions {
		if sr.windowIdx < len(m.windows) {
			return fmt.Sprintf("%s / %d.%s", m.session, m.windows[sr.windowIdx].Index, m.windows[sr.windowIdx].Name)
		}
	} else {
		if sr.sessionIdx >= 0 && sr.sessionIdx < len(m.sessions) {
			sd := m.sessions[sr.sessionIdx]
			if sr.windowIdx < len(sd.Windows) {
				return fmt.Sprintf("%s / %d.%s", sd.Name, sd.Windows[sr.windowIdx].Index, sd.Windows[sr.windowIdx].Name)
			}
		}
	}
	return ""
}

// renderSearchResultRow renders one search result row.
func (m Model) renderSearchResultRow(sr searchResult, idx int) string {
	// Insert window group header when window changes.
	// (Handled in the caller via prevWindow tracking — for now render inline.)

	proc := sr.proc

	prefix := treePrefix(proc, sr.procIdx, m.getProcessList(sr))

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

	// Command with search matching highlighted.
	uncoloredBase := baseLeft + statusStr + " "
	maxCmdLen := m.width - len(uncoloredBase) - len(prefix) - len(selMarker) - 2
	if maxCmdLen < 4 {
		maxCmdLen = 4
	}
	cmd := highlightMatch(proc.Command, m.searchQuery, maxCmdLen)

	isSelected := idx == m.searchSelected
	row := fmt.Sprintf("%s%s%s %s%s",
		selMarker,
		baseLeft,
		styledStatus,
		StyleTree.Render(prefix),
		cmd,
	)
	if isSelected {
		return StyleSelected.Render(row)
	}
	return row
}

// highlightMatch returns the command string with matched characters styled.
func highlightMatch(cmd, query string, maxLen int) string {
	if query == "" {
		return truncate(cmd, maxLen)
	}
	lowerCmd := strings.ToLower(cmd)
	lowerQ := strings.ToLower(query)

	var result strings.Builder
	written := 0
	cmdRunes := []rune(cmd)
	i := 0
	for i < len(cmdRunes) && written < maxLen {
		remaining := maxLen - written
		// Check if this rune matches the first character of the query.
		matched := false
		if strings.HasPrefix(lowerCmd[i:], lowerQ) {
			// Highlight the full matched portion.
			matchLen := len([]rune(query))
			if matchLen > remaining {
				matchLen = remaining
			}
			matchStr := string(cmdRunes[i : i+matchLen])
			result.WriteString(StyleSearchMatch.Render(matchStr))
			i += matchLen
			written += matchLen
			matched = true
		}
		if !matched {
			result.WriteRune(cmdRunes[i])
			i++
			written++
		}
	}
	if i < len(cmdRunes) {
		// Truncation indicator.
		s := result.String()
		if len(s) > 3 {
			s = s[:len(s)-3] + "..."
		}
		return s
	}
	return result.String()
}
