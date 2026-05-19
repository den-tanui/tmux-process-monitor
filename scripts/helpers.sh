#!/usr/bin/env bash

# Helper functions for tmux-process-monitor

# get_option VAR DEFAULT
# Reads a global tmux option, returns default if unset or empty.
get_option() {
	local option="$1"
	local default_value="$2"
	local result
	result=$(tmux show-option -gqv "$option" 2>/dev/null)
	if [ -z "$result" ]; then
		echo "$default_value"
	else
		echo "$result"
	fi
}

# get_pane_pid
# Prints the PID of the current pane's shell process.
get_pane_pid() {
	tmux display-message -p "#{pane_pid}" 2>/dev/null
}
