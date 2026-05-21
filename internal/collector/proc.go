package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// /proc helpers (Linux only)

var clocksPerSec = uint64(100) // default; overridden by init() if available

func init() {
	// runtime.GOARCH is unused here; we just ensure Linux clock ticks default.
	_ = runtime.GOOS
}

// readStatTicks reads utime+stime from /proc/PID/stat (fields 14 and 15,
// 1-indexed). Returns 0 on any error; caller must handle the zero case.
func readStatTicks(pid int) uint64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	// The second field is the comm in parentheses and may contain spaces.
	// Find the last ')' to skip over it safely.
	raw := string(data)
	rp := strings.LastIndex(raw, ")")
	if rp < 0 {
		return 0
	}
	fields := strings.Fields(raw[rp+1:])
	// After ')' the remaining fields start at index 0 = field 3 (state).
	// field 14 (utime) → index 11, field 15 (stime) → index 12.
	if len(fields) < 13 {
		return 0
	}
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	return utime + stime
}

// readMemRSS returns the RSS of pid in bytes (VmRSS from /proc/PID/status).
func readMemRSS(pid int) int64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
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

// readCmdline returns argv[0] basename + args and the full raw cmdline.
func readCmdline(pid int) (short, full string) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(data) == 0 {
		// Kernel thread — read comm instead.
		comm, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		s := strings.TrimSpace(string(comm))
		return "[" + s + "]", "[" + s + "]"
	}
	// argv is NUL-separated.
	parts := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	full = strings.Join(parts, " ")
	base := filepath.Base(parts[0])
	if len(parts) > 1 {
		short = base + " " + strings.Join(parts[1:], " ")
	} else {
		short = base
	}
	return short, full
}

// readPPID reads the parent PID from /proc/PID/status.
func readPPID(pid int) int {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "PPid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		ppid, _ := strconv.Atoi(fields[1])
		return ppid
	}
	return 0
}

// readChildren returns the PIDs listed in /proc/PID/task/PID/children.
// This file is available on Linux kernels with CONFIG_PROC_CHILDREN=y.
// Falls back to scanning /proc for matching PPid entries when unavailable.
func readChildren(pid int) []int {
	taskDir := fmt.Sprintf("/proc/%d/task", pid)
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
	entries, err := os.ReadDir("/proc")
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
		if readPPID(pid) == ppid {
			children = append(children, pid)
		}
	}
	return children
}

// isPluginProcess returns true when the process is an actual tmux plugin.
// It matches executables or scripts located inside a /plugins/ folder,
// while filtering out standard system binaries and interactive developer tools.
func isPluginProcess(pid int) bool {
	exePath, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err == nil {
		// 1. If the compiled binary itself is inside the plugins folder, it's a plugin!
		if strings.Contains(exePath, "/plugins/") {
			return true
		}
		
		// Extract base name of the running executable
		baseName := exePath
		if idx := strings.LastIndex(exePath, "/"); idx >= 0 {
			baseName = exePath[idx+1:]
		}
		
		// 2. Blacklist system/interactive dev tools to prevent false positives when working in the dir
		switch baseName {
		case "git", "make", "nvim", "vim", "vi", "emacs", "nano", "go", "cargo", "npm", "yarn", "node", "python", "python3", "gcc", "clang", "gdb", "docker":
			return false
		}
	}

	// 3. Check if the command line arguments reference a script inside the plugins directory
	cmd, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err == nil {
		cmdline := string(cmd)
		for _, part := range strings.Split(cmdline, "\x00") {
			if strings.Contains(part, "/plugins/") {
				return true
			}
		}
	}

	return false
}

// totalRAMBytes returns the total physical memory in bytes.
func totalRAMBytes() int64 {
	data, err := os.ReadFile("/proc/meminfo")
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
	tickDiff := float64(curr.totalTicks - prev.totalTicks)
	return (tickDiff / float64(clocksPerSec) / elapsed) * 100.0
}

// pidExists returns true when /proc/PID/stat can be read.
func pidExists(pid int) bool {
	_, err := os.Stat(fmt.Sprintf("/proc/%d/stat", pid))
	return err == nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Wall-clock helper used in tests / real code uniformly.

func nowTS() time.Time { return time.Now() }

// readState reads the process state from /proc/PID/stat.
func readState(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "Unknown"
	}
	raw := string(data)
	rp := strings.LastIndex(raw, ")")
	if rp < 0 {
		return "Unknown"
	}
	fields := strings.Fields(raw[rp+1:])
	if len(fields) < 1 {
		return "Unknown"
	}
	switch fields[0] {
	case "R":
		return "running"
	case "S":
		return "sleeping"
	case "D":
		return "disk sleep"
	case "Z":
		return "zombie"
	case "T":
		return "stopped"
	case "t":
		return "tracing stop"
	case "X", "x":
		return "dead"
	case "I":
		return "idle"
	default:
		return fields[0]
	}
}

// getPluginName extracts a clean plugin name from process environment or command line.
func getPluginName(pid int, fullCmd string) string {
	// 1. Try environment variables (PWD or TMUX_PLUGIN_MANAGER_PATH)
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err == nil {
		env := string(data)
		for _, part := range strings.Split(env, "\x00") {
			if strings.HasPrefix(part, "PWD=") {
				pwd := part[4:]
				pIdx := strings.Index(pwd, "/plugins/")
				if pIdx >= 0 {
					sub := pwd[pIdx+len("/plugins/"):]
					end := strings.Index(sub, "/")
					if end > 0 {
						return sub[:end]
					}
					return sub
				}
			}
		}
	}

	// 2. Fall back to command line path
	idx := strings.Index(fullCmd, "/plugins/")
	if idx >= 0 {
		sub := fullCmd[idx+len("/plugins/"):]
		end := strings.Index(sub, "/")
		if end > 0 {
			return sub[:end]
		}
		// Try spaces or args
		endSpace := strings.Index(sub, " ")
		if endSpace > 0 {
			sub = sub[:endSpace]
		}
		endSlash := strings.Index(sub, "/")
		if endSlash > 0 {
			return sub[:endSlash]
		}
		return sub
	}

	return "tmux-plugin"
}


