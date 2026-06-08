#!/usr/bin/env bash

# tmux-process-monitor launcher
# Invoked by the keybinding; opens a tmux popup running the Go binary.

script_dir=$(dirname "$0")
script_dir=$(
	cd "$script_dir"
	pwd
)

. "$script_dir/helpers.sh"

PLUGIN_DIR="$(dirname "$script_dir")"
BINARY="$PLUGIN_DIR/bin/tmux-process-monitor"

# If binary is not installed, attempt install now
if [ ! -x "$BINARY" ]; then
	"$PLUGIN_DIR/scripts/install.sh"
fi

REFRESH_RATE=$(get_option "@tmux_process_monitor_refresh_rate" "2.0")
WIDTH=$(get_option "@tmux_process_monitor_width" "80%")
HEIGHT=$(get_option "@tmux_process_monitor_height" "80%")

SESSION_NAME=$(tmux display-message -p '#{session_name}')
WINDOW_NAME=$(tmux display-message -p '#{window_name}')
WINDOW_INDEX=$(tmux display-message -p '#{window_index}')
PANE_INDEX=$(tmux display-message -p '#{pane_index}')
CWD=$(tmux display-message -p '#{pane_current_path}')

# Pass --overview flag when called with that argument
if [ "$1" = "--overview" ]; then
	ARGS="--overview -r $REFRESH_RATE"
	TITLE=" tmux-process-monitor — System Overview "
else
	ARGS="-i $WINDOW_INDEX -p $PANE_INDEX -w $WINDOW_NAME -r $REFRESH_RATE $SESSION_NAME"
	TITLE=" tmux-process-monitor — Session: $SESSION_NAME "
fi

# shellcheck disable=SC2086
tmux display-popup -T "$TITLE" -E -w "$WIDTH" -h "$HEIGHT" -d "$CWD" "$BINARY" $ARGS
