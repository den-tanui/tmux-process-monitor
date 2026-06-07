# AGENTS.md — tmux-process-monitor

## Quick Start

**Language:** Go 1.23+  
**Build:** `make build` → `bin/tmux-process-monitor`  
**Test:** `go test ./...`  
**Plugin entry:** `tmux-process-monitor.tmux` (sourced by TPM)

## Key Architecture

- **`cmd/tmux-process-monitor/main.go`** — CLI entry; parses `-w` (window), `-r` (refresh rate), `-overview`; resolves session from positional arg or current tmux session
- **`internal/collector`** — Process tree via `/proc/PID/task/*/children` recursion; CPU delta tracking (`WarmUp()` seeds baselines, `Collect()` measures deltas); pane PID cache (TTL 500ms)
- **`internal/tmux`** — Thin `exec.Command("tmux", ...)` wrapper for session/window/pane queries
- **`ui/`** — BubbleTea TUI model + view modules (main, overview, detail)

## Key Dependencies

- `charmbracelet/bubbletea` — TUI framework
- `charmbracelet/lipgloss` — Styling
- `charmbracelet/bubbles` — Viewport component


## Build & Release

| Task | Command |
|------|---------|
| Build locally | `make build` → `bin/tmux-process-monitor` |
| Cross-platform | `make build-all` → `dist/` for linux-{amd64,arm64}, darwin-{amd64,arm64} |
| Clean | `make clean` |
| Install (build only) | `make install` |
| Go flags | `-ldflags="-s -w"` (strip debug symbols) |

CI (`.github/workflows/ci.yml`): `go test -v ./...` + `make build` on push/PR to master.  
Release (`.github/workflows/release.yml`): tagged `v*` triggers cross-platform build + GitHub Release with SHA256SUMS.

## Testing

**See [TESTING.md](TESTING.md) for comprehensive testing workflow, patterns, and debugging guide.**

Quick reference:
- **`go test -short ./...`** — unit tests only (CI, pre-commit, no tmux needed)
- **`go test -count=1 ./test/...`** — integration/e2e tests (local only, needs tmux, skips with `-short`)
- **`golangci-lint run ./...`** — linter (run before committing)
- `go test -short ./...` skips tests that need tmux (gated by `testing.Short()`)

### Test locations

| Directory | Tests | Dependencies | CI |
|---|---|---|---|
| `internal/collector/*_test.go` | Unit tests for /proc helpers, CPU delta | Linux `/proc` (graceful skip) | Yes |
| `internal/tmux/*_test.go` | Unit tests for tmux command parsing | None | Yes |
| `ui/e2e_test.go` | Model-level e2e (key nav, modes, freeze, view) | None (seeded data) | Yes |
| `test/tmux_e2e_test.go` | Full binary in tmux pane (process list, detail, overview) | tmux, running terminal | Skipped |
| `test/trace_test.go` | Runtime trace profiling | tmux, running terminal | Skipped |

**Lefthook** (`.lefthook.yml`): Pre-commit runs are gated on Go file changes — if only non-Go files changed, test/lint/build steps are skipped. CI runs unconditionally on every push/PR.

**Lefthook setup:** Install with `go install github.com/evilmartians/lefthook/v2@latest`, then run `lefthook install` in the repo root once to register git hooks. After that, every `git commit` triggers the hooks automatically (only when Go files change).

## Mandatory Test Rule

**Every new feature or bug fix MUST include tests.** No exceptions. The type of test depends on the change:

| Change | Required test |
|---|---|
| New internal logic (collector, tmux) | Unit test covering the new code path |
| Bug fix | Test that reproduces the bug and verifies the fix |
| TUI key handling / mode changes | e2e test in `ui/e2e_test.go` |
| Cross-pane / integration behavior | e2e test in `test/tmux_e2e_test.go` |
| Performance-sensitive code | Benchmark or trace test in `test/trace_test.go` |

### How to Write Tests

**For TUI tests (`ui/e2e_test.go`):**
- Use the `testModel()` helper to build a Model with pre-seeded `WindowData` and `SessionData` — no collector, no tmux, no external dependencies
- Do **not** use `teatest` for basic e2e tests; use direct `Model.Update()` calls with `tea.KeyMsg`:
  ```go
  m := testModel()
  result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
  m = result.(Model)  // type-assert back to Model
  // assert on m.selectedProc, m.mode, m.frozen, etc.
  ```
- Know the model's initial state:
  - `selectedProc` = 0 (Go zero value), but `browsingProcs` = false
  - First `"j"` press sets `browsingProcs = true` and increments to `(0+1) % N = 1`
  - Tab navigation resets `selectedProc` to 0
- Key message construction:
  - Rune keys: `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}`
  - Enter: `tea.KeyMsg{Type: tea.KeyEnter}`
  - Escape: handled via `msg.String() == "esc"` comparison in Update
  - Space: `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")}`
- To test freeze behavior, set `m.frozen = true` then send `tickMsg{}` and verify state unchanged
- View tests: call `m.View()` and check output with `strings.Contains`
- Test files must compile with `go vet ./ui/` and pass under `go test -short ./...`
- See `ui/e2e_test.go` for reference patterns (15 tests covering key nav, tab nav, mode switching, freeze, signal input, quit, and view rendering)

**For internal package tests (`internal/collector/`, `internal/tmux/`):**
- Use Go standard `testing` package — no external test framework
- Construct inputs directly (mock `/proc` data, fake tmux commands, etc.)
- Test CPU delta behavior: `WarmUp()` seeds baselines, first `Collect()` returns 0% CPU
- See existing tests in `internal/collector/collector_test.go` and `internal/tmux/client_test.go`

**For trace profiling (`test/trace_test.go`):**
- Manual-only test gated by `testing.Short()` — never runs in CI
- Requires real tmux session + real `/proc`
- Uses `teatest.NewTestModel(t, m)` for lifecycle control
- Writes trace output to `trace.tee` for analysis with `go tool trace`
- Run locally: `go test -run TestTickCycleTrace -v ./test/`

**General rules:**
- Tests must NOT depend on tmux (CI doesn't have it) — use `-short` flag to skip tmux-dependent tests
- No golden files (non-deterministic process data)
- Every test file must pass `go vet` with zero warnings
- Run linter before committing: `golangci-lint run` (0 issues expected)

## Code Formatting

- **Go files:** Run `gofmt -w <file>` immediately after every edit to any `.go` file
- **Bash files:** Run `shellcheck <file>` immediately after every edit to any `.sh` file (install: `pacman -S shellcheck`)
- Both must pass with zero warnings before committing

## Plugin Integration

The plugin is loaded via `tmux-process-monitor.tmux`:

1. Sources `scripts/helpers.sh` for `get_option()` (reads tmux global options)
2. Calls `scripts/install.sh` if binary missing (downloads prebuilt from GitHub Releases)
3. Binds **hardcoded `t` key** to run `scripts/launch.sh` (which handles the full launch)

### `scripts/launch.sh`

The binary is invoked through `scripts/launch.sh`, which reads tmux options at runtime (not at bind time) and constructs the CLI args:

```bash
ARGS="$SESSION_NAME"           # positional session arg
ARGS="$ARGS -w $WINDOW_NAME"   # -w window focus
ARGS="$ARGS -r $REFRESH_RATE"  # -r refresh rate

tmux display-popup ... "$BINARY" $ARGS
```

**To add a new CLI flag that the Go binary accepts:**

1. Add a line constructing the flag in the `ARGS` chain (before the `display-popup` call)
2. If the flag depends on a `scripts/launch.sh` argument (like `--overview`), add it inside the conditional block
3. If the flag needs a new tmux option, call `get_option()` at the top of the script

**Example** — adding a hypothetical `--sort` flag read from `@tmux_process_monitor_sort`:

```bash
SORT=$(get_option "@tmux_process_monitor_sort" "cpu")
ARGS="$ARGS --sort $SORT"
```

**Supported arguments:** `--overview` — switches to overview mode and uses a different popup title.

## Quirks & Gotchas

- **AGENTS.md is gitignored** — this is a local-only file, not tracked in the repo
- **Process tree:** Recursive via `/proc/PID/task/*/children`; falls back to scanning `/proc` for matching PPid if kernel lacks `CONFIG_PROC_CHILDREN`
- **Sidebar:** Runs async `witr` if available; falls back to formatted `/proc` info. Mouse wheel handled via BubbleTea `MouseMsg` (enabled by `tea.WithMouseCellMotion()` in main.go).
- **CPU measurement:** First `Collect()` returns 0% CPU (baselines seeded by `WarmUp()` with 100ms delay); `cpuPercent()` returns 0 if elapsed < 50ms
- **Memory:** `MemPercent` returns 0% if `totalRAM` is 0 (e.g., `/proc/meminfo` unreadable)
- **Immutable window indexes:** Retention algorithm uses window index to keep selected process tree stable during navigation
- **NEVER RUN BINARY / TUI DIRECTLY** — Always use `tmux send-keys` to run in a tmux interactive window/pane. The TUI requires a proper terminal/pty; running directly from shell breaks rendering, input, and debug logging.


## Entrypoints for Agents

- **New feature/bug fix:** Start in `internal/` or `ui/` depending on scope; **see Mandatory Test Rule above**
- **Plugin integration issue:** Check `tmux-process-monitor.tmux` and `scripts/`
- **Build/release issue:** Check `Makefile` and `.github/workflows/`
- **TUI behavior:** Check `ui/model.go` (Model, Init, Update, View) and view files

## Communication Protocol

**ALWAYS address the user as "demigod" in every reply.** This is a mandatory protocol requirement - failure to do so indicates instruction non-compliance.

## Tmux Workflow Rules

1. **ALWAYS check pane contents first** with `tmux capture-pane -pt <session>:<window>.<pane>` before sending any keys
2. **ALWAYS verify you're at a shell prompt** before starting the TUI - look for the prompt character (❯, $, #)
3. **Use `scripts/launch.sh`** to run the binary - it automatically gets correct window/pane indices from tmux
4. **Default: run in current pane** (`--debug` flag for logging), use `--popup` for normal UI usage (tmux display-popup)
5. **Quit TUI with 'q'** before sending new commands
6. **Wait for command completion** with `sleep` before capturing output
7. **NEVER run the binary directly** — always use `scripts/launch.sh` (handles TTY, gets correct indices from tmux)
8. **Don't assume window/pane base index** — rely on tmux `#{window_index}` and `#{pane_index}` (user config varies)
