package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type PluginData struct {
	Name         string
	PIDs         []int
	CPUTotal     float64
	MemTotal     int64
	ProcessCount int
	PrimaryCmd   string
	Status       string
}

func (m Model) getPluginsData() []PluginData {
	pluginMap := make(map[string]*PluginData)
	seenPIDs := make(map[int]bool)

	for _, s := range m.sessions {
		for _, w := range s.Windows {
			for _, p := range w.Processes {
				if p.IsPlugin && !seenPIDs[p.PID] {
					seenPIDs[p.PID] = true
					pName := p.PluginName
					if pName == "" {
						pName = "tmux-plugin"
					}

					pd, exists := pluginMap[pName]
					if !exists {
						pd = &PluginData{
							Name:       pName,
							PrimaryCmd: p.Command,
							Status:     p.Status,
						}
						pluginMap[pName] = pd
					}

					pd.PIDs = append(pd.PIDs, p.PID)
					pd.CPUTotal += p.CPUPercent
					pd.MemTotal += p.MemRSS
					pd.ProcessCount++

					// Status precedence: running > disk sleep > sleeping > zombie > stopped
					if p.Status == "running" {
						pd.Status = "running"
					} else if pd.Status != "running" && p.Status == "disk sleep" {
						pd.Status = "disk sleep"
					} else if pd.Status != "running" && pd.Status != "disk sleep" && p.Status == "sleeping" {
						pd.Status = "sleeping"
					}
				}
			}
		}
	}

	var list []PluginData
	for _, pd := range pluginMap {
		list = append(list, *pd)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})

	return list
}

func (m Model) viewPlugins() string {
	sections := []string{
		center(StyleHeader.Render("Tmux Plugins Telemetry Dashboard"), m.width),
		m.renderSeparator(),
	}

	plugins := m.getPluginsData()
	if len(plugins) == 0 {
		sections = append(sections, "", center(StyleValue.Render("No active tmux plugin processes detected."), m.width))
		for len(sections) < m.height-3 {
			sections = append(sections, "")
		}
		sections = append(sections, m.renderSeparator(), m.renderStatsLine(), m.renderFooter())
		return strings.Join(sections, "\n")
	}

	// Table header
	hdr := StyleTree.Render(
		fmt.Sprintf("  %-26s %6s %8s %10s  %-20s %-10s", "PLUGIN NAME", "PROCS", "CPU%", "MEMORY", "PRIMARY CMD", "STATUS"),
	)
	sections = append(sections, hdr, m.renderSeparator())

	const chrome = 7
	availRows := m.height - chrome
	if availRows < 1 {
		availRows = 1
	}
	firstVisible := 0
	if m.selectedPlugin >= availRows {
		firstVisible = m.selectedPlugin - availRows + 1
	}

	var rows []string
	for i, p := range plugins {
		if i < firstVisible {
			continue
		}
		if len(rows) >= availRows {
			break
		}

		memMB := p.MemTotal / 1024 / 1024
		memStr := fmt.Sprintf("%dMB", memMB)
		
		var statusStyle lipgloss.Style
		switch p.Status {
		case "running":
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff87")).Bold(true)
		case "sleeping", "idle":
			statusStyle = StyleValue
		case "stopped", "tracing stop":
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf00"))
		case "zombie", "dead":
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f5f")).Bold(true)
		case "disk sleep":
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#af87ff"))
		default:
			statusStyle = StyleValue
		}

		statusStr := fmt.Sprintf("%-10s", p.Status)
		if len(statusStr) > 10 {
			statusStr = statusStr[:10]
		}
		styledStatus := statusStyle.Render(statusStr)

		isSelected := i == m.selectedPlugin
		prefix := "  "
		if isSelected {
			prefix = StyleValue.Render("» ")
		}

		row := fmt.Sprintf("%s%-26s %6d %6.1f%% %8s  %-20s %s",
			prefix,
			truncate(p.Name, 26),
			p.ProcessCount,
			p.CPUTotal,
			memStr,
			truncate(p.PrimaryCmd, 20),
			styledStatus,
		)

		if isSelected {
			rows = append(rows, StyleSelected.Render(row))
		} else {
			rows = append(rows, StyleValue.Render(row))
		}
	}
	sections = append(sections, rows...)

	for len(sections) < m.height-3 {
		sections = append(sections, "")
	}

	sections = append(sections, m.renderSeparator(), m.renderStatsLine(), m.renderFooter())
	return strings.Join(sections, "\n")
}
