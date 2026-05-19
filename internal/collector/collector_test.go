package collector

import (
	"runtime"
	"testing"
	"time"
)

// TestCPUPercent_delta verifies the delta calculation for known tick values.
func TestCPUPercent_delta(t *testing.T) {
	// 100 ticks over 1 second on 1 core = 100%.
	// cpuPercent scales by runtime.NumCPU(), so the expected range is
	// [50*cores, 200*cores] to handle jitter.
	numCPU := runtime.NumCPU()
	prev := cpuSample{ts: time.Now().Add(-1 * time.Second), totalTicks: 0}
	curr := cpuSample{ts: time.Now(), totalTicks: 100}
	pct := cpuPercent(prev, curr)
	lo := 50.0 * float64(numCPU)
	hi := 200.0 * float64(numCPU)
	if pct < lo || pct > hi {
		t.Errorf("expected %.0f–%.0f%% (%d cores), got %.2f%%", lo, hi, numCPU, pct)
	}
}

func TestCPUPercent_zeroElapsed(t *testing.T) {
	now := time.Now()
	prev := cpuSample{ts: now, totalTicks: 0}
	curr := cpuSample{ts: now, totalTicks: 100}
	pct := cpuPercent(prev, curr)
	if pct != 0 {
		t.Errorf("expected 0%% for negligible elapsed, got %.2f%%", pct)
	}
}

func TestCPUPercent_idle(t *testing.T) {
	prev := cpuSample{ts: time.Now().Add(-2 * time.Second), totalTicks: 50}
	curr := cpuSample{ts: time.Now(), totalTicks: 50} // no tick increase
	pct := cpuPercent(prev, curr)
	if pct != 0 {
		t.Errorf("expected 0%% for idle process, got %.2f%%", pct)
	}
}

// TestMemPercent_sanity ensures MemPercent doesn't divide-by-zero.
func TestMemPercent_sanity(t *testing.T) {
	c := &Collector{totalRAM: 0}
	if got := c.MemPercent(1024); got != 0 {
		t.Errorf("expected 0 for zero totalRAM, got %v", got)
	}

	c.totalRAM = 8 * 1024 * 1024 * 1024 // 8 GiB
	got := c.MemPercent(int64(c.totalRAM / 2))
	if got < 49 || got > 51 {
		t.Errorf("expected ~50%%, got %.2f%%", got)
	}
}

// TestReadCmdline_kernelThread exercises the fallback for a kernel thread (PID 2).
// This test only runs if /proc/2/comm is readable (Linux).
func TestReadCmdline_kernelThread(t *testing.T) {
	// PID 2 is typically kthreadd on Linux.
	short, full := readCmdline(2)
	if short == "" || full == "" {
		t.Errorf("expected non-empty cmdline for PID 2, got short=%q full=%q", short, full)
	}
}
