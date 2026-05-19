package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/den-tanui/tmux-process-monitor/internal/collector"
)

// buildPluginsTree gathers all plugin processes from system /proc, groups them
// by plugin name, and formats them into a tree structure compatible with treePrefix.
func (m Model) buildPluginsTree() []collector.Process {
	if m.frozen && len(m.cachedPluginsTree) > 0 {
		return m.cachedPluginsTree
	}

	rawProcs := m.coll.CollectSystemPlugins()

	groups := make(map[string][]collector.Process)
	for _, p := range rawProcs {
		pName := p.PluginName
		if pName == "" {
			pName = "tmux-plugin"
		}
		groups[pName] = append(groups[pName], p)
	}

	var pNames []string
	for k := range groups {
		pNames = append(pNames, k)
	}
	sort.Strings(pNames)

	var tree []collector.Process
	for i, pName := range pNames {
		procs := groups[pName]

		// To preserve tree hierarchy, we shift Depth of all nodes by +1
		// and find the last process at Depth 1 (originally 0) to mark IsLastChild.
		lastRootIdx := -1
		for idx, p := range procs {
			if p.Depth == 0 {
				lastRootIdx = idx
			}
		}

		shiftedProcs := make([]collector.Process, len(procs))
		for idx, p := range procs {
			p.Depth = p.Depth + 1
			if p.Depth == 1 {
				p.IsLastChild = (idx == lastRootIdx)
			}
			shiftedProcs[idx] = p
		}

		// Calculate aggregated metrics for the virtual parent node
		var cpuTotal float64
		var memTotal int64
		status := "sleeping"

		for _, p := range procs {
			cpuTotal += p.CPUPercent
			memTotal += p.MemRSS
			if p.Status == "running" {
				status = "running"
			}
		}

		isLastPlugin := (i == len(pNames)-1)

		// Create the virtual parent folder node
		virtualParent := collector.Process{
			PID:         0, // virtual PID
			Command:     "📁 " + pName,
			FullCmdline: fmt.Sprintf("Tmux Plugin: %s", pName),
			Depth:       0,
			IsLastChild: isLastPlugin,
			HasChildren: len(procs) > 0,
			IsPlugin:    true,
			PluginName:  pName,
			Status:      status,
			CPUPercent:  cpuTotal,
			MemRSS:      memTotal,
			MemPercent:  m.coll.MemPercent(memTotal),
		}

		tree = append(tree, virtualParent)
		tree = append(tree, shiftedProcs...)
	}

	return tree
}

func (m Model) viewPlugins() string {
	sections := []string{
		center(StyleHeader.Render("Tmux Plugins Telemetry Dashboard"), m.width),
		m.renderSeparator(),
	}

	pluginsTree := m.buildPluginsTree()
	if len(pluginsTree) == 0 {
		sections = append(sections, "", center(StyleValue.Render("No active tmux plugin processes detected."), m.width))
		for len(sections) < m.height-3 {
			sections = append(sections, "")
		}
		sections = append(sections, m.renderSeparator(), m.renderStatsLine(), m.renderFooter())
		return strings.Join(sections, "\n")
	}

	// Table header
	hdr := StyleTree.Render(
		fmt.Sprintf("%8s %6s %15s %-10s  %s", "PID", "CPU%", "MEM", "STATUS", "COMMAND"),
	)
	sections = append(sections, hdr, m.renderSeparator())

	const chrome = 9 // Header(1) + Sep(1) + TableHdr(1) + Sep(1) + Separator(1) + Stats(1) + Footer(1) + padding = 9
	availRows := m.height - chrome
	if availRows < 1 {
		availRows = 1
	}
	firstVisible := 0
	if m.selectedPlugin >= availRows {
		firstVisible = m.selectedPlugin - availRows + 1
	}

	var rows []string
	for i, proc := range pluginsTree {
		if i < firstVisible {
			continue
		}
		if len(rows) >= availRows {
			break
		}
		rows = append(rows, m.renderProcessRow(proc, i, pluginsTree))
	}
	sections = append(sections, rows...)

	for len(sections) < m.height-3 {
		sections = append(sections, "")
	}

	sections = append(sections, m.renderSeparator(), m.renderStatsLine(), m.renderFooter())
	return strings.Join(sections, "\n")
}
