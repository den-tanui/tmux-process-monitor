# tmux-process-monitor Design

**Date:** 2026-05-19
**Status:** Approved

## Overview

A tmux plugin for monitoring resource usage (CPU and memory) of processes grouped by tmux windows, rewritten in Go with enhanced features including process tree visualization, per-session graphs, and extended process information.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      tmux (trigger)                         │
│                    prefix + keybinding                      │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│              Shell installer script (install.sh)            │
│         Detects OS/arch → downloads pre-built binary        │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                    Go binary (main)                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │
│  │  bubbletea  │  │   process   │  │    graph renderer   │ │
│  │     TUI     │◄─┤   collector │  │   (per-session)     │ │
│  └─────────────┘  └─────────────┘  └─────────────────────┘ │
│         │                │                    │            │
│         └────────────────┼────────────────────┘            │
│                          ▼                                   │
│              ┌───────────────────────┐                      │
│              │   tmux integration    │                      │
│              │  (session/window API)│                      │
│              └───────────────────────┘                      │
└─────────────────────────────────────────────────────────────┘
```

## Components

### 1. Shell Installer (`scripts/install.sh`)

- Detects OS (Linux/Darwin) and architecture (x64/arm64)
- Fetches appropriate binary from GitHub releases
- Sets executable permissions
- Creates wrapper script in TPM path

### 2. Process Collector (`internal/collector/`)

- Queries tmux for active sessions and windows via `tmux list-*` commands
- Maps tmux panes to their child processes using `ps` with process tree
- Collects CPU%, memory%, PID, command, parent-child relationships
- Identifies plugin-spawned processes (by detecting tmux plugin environment)
- Refreshes at configurable interval (default: 2s)

### 3. Bubbletea TUI (`ui/`)

- **Main view**: Process list grouped by tmux window
- **Process tree mode**: Collapsible parent-child hierarchy
- **Graph view**: Per-session CPU/memory graphs (last 60 data points)
- **Process detail view**: Extended info panel
- **Navigation**: vim-style (h/j/k/l), window switching
- **Actions**: Kill (SIGTERM), send custom signals, yank PID/command

### 4. Graph Renderer (`ui/graph/`)

- Renders ASCII-style line graphs using lipgloss styling
- Tracks history per session (configurable window, default 60 samples)
- Shows CPU and memory trends

### 5. Tmux Integration (`internal/tmux/`)

- Wraps `tmux list-sessions`, `tmux list-windows`, `tmux list-panes`
- Parses output into structured data
- Handles tmux not running edge case

## Data Flow

```
User presses prefix+m → shell script launches Go binary
                                    │
                                    ▼
                         Bubbletea initializes
                         │
            ┌────────────┼────────────┐
            ▼            ▼            ▼
      collector    graph store   config store
      (periodic)   (in-memory)   (from tmux conf)
            │            │            │
            └────────────┼────────────┘
                         ▼
                   TUI renders
                   (re-renders on each tick)
```

## Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `@tmux_process_monitor_key` | `m` | Keybinding to open monitor |
| `@tmux_process_monitor_refresh_rate` | `2.0` | Refresh interval in seconds |
| `@tmux_process_monitor_width` | `80%` | Popup width |
| `@tmux_process_monitor_height` | `80%` | Popup height |

## Keyboard Controls

- `h`/`l` or `←`/`→` — Navigate between windows
- `j`/`k` or `↑`/`↓` — Navigate up/down through processes
- `Enter` — Toggle process tree / view details
- `g` — Toggle graph view
- `x` — Send SIGTERM to selected process
- `s` — Send custom signal
- `y` — Yank command to clipboard
- `Y` — Yank PID to clipboard
- `?` — Show help
- `q` or `Q` — Quit

## Error Handling

- **tmux not running**: Show friendly message, exit cleanly
- **Process collection fails**: Log error, show last known state, retry on next tick
- **Binary not found**: Installer shows clear error with OS-specific instructions

## Dependencies

- Go 1.21+
- bubbletea (TUI framework)
- lipgloss (styling)
- tcell or termbox (terminal capabilities)

## Testing Strategy

- **Unit tests**: Process collector parsing, graph data transformation
- **Integration tests**: tmux command execution (mock or real tmux instance)
- **Manual testing**: Run in actual tmux session with various process configurations