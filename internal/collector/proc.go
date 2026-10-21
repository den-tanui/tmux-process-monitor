package collector

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// /proc helpers (Linux only)

const statusUnknown = "Unknown"

// clocksPerSec is the kernel's USER_HZ: the unit of the utime/stime fields
// in /proc/PID/stat. It is 100 on every mainstream Linux configuration.
const clocksPerSec = 100

// procRoot is the root of the proc filesystem. Tests point it at a synthetic
// tree.
var procRoot = "/proc"

// procReadCount counts the reads of the proc filesystem. It exists so the
// benchmarks can report the number of file reads per operation.
var procReadCount atomic.Int64

// procPath builds a path under the proc root without going through
// fmt.Sprintf, which allocates and boxes the pid.
func procPath(pid int, name string) string {
	var buf [96]byte
	b := append(buf[:0], procRoot...)
	b = append(b, '/')
	b = strconv.AppendInt(b, int64(pid), 10)
	b = append(b, '/')
	b = append(b, name...)
	return string(b)
}

// readProcFile reads a file under the proc root and counts the read.
func readProcFile(pid int, name string) ([]byte, error) {
	procReadCount.Add(1)
	return os.ReadFile(procPath(pid, name))
}

// procStat is the subset of /proc/PID/stat the collector needs.
type procStat struct {
	ok    bool
	ppid  int
	state string
	ticks uint64
}

// parseStat parses /proc/PID/stat in a single pass.
//
// The second field is the comm, which is parenthesised and may contain
// spaces, so the scan starts after the LAST ')'. The fields that follow are
// indexed from 0: 0 = state, 1 = ppid, 11 = utime, 12 = stime.
func parseStat(data []byte) procStat {
	rp := bytes.LastIndexByte(data, ')')
	if rp < 0 {
		return procStat{}
	}

	var ps procStat
	var utime, stime uint64
	idx := 0
	for i := rp + 1; i < len(data) && idx <= 12; {
		for i < len(data) && data[i] == ' ' {
			i++
		}
		if i >= len(data) {
			break
		}
		start := i
		for i < len(data) && data[i] != ' ' {
			i++
		}
		switch idx {
		case 0:
			ps.state = stateName(data[start:i])
		case 1:
			ps.ppid = atoiBytes(data[start:i])
		case 11:
			utime = uint64(atoiBytes(data[start:i]))
		case 12:
			stime = uint64(atoiBytes(data[start:i]))
		}
		idx++
	}
	if idx < 13 {
		return procStat{}
	}
	ps.ok = true
	ps.ticks = utime + stime
	return ps
}

var (
	statusPPidPrefix  = []byte("PPid:")
	statusVMRSSPrefix = []byte("VmRSS:")
)

// parseStatus extracts the parent pid and the resident set size from
// /proc/PID/status in a single pass over the lines.
func parseStatus(data []byte) (ppid int, rssBytes int64) {
	for len(data) > 0 {
		line := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			data = nil
		}
		switch {
		case bytes.HasPrefix(line, statusPPidPrefix):
			ppid = atoiBytes(bytes.TrimSpace(line[len(statusPPidPrefix):]))
		case bytes.HasPrefix(line, statusVMRSSPrefix):
			rssBytes = int64(atoiBytes(bytes.TrimSpace(line[len(statusVMRSSPrefix):]))) * 1024
		}
		if ppid != 0 && rssBytes != 0 {
			break
		}
	}
	return ppid, rssBytes
}

// stateName maps a /proc/PID/stat state code to its display name.
func stateName(code []byte) string {
	if len(code) == 1 {
		switch code[0] {
		case 'R':
			return "running"
		case 'S':
			return "sleeping"
		case 'D':
			return "disk sleep"
		case 'Z':
			return "zombie"
		case 'T':
			return "stopped"
		case 't':
			return "tracing stop"
		case 'I':
			return "idle"
		case 'X', 'x':
			return "dead"
		}
	}
	return string(code)
}

// atoiBytes parses a non-negative decimal integer, stopping at the first
// byte that is not a digit.
func atoiBytes(b []byte) int {
	n := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// readCmdline returns argv[0] basename + args and the full raw cmdline.
func readCmdline(pid int) (short, full string) {
	data, err := readProcFile(pid, "cmdline")
	if err != nil || len(data) == 0 {
		// Kernel thread — read comm instead.
		comm, _ := readProcFile(pid, "comm")
		s := strings.TrimSpace(string(comm))
		return "[" + s + "]", "[" + s + "]"
	}
	// argv is NUL-separated.
	parts := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	if len(parts) == 0 || parts[0] == "" {
		return "[" + statusUnknown + "]", "[" + statusUnknown + "]"
	}
	full = strings.Join(parts, " ")
	base := filepath.Base(parts[0])
	if len(parts) > 1 {
		short = base + " " + strings.Join(parts[1:], " ")
	} else {
		short = base
	}
	return short, full
}

// readChildren returns the PIDs listed in /proc/PID/task/PID/children.
// This file is available on Linux kernels with CONFIG_PROC_CHILDREN=y.
// Falls back to scanning /proc for matching PPid entries when unavailable.
func readChildren(pid int) []int {
	taskDir := procPath(pid, "task")
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return scanChildrenFallback(pid)
	}

	var children []int
	var hasChildrenFile bool

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(taskDir, e.Name(), "children")
		data, err := os.ReadFile(path)
		if err == nil {
			hasChildrenFile = true
			for _, s := range strings.Fields(string(data)) {
				if c, e := strconv.Atoi(s); e == nil {
					children = append(children, c)
				}
			}
		}
	}

	if !hasChildrenFile {
		// Fallback: scan /proc for processes whose PPid matches.
		return scanChildrenFallback(pid)
	}
	return children
}

func scanChildrenFallback(ppid int) []int {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	var children []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := readProcFile(pid, "status")
		if err != nil {
			continue
		}
		if parent, _ := parseStatus(data); parent == ppid {
			children = append(children, pid)
		}
	}
	return children
}

// totalRAMBytes returns the total physical memory in bytes.
func totalRAMBytes() int64 {
	data, err := os.ReadFile(procRoot + "/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kb, _ := strconv.ParseInt(fields[1], 10, 64)
		return kb * 1024
	}
	return 0
}

// cpuPercent computes the CPU % between two samples.
// Returns the percentage of one core used (can exceed 100% for multi-threaded processes).
func cpuPercent(prev, curr cpuSample) float64 {
	elapsed := curr.ts.Sub(prev.ts).Seconds()
	if elapsed < 0.05 {
		return 0
	}
	if curr.totalTicks < prev.totalTicks {
		// The process exited or the PID was recycled: the baseline is stale
		// and the unsigned subtraction would wrap around.
		return 0
	}
	tickDiff := float64(curr.totalTicks - prev.totalTicks)
	return (tickDiff / float64(clocksPerSec) / elapsed) * 100.0
}

// ──────────────────────────────────────────────────────────────────────────────
// Wall-clock helper used in tests / real code uniformly.

func nowTS() time.Time { return time.Now() }
