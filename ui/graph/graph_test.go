package graph

import (
	"strings"
	"testing"
	"time"
)

func TestStore_pushAndGet(t *testing.T) {
	s := NewStore(3)
	s.Push("main", 10, 20)
	s.Push("main", 30, 40)
	s.Push("main", 50, 60)
	pts := s.Get("main")
	if len(pts) != 3 {
		t.Fatalf("expected 3 points, got %d", len(pts))
	}
	if pts[2].CPU != 50 || pts[2].Mem != 60 {
		t.Errorf("last point wrong: %+v", pts[2])
	}
}

func TestStore_ringEviction(t *testing.T) {
	s := NewStore(2)
	s.Push("s", 1, 1)
	s.Push("s", 2, 2)
	s.Push("s", 3, 3) // should evict first
	pts := s.Get("s")
	if len(pts) != 2 {
		t.Fatalf("expected 2 points after eviction, got %d", len(pts))
	}
	if pts[0].CPU != 2 {
		t.Errorf("expected oldest to be evicted; pts[0].CPU=%v", pts[0].CPU)
	}
}

func TestStore_emptySession(t *testing.T) {
	s := NewStore(10)
	pts := s.Get("nonexistent")
	if pts != nil {
		t.Errorf("expected nil for unknown session, got %v", pts)
	}
}

func TestRender_dimensionsRespected(t *testing.T) {
	pts := []DataPoint{
		{Timestamp: time.Now(), CPU: 50, Mem: 30},
		{Timestamp: time.Now(), CPU: 60, Mem: 40},
	}
	out := Render(pts, 40, 10)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// height lines (plotH) + 1 x-axis + 1 legend = height total
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d:\n%s", len(lines), out)
	}
}

func TestRender_empty(t *testing.T) {
	out := Render(nil, 40, 10)
	// Should return height empty lines without panicking.
	lines := strings.Split(out, "\n")
	if len(lines) < 10 {
		t.Errorf("expected at least 10 lines for empty input, got %d", len(lines))
	}
}
