#!/usr/bin/env bash

# tmux-process-monitor main plugin file
# Sourced by TPM when the plugin is loaded

# Get script directory
SCRIPT_PATH="${BASH_SOURCE[0]}"
if [ -z "$SCRIPT_PATH" ]; then
    SCRIPT_PATH="$0"
fi
CURRENT_DIR="$(cd "$(dirname "$SCRIPT_PATH")" && pwd)"

# Source helpers for get_option function
. "$CURRENT_DIR/scripts/helpers.sh"

BINARY="$CURRENT_DIR/bin/tmux-process-monitor"

# Ensure binary exists
if [ ! -x "$BINARY" ]; then
    "$CURRENT_DIR/scripts/install.sh"
fi

# Get config with defaults
REFRESH_RATE="$(get_option "@tmux_process_monitor_refresh_rate" "2.0")"
WIDTH="$(get_option "@tmux_process_monitor_width" "80%")"
HEIGHT="$(get_option "@tmux_process_monitor_height" "80%")"

tmux bind-key t run-shell "
    SESSION=\$(tmux display-message -p '#{session_name}');
    WINDOW=\$(tmux display-message -p '#{window_name}');
    CWD=\$(tmux display-message -p '#{pane_current_path}');
    tmux display-popup -T ' tmux-process-monitor ' -E -w '$WIDTH' -h '$HEIGHT' -d \"\$CWD\" '$BINARY' \"\$SESSION\" -w \"\$WINDOW\" -r '$REFRESH_RATE'
"
