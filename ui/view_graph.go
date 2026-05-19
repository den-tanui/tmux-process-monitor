package ui

import (
	"fmt"
	"strings"

	"github.com/NimbleMarkets/ntcharts/sparkline"
)

// viewWithGraph renders the split-screen graph view:
// top half = condensed process list, bottom half = CPU/MEM graph.
func (m Model) viewWithGraph() string {
	// Divide height: header(1) + sep(1) + tabs(1) + sep(1) = 4 chrome
	// footer: sep(1) + stats(1) + footer(1) = 3 lines
	const chrome = 4
	const footerLines = 3
	available := m.height - chrome - footerLines
	if available < 4 {
		return m.viewMain() // not enough room; fall back
	}

	listH := available * 6 / 10
	graphH := available - listH
	if graphH < 4 {
		graphH = 4
	}
	if listH < 2 {
		listH = 2
	}

	sections := []string{
		m.renderHeader(),
		m.renderSeparator(),
		m.renderTabs(),
		m.renderSeparator(),
	}

	procLines := m.renderProcessList(listH)
	sections = append(sections, procLines...)

	// Graph separator + graph.
	sections = append(sections, m.renderSeparator())

	var winIdx int
	if m.currentTab < len(m.windows) {
		winIdx = m.windows[m.currentTab].Index
	}
	key := fmt.Sprintf("%s:%d", m.session, winIdx)
	pts := m.graphStore.Get(key)

	var cpuData []float64
	var memData []float64
	for _, p := range pts {
		cpuData = append(cpuData, p.CPU)
		memData = append(memData, p.Mem)
	}

	cpuH := (graphH - 2) / 2
	memH := graphH - 2 - cpuH
	
	if cpuH < 1 { cpuH = 1 }
	if memH < 1 { memH = 1 }

	cpuChart := sparkline.New(m.width, cpuH, sparkline.WithData(cpuData), sparkline.WithStyle(StyleHigh))
	memChart := sparkline.New(m.width, memH, sparkline.WithData(memData), sparkline.WithStyle(StyleTree))
	
	cpuChart.DrawBraille()
	memChart.DrawBraille()

	sections = append(sections, StyleHigh.Render(fmt.Sprintf("  CPU (max: %.1f%%)", cpuChart.MaxValue())))
	for _, gl := range strings.Split(strings.TrimRight(cpuChart.View(), "\n"), "\n") {
		sections = append(sections, gl)
	}

	sections = append(sections, StyleTree.Render(fmt.Sprintf("  MEM (max: %.1f%%)", memChart.MaxValue())))
	for _, gl := range strings.Split(strings.TrimRight(memChart.View(), "\n"), "\n") {
		sections = append(sections, gl)
	}

	for len(sections) < m.height-3 {
		sections = append(sections, "")
	}
	
	sections = append(sections, m.renderSeparator())
	sections = append(sections, m.renderStatsLine())
	sections = append(sections, m.renderFooter())
	
	if len(sections) > m.height {
		sections = sections[:m.height]
	}
	return strings.Join(sections, "\n")
}
