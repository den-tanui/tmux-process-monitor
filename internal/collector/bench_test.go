package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writeFakeProc creates a synthetic proc tree: n processes chained as
// parent -> child, each with a stat, status and cmdline file plus a single
// task directory holding the children file.
func writeFakeProc(tb testing.TB, root string, n int) {
	tb.Helper()
	for i := 1; i <= n; i++ {
		dir := filepath.Join(root, strconv.Itoa(i))
		taskDir := filepath.Join(dir, "task", strconv.Itoa(i))
		if err := os.MkdirAll(taskDir, 0o755); err != nil {
			tb.Fatalf("mkdir %s: %v", taskDir, err)
		}

		ppid := i - 1
		if ppid == 0 {
			ppid = 1
		}
		// Layout after the comm: state ppid pgrp session tty tpgid flags
		// minflt cminflt majflt cmajflt utime stime
		stat := fmt.Sprintf("%d (proc %d) S %d 0 0 -1 0 0 0 0 0 0 0 20 0\n", i, i, ppid)
		writeFile(tb, filepath.Join(dir, "stat"), stat)

		status := fmt.Sprintf("Name:\tproc%d\nUmask:\t0022\nState:\tS (sleeping)\nPPid:\t%d\nVmRSS:\t%d kB\n", i, ppid, 1024*(i%10+1))
		writeFile(tb, filepath.Join(dir, "status"), status)

		writeFile(tb, filepath.Join(dir, "cmdline"), fmt.Sprintf("proc%d\x00--flag\x00", i))

		children := ""
		if i < n {
			children = strconv.Itoa(i + 1)
		}
		writeFile(tb, filepath.Join(taskDir, "children"), children)
	}
}

func writeFile(tb testing.TB, path, content string) {
	tb.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		tb.Fatalf("write %s: %v", path, err)
	}
}

// withFakeProc points the collector at a synthetic proc tree for the
// duration of the test and restores the real one afterwards.
func withFakeProc(tb testing.TB, n int) {
	tb.Helper()
	root := tb.TempDir()
	writeFakeProc(tb, root, n)
	old := procRoot
	procRoot = root
	tb.Cleanup(func() { procRoot = old })
}

func newBenchCollector() *Collector {
	return &Collector{
		cpuBaseline: make(map[int]cpuSample),
		totalRAM:    1 << 30,
	}
}

// BenchmarkBuildTree measures the cost of walking a process tree, including
// the number of /proc file reads per operation.
func BenchmarkBuildTree(b *testing.B) {
	for _, n := range []int{1, 10, 50} {
		b.Run(fmt.Sprintf("procs=%d", n), func(b *testing.B) {
			withFakeProc(b, n)
			c := newBenchCollector()

			b.ReportAllocs()
			procReadCount.Store(0)
			b.ResetTimer()
			for b.Loop() {
				c.buildTree(1, 0, -1, nil)
			}
			b.StopTimer()
			b.ReportMetric(float64(procReadCount.Load())/float64(b.N), "reads/op")
		})
	}
}
