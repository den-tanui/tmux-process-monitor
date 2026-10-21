package collector

import (
	"sync"
	"testing"
	"time"
)

// TestCPUPercent_delta verifies the delta calculation for known tick values.
func TestCPUPercent_delta(t *testing.T) {
	// 100 ticks over 1 second = 100% of one core.
	prev := cpuSample{ts: time.Now().Add(-1 * time.Second), totalTicks: 0}
	curr := cpuSample{ts: time.Now(), totalTicks: 100}
	pct := cpuPercent(prev, curr)
	if pct < 50 || pct > 200 {
		t.Errorf("expected 50–200%%, got %.2f%%", pct)
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

// TestCPUPercent_idle verifies that a process with no tick increase reports 0%.
func TestCPUPercent_idle(t *testing.T) {
	prev := cpuSample{ts: time.Now().Add(-2 * time.Second), totalTicks: 50}
	curr := cpuSample{ts: time.Now(), totalTicks: 50} // no tick increase
	pct := cpuPercent(prev, curr)
	if pct != 0 {
		t.Errorf("expected 0%% for idle process, got %.2f%%", pct)
	}
}

// TestCPUPercent_tickRegression verifies that a tick counter going backwards
// (the process exited, or the PID was recycled) does not underflow the
// unsigned subtraction and report an absurd CPU usage.
func TestCPUPercent_tickRegression(t *testing.T) {
	prev := cpuSample{ts: time.Now().Add(-2 * time.Second), totalTicks: 5000}
	curr := cpuSample{ts: time.Now(), totalTicks: 0} // process restarted
	pct := cpuPercent(prev, curr)
	if pct != 0 {
		t.Errorf("expected 0%% when the tick counter regresses, got %.2f%%", pct)
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

// TestSessionName_raceSafe verifies that reading the session name concurrently
// with SetSession is free of data races. Run with -race to be meaningful.
func TestSessionName_raceSafe(_ *testing.T) {
	c := &Collector{session: "s1"}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			c.SetSession("s2")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = c.sessionName()
		}
	}()
	wg.Wait()
}
