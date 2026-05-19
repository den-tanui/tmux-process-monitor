#!/usr/bin/env bash

# tmux-process-monitor main plugin file
# Sourced by TPM when the plugin is loaded

script_dir=$(dirname "$0")
script_dir=$(
	cd "$script_dir"
	pwd
)

. "$script_dir/scripts/helpers.sh"

# Ensure the binary is present; run install if not
bin="$script_dir/bin/tmux-process-monitor"
if [ ! -x "$bin" ]; then
	"$script_dir/scripts/install.sh"
fi

# Monitor keybinding (default: m)
monitor_key=$(get_option "@tmux_process_monitor_key" "m")
monitor_script="$script_dir/scripts/launch.sh"

lowercase_key=$(echo "$monitor_key" | tr '[:upper:]' '[:lower:]')
if [ "$lowercase_key" != "none" ]; then
	tmux bind-key "${monitor_key}" run-shell -t "#{pane_id}" "\"$monitor_script\""
fi

# Overview keybinding (default: M)
overview_key=$(get_option "@tmux_process_monitor_overview_key" "M")
lowercase_overview_key=$(echo "$overview_key" | tr '[:upper:]' '[:lower:]')
if [ "$lowercase_overview_key" != "none" ]; then
	tmux bind-key "${overview_key}" run-shell -t "#{pane_id}" "\"$monitor_script\" --overview"
fi
