package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/den-tanui/tmux-process-monitor/internal/collector"
)

// benchModel builds a Model with a large synthetic process tree: windows x
// procsPerWindow processes, spread over a few sessions.
func benchModel(windows, procsPerWindow int) Model {
	m := New(nil, "bench-session", "", -1, -1, 2*time.Second)
	m.width = 200
	m.height = 60

	pid := 1000
	for w := 0; w < windows; w++ {
		procs := make([]collector.Process, 0, procsPerWindow)
		for i := 0; i < procsPerWindow; i++ {
			pid++
			procs = append(procs, collector.Process{
				PID:         pid,
				Command:     fmt.Sprintf("proc-%d --flag value", pid),
				FullCmdline: fmt.Sprintf("/usr/bin/proc-%d --flag value --other thing", pid),
				CPUPercent:  float64(pid % 100),
				MemRSS:      int64(pid%50+1) * 1024 * 1024,
				MemPercent:  float64(pid % 10),
				Status:      "sleeping",
				Depth:       0,
			})
		}
		m.windows = append(m.windows, collector.WindowData{
			Name:      fmt.Sprintf("win-%d", w),
			Index:     w,
			Processes: procs,
		})
	}

	m.sessions = []collector.SessionData{{
		Name:         "bench-session",
		Windows:      m.windows,
		WindowCount:  len(m.windows),
		ProcessCount: windows * procsPerWindow,
	}}
	return m
}

// BenchmarkRebuildSearchResults measures the cost of rebuilding the search
// result list, which runs on every keystroke.
func BenchmarkRebuildSearchResults(b *testing.B) {
	m := benchModel(5, 200)
	m.searchQuery = "proc"
	m.searchAllSessions = true

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.searchResults = m.rebuildSearchResults()
	}
}

// BenchmarkRenderSidebarMultiSelect measures the cost of rendering the
// multi-select sidebar, which looks up every selected PID by scanning the
// whole process tree.
func BenchmarkRenderSidebarMultiSelect(b *testing.B) {
	m := benchModel(5, 200)
	for i := 0; i < 50; i++ {
		m.selectedPIDs[1001+i] = struct{}{}
	}
	m.showingMultiSelect = true
	m.sidebarOpen = true

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = m.renderSidebar(45, 40)
	}
}

// BenchmarkPidToName measures a single PID lookup, which the sidebar does
// twice per selected process per render.
func BenchmarkPidToName(b *testing.B) {
	m := benchModel(5, 200)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = m.pidToName(1500)
	}
}

// BenchmarkWrapANSI measures the ANSI-aware wrapping used by the sidebar.
func BenchmarkWrapANSI(b *testing.B) {
	line := strings.Repeat("segment ", 40)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = wrapANSI(line, 40)
	}
}
