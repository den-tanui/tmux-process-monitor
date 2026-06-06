#!/usr/bin/env bash

# tmux-process-monitor main plugin file
# Sourced by TPM when the plugin is loaded

# Get script directory
SCRIPT_PATH="${BASH_SOURCE[0]}"
if [ -z "$SCRIPT_PATH" ]; then
    SCRIPT_PATH="$0"
fi
CURRENT_DIR="$(cd "$(dirname "$SCRIPT_PATH")" && pwd)"

BINARY="$CURRENT_DIR/bin/tmux-process-monitor"

# Ensure binary exists
if [ ! -x "$BINARY" ]; then
    "$CURRENT_DIR/scripts/install.sh"
fi

tmux bind-key t run-shell "$CURRENT_DIR/scripts/launch.sh"
