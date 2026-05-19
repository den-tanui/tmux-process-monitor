// Package graph provides an in-memory ring-buffer data store for CPU/memory
// time-series and an ASCII line-graph renderer using lipgloss.
package graph

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ──────────────────────────────────────────────────────────────────────────────
// Data store

// DataPoint holds a single sampled measurement.
type DataPoint struct {
	Timestamp time.Time
	CPU       float64 // 0–100+
	Mem       float64 // 0–100
}

// Store is a thread-safe, per-session ring buffer of DataPoints.
type Store struct {
	mu      sync.RWMutex
	data    map[string][]DataPoint
	maxSize int
}

// NewStore creates a Store with the given ring-buffer capacity per session.
func NewStore(maxSize int) *Store {
	if maxSize <= 0 {
		maxSize = 60
	}
	return &Store{
		data:    make(map[string][]DataPoint),
		maxSize: maxSize,
	}
}

// Push appends a data point for the named session, evicting the oldest if full.
func (s *Store) Push(session string, cpu, mem float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pts := s.data[session]
	pts = append(pts, DataPoint{Timestamp: time.Now(), CPU: cpu, Mem: mem})
	if len(pts) > s.maxSize {
		pts = pts[len(pts)-s.maxSize:]
	}
	s.data[session] = pts
}

// Get returns a copy of the ring buffer for the named session.
func (s *Store) Get(session string) []DataPoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.data[session]
	if len(src) == 0 {
		return nil
	}
	dst := make([]DataPoint, len(src))
	copy(dst, src)
	return dst
}

// ──────────────────────────────────────────────────────────────────────────────
// Renderer

var (
	styleCPU  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff87")) // bright green
	styleMem  = lipgloss.NewStyle().Foreground(lipgloss.Color("#87d7ff")) // sky blue
	styleAxis = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))
	styleLbl  = lipgloss.NewStyle().Foreground(lipgloss.Color("#aaaaaa"))
)

// barRunes maps a normalised row (0 = bottom, n = top) to a block character.
// We use eighth-block characters for sub-row resolution.
var barRunes = []rune(" ▁▂▃▄▅▆▇█")

// Render returns a lipgloss-styled ASCII graph string of the given data points
// that fits within width × height terminal cells.
//
// Layout:
//
//	100%│·····················   ← top label + axis
//	    │   cpu line
//	    │   mem line
//	  0%│_____________________
//	    └────────────────────── time →
//	    CPU XX.X%   MEM XX.X%   ← legend (1 line)
//
// The function is allocation-friendly; call freely on every render tick.
func Render(points []DataPoint, width, height int) string {
	const axisWidth = 5 // "100% " label + "│"

	plotW := width - axisWidth
	plotH := height - 2 // reserve 1 line for x-axis, 1 for legend

	if plotW <= 0 || plotH <= 0 || len(points) == 0 {
		return strings.Repeat("\n", height)
	}

	// Trim or pad points to plotW columns (newest = rightmost).
	pts := points
	if len(pts) > plotW {
		pts = pts[len(pts)-plotW:]
	}

	// Build a 2D grid: grid[row][col] = rune (space or block).
	// row 0 = top (100%), row plotH-1 = bottom (0%).
	type cellKind byte
	const (
		cellEmpty cellKind = iota
		cellCPU
		cellMem
		cellBoth
	)
	grid := make([][]cellKind, plotH)
	for r := range grid {
		grid[r] = make([]cellKind, plotW)
	}

	normalize := func(v float64) int {
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		// row 0 = top = 100%
		row := plotH - 1 - int(v/100*float64(plotH-1)+0.5)
		if row < 0 {
			row = 0
		}
		if row >= plotH {
			row = plotH - 1
		}
		return row
	}

	for i, dp := range pts {
		col := i + (plotW - len(pts)) // right-align
		if col < 0 || col >= plotW {
			continue
		}
		cpuRow := normalize(dp.CPU)
		memRow := normalize(dp.Mem)

		if cpuRow == memRow {
			grid[cpuRow][col] = cellBoth
		} else {
			grid[cpuRow][col] = cellCPU
			grid[memRow][col] = cellMem
		}
	}

	// Render each row to a string.
	var sb strings.Builder
	for r := 0; r < plotH; r++ {
		// Y-axis label (every quarter).
		pct := 100 - int(float64(r)/float64(plotH-1)*100)
		label := ""
		if r == 0 {
			label = "100%"
		} else if r == plotH/2 {
			label = " 50%"
		} else if r == plotH-1 {
			label = "  0%"
		}
		axisLabel := fmt.Sprintf("%4s", label)
		sb.WriteString(styleAxis.Render(axisLabel + "│"))
		_ = pct

		// Plot cells.
		for c := 0; c < plotW; c++ {
			switch grid[r][c] {
			case cellCPU:
				sb.WriteString(styleCPU.Render("▪"))
			case cellMem:
				sb.WriteString(styleMem.Render("▪"))
			case cellBoth:
				sb.WriteString(styleCPU.Render("●"))
			default:
				sb.WriteRune(' ')
			}
		}
		sb.WriteRune('\n')
	}

	// X-axis.
	sb.WriteString(styleAxis.Render("    └" + strings.Repeat("─", plotW)))
	sb.WriteRune('\n')

	// Legend with latest values.
	if len(points) > 0 {
		last := points[len(points)-1]
		legend := fmt.Sprintf("  %s %s",
			styleCPU.Render(fmt.Sprintf("CPU %.1f%%", last.CPU)),
			styleMem.Render(fmt.Sprintf("MEM %.1f%%", last.Mem)),
		)
		sb.WriteString(styleLbl.Render("    ") + legend)
	}

	return sb.String()
}
