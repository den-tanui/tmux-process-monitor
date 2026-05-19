// Package config reads plugin configuration from tmux global options.
package config

import (
	"os/exec"
	"strconv"
	"strings"
)

// Config holds all plugin configuration values.
type Config struct {
	MonitorKey   string
	OverviewKey  string
	RefreshRate  float64 // seconds
	PopupWidth   string  // e.g. "80%"
	PopupHeight  string  // e.g. "80%"
}

// Defaults holds the factory defaults.
var Defaults = Config{
	MonitorKey:  "m",
	OverviewKey: "M",
	RefreshRate: 2.0,
	PopupWidth:  "80%",
	PopupHeight: "80%",
}

// Load reads all plugin options from tmux, falling back to Defaults.
func Load() Config {
	return Config{
		MonitorKey:  getOption("@tmux_process_monitor_key", Defaults.MonitorKey),
		OverviewKey: getOption("@tmux_process_monitor_overview_key", Defaults.OverviewKey),
		RefreshRate: getFloat("@tmux_process_monitor_refresh_rate", Defaults.RefreshRate),
		PopupWidth:  getOption("@tmux_process_monitor_width", Defaults.PopupWidth),
		PopupHeight: getOption("@tmux_process_monitor_height", Defaults.PopupHeight),
	}
}

// getOption reads a global tmux option, returning the default on any error or
// empty result — exactly matching the behaviour of helpers.sh's get_option().
func getOption(option, defaultVal string) string {
	out, err := exec.Command("tmux", "show-option", "-gqv", option).Output()
	if err != nil {
		return defaultVal
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return defaultVal
	}
	return v
}

func getFloat(option string, defaultVal float64) float64 {
	s := getOption(option, "")
	if s == "" {
		return defaultVal
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return defaultVal
	}
	return v
}
