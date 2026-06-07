# TESTING.md — tmux-process-monitor

Comprehensive testing workflow for the tmux-process-monitor project.

## Overview

This project uses a multi-layered testing strategy:
- **Unit tests** (`go test -short ./...`) — Fast, no external dependencies, run in CI
- **Integration/E2E tests** (`go test -count=1 ./test/...`) — Require tmux, run locally only
- **TUI model tests** (`ui/e2e_test.go`) — Test Model.Update/View with seeded data
- **Trace profiling** (`test/trace_test.go`) — Manual performance analysis

---

## Quick Reference

| Command | Purpose | Dependencies |
|---------|---------|--------------|
| `go test -short ./...` | Unit tests only (CI) | None |
| `go test -count=1 ./test/...` | Integration tests | tmux, running terminal |
| `golangci-lint run ./...` | Linting | golangci-lint |
| `make build` | Build binary | Go 1.23+ |
| `go test -run TestTickCycleTrace -v ./test/` | Trace profiling | tmux, running terminal |

---

## Test Structure

```
internal/
├── collector/
│   └── collector_test.go      # Unit tests for /proc parsing, CPU delta
├── tmux/
│   └── client_test.go         # Unit tests for tmux command parsing
ui/
├── e2e_test.go                # Model-level e2e (key nav, modes, freeze, view)
├── model.go                   # Main TUI model
├── view_main.go               # Main view rendering
├── view_detail.go             # Detail view
├── view_overview.go           # Overview mode
└── view_help.go               # Help overlay
test/
├── tmux_e2e_test.go           # Full binary in tmux pane (process list, detail, overview)
└── trace_test.go              # Runtime trace profiling
```

---

## Unit Tests (CI)

Run in CI on every push/PR. No tmux required.

```bash
go test -short ./...
```

### What's Tested

**internal/collector/collector_test.go**
- Process tree construction via `/proc/PID/task/*/children`
- CPU delta tracking: `WarmUp()` seeds baselines, first `Collect()` returns 0% CPU
- Memory calculation: `MemPercent` returns 0% if `totalRAM` is 0
- Fallback to scanning `/proc` when `CONFIG_PROC_CHILDREN` unavailable

**internal/tmux/client_test.go**
- `ListSessions()` parsing
- `ListWindows()` parsing (name trimming)
- `ListPaneInfo()` format `#{pane_index}:#{pane_pid}`
- `CurrentSession()` resolution

### Adding Unit Tests

```go
// internal/collector/collector_test.go
func TestNewFeature(t *testing.T) {
    // Construct inputs directly (mock /proc data, fake tmux commands)
    // Test the new code path
    // Use testing.T for assertions
}
```

---

## TUI Model Tests (ui/e2e_test.go)

Tests the BubbleTea Model directly with seeded data — no collector, no tmux, no external dependencies.

```bash
go test -short ./ui/...
```

### Test Patterns

```go
// Use testModel() helper to build Model with pre-seeded data
m := testModel()

// Send key messages directly to Model.Update()
result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
m = result.(Model)  // type-assert back to Model

// Assert on state
if m.selectedProc != 1 {
    t.Errorf("expected selectedProc=1, got %d", m.selectedProc)
}

// View tests
view := m.View()
if !strings.Contains(view, "expected text") {
    t.Errorf("view missing expected text")
}
```

### Key Message Construction

```go
// Rune keys (j, k, h, l, space)
tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}

// Enter
tea.KeyMsg{Type: tea.KeyEnter}

// Escape (handled via msg.String() == "esc" in Update)
tea.KeyMsg{Type: tea.KeyEscape}

// Tab
tea.KeyMsg{Type: tea.KeyTab}
```

### Initial Model State

```go
// testModel() returns:
m.selectedProc    = 0  // Go zero value
m.browsingProcs   = false
m.currentTab      = 0
m.mode            = ViewMain
m.frozen          = false
```

### Navigation Behavior

- First `"j"` press: sets `browsingProcs = true`, increments to `(0+1) % N = 1`
- Tab navigation: resets `selectedProc = 0`
- Separator rows (`PID == 0`): skipped by j/k navigation

### Freeze Testing

```go
m.frozen = true
result, _ := m.Update(tickMsg{})
m = result.(Model)
// Verify state unchanged
```

---

## Integration Tests (test/tmux_e2e_test.go)

Full binary execution in a real tmux pane. Requires tmux session.

```bash
go test -count=1 ./test/...
```

### Test Setup

```go
func TestTmuxE2E(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }
    
    // Create tmux session
    session := "test-tmux-process-monitor"
    tmux.NewSession(session, "zsh")
    defer tmux.KillSession(session)
    
    // Build binary
    binary := buildBinary(t)
    
    // Launch in tmux pane
    pane := tmux.NewPane(session, binary+" test-session -r 0.1")
    defer pane.Close()
    
    // Wait for UI to render
    pane.WaitForText("Session:")
    
    // Send keys and capture output
    pane.SendKeys("j")
    output := pane.Capture()
    
    // Assert
    if !strings.Contains(output, "expected") {
        t.Errorf("expected output not found")
    }
}
```

### Helper Functions (test/helpers.go)

```go
// tmux.NewSession(name, shell) — creates detached session
// tmux.NewPane(session, cmd) — creates pane running cmd
// pane.SendKeys(keys...) — sends keystrokes
// pane.Capture() — captures pane content
// pane.WaitForText(text) — polls until text appears
// tmux.KillSession(name) — cleanup
```

---

## Trace Profiling (test/trace_test.go)

Manual-only test for performance analysis. Never runs in CI.

```bash
go test -run TestTickCycleTrace -v ./test/
```

### Usage

```go
func TestTickCycleTrace(t *testing.T) {
    if testing.Short() {
        t.Skip("trace test requires tmux")
    }
    
    m := createRealModel(t) // with real collector + tmux
    tm := teatest.NewTestModel(t, m)
    defer tm.Close()
    
    // Run for N ticks
    for i := 0; i < 10; i++ {
        tm.Send(tickMsg{})
        time.Sleep(100 * time.Millisecond)
    }
    
    // Writes trace.tee for: go tool trace trace.tee
}
```

---

## Debug Logging

The binary supports `-debug` flag for structured JSON logging to `/tmp/tmux-process-monitor.log`:

```bash
./bin/tmux-process-monitor test-session -w window-name -i 1 -p 1 -r 2.0 -debug
```

### Log Format (slog JSON)

```json
{"time":"2026-06-06T09:57:50.774682362+03:00","level":"DEBUG","msg":"debug logging enabled"}
{"time":"2026-06-06T09:57:51.123456789+03:00","level":"DEBUG","msg":"applyWindowData called","windowCount":2,"initialWinIdx":1,"currentWindows":0}
{"time":"2026-06-06T09:57:51.123456789+03:00","level":"DEBUG","msg":"checking window","index":0,"tmuxIndex":1,"name":"window-one"}
{"time":"2026-06-06T09:57:51.123456789+03:00","level":"DEBUG","msg":"matched window","currentTab":0,"tmuxIndex":1}
```

### Adding Debug Logs

```go
import "log/slog"

slog.Debug("operation name", "key1", value1, "key2", value2)
```

---

## Pre-Commit Hooks (Lefthook)

Configured in `.lefthook.yml`. Runs only when Go files change.

```bash
# Install once
go install github.com/evilmartians/lefthook/v2@latest
lefthook install

# Runs on every git commit (if Go files changed):
# - go test -short ./...
# - golangci-lint run ./...
# - make build
```

### Manual Pre-Commit

```bash
go test -short ./...
golangci-lint run ./...
make build
```

---

## CI Pipeline (.github/workflows/ci.yml)

Runs on every push/PR to master:

```yaml
- go test -v ./...          # All tests (short mode)
- make build                # Build verification
```

---

## Release Pipeline (.github/workflows/release.yml)

Triggered on tagged `v*` releases:

```yaml
- make build-all            # Cross-platform builds
- GitHub Release with SHA256SUMS
```

---

## Common Test Scenarios

### 1. Adding a New Key Binding

1. Add unit test in `ui/e2e_test.go`:
   ```go
   func TestNewKeyBinding(t *testing.T) {
       m := testModel()
       result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
       m = result.(Model)
       // assert new behavior
   }
   ```

2. Add integration test in `test/tmux_e2e_test.go` if it affects tmux interaction

### 2. Fixing a Bug in Collector

1. Add reproducing test in `internal/collector/collector_test.go`
2. Fix the code
3. Verify test passes

### 3. Changing Window/Pane Selection Logic

1. Update `ui/e2e_test.go` with new `testModel()` scenarios
2. Add debug logs with `slog.Debug()`
3. Test manually with `-debug` flag
4. Add integration test if complex tmux interaction

### 4. Modifying View Rendering

1. Test with `m.View()` in `ui/e2e_test.go`
2. Check `strings.Contains(view, "expected")`
3. Verify no layout corruption at different widths

---

## Debugging Tips

### View Debug Logs

```bash
tail -f /tmp/tmux-process-monitor.log
```

### Capture TUI State

```bash
# In tmux pane running the TUI
tmux capture-pane -pt session:window.pane
```

### Run Single Test

```bash
go test -v -run TestSpecificName ./ui/
go test -v -run TestSpecificName ./internal/collector/
```

### Verbose Output

```bash
go test -v ./...
go test -count=1 -v ./test/...
```

---

## Layered Debugging Methodology

When a feature doesn't work, isolate the failure layer by layer — from the
smallest logic unit outward to the real execution environment.

### The Layers

| Layer | What It Tests | Tools |
|-------|--------------|-------|
| **1. Core logic** | The function in isolation with known inputs | Direct function call in unit test |
| **2. Message pipeline** | Routing through `Model.Update()` | `sessionDataMsg{...}` via `m.Update()` |
| **3. User configuration** | The exact setup (window indices, base-index, etc.) | Variant of layer 2 with user's data |
| **4. Real execution** | The full binary in a tmux pane | `fmt.Fprintf(os.Stderr, ...)` + `tmux capture-pane` |
| **5. Compare** | Expected vs actual values at each stage | Diff test assertions against debug output |

### Worked Example: `-i` Flag Not Auto-Selecting Tab

The `initialWindowIndex` feature was broken in real use but all tests passed.
Here's how each layer narrowed the search:

#### Layer 1 — Unit test the matching logic

Write a test calling `applyWindowData` with `initialWindowIndex=1` and windows
`[{Index:0,"editor"}, {Index:1,"shell"}, {Index:2,"logs"}]`:

```go
m := New(nil, "test-session", "", 1, 2*time.Second)
incoming := []collector.WindowData{
    {Name: "editor", Index: 0},
    {Name: "shell",  Index: 1},
    {Name: "logs",   Index: 2},
}
m = m.applyWindowData(incoming)
if m.currentTab != 1 { t.Error(...) }
```

**Result:** Passed. The matching logic is correct.

#### Layer 2 — Test the message pipeline

Send a real `sessionDataMsg` through `Model.Update()` — the same path the
binary uses:

```go
sessions := []collector.SessionData{{
    Name: "test-session",
    Windows: []collector.WindowData{
        {Name: "editor", Index: 0},
        {Name: "shell",  Index: 1},
    },
}}
result, _ := m.Update(sessionDataMsg{sessions})
m = result.(Model)
```

**Result:** Passed. The message handler routes data correctly.

#### Layer 3 — Replicate user configuration

The user's tmux uses `base-index=1` (windows at indices 1, 2, 3, 4, not 0, 1, 2, 3).
Add a test matching their exact layout:

```go
Windows: []collector.WindowData{
    {Name: "nvim",    Index: 1},
    {Name: "opencode", Index: 2},
    {Name: "zsh",     Index: 3},
    {Name: "zsh",     Index: 4},
}
```

**Result:** Passed. The TUI *would* select the right tab if `-i 2` reached the model.

#### Layer 4 — Trace real execution

All model-level tests pass, so the problem must be upstream. Add
`fmt.Fprintf(os.Stderr, ...)` to the `sessionDataMsg` handler to dump real
values, then run the binary in a tmux pane and capture stderr:

```bash
tmux new-window -d -n testfix './bin/tmux-process-monitor tmux-process-monitor -i 2 -r 2.0'
tmux capture-pane -pt tmux-process-monitor:testfix
```

Pro tip: `fmt.Fprintf(os.Stderr, ...)` is the simplest tracer — no import
changes needed, no structured logging setup, and it shows up mixed into
`tmux capture-pane` output.

#### Layer 5 — Compare

Debug log shows `initialWindowIndex=-1` but test passed with `initialWindowIndex=2`.
The value never made it into the model — pointing to flag parsing, not the Go logic.

Root cause: Go's `flag.Parse()` stops at the first non-flag argument, so
`tmux-process-monitor mysession -i 2` never parsed `-i`.

### When to Use This Approach

- **Tests pass, real app fails** — always Layer 4 + 5
- **Complex data pipeline** (config → parse → route → model) — Layers 1→2→3
- **Environment-specific bug** (base-index, locale, terminal) — skip to Layer 3

---

## Test Coverage

```bash
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

---

## Performance Benchmarks

```bash
go test -bench=. -benchmem ./internal/collector/
go test -bench=. -benchmem ./internal/tmux/
```

---

## Troubleshooting

### Tests Hang

- Check for deadlocks in `Update()` or `View()`
- Ensure `tea.Quit` is returned on quit keys
- Verify no blocking operations in render path

### tmux Tests Fail

- Ensure tmux is running: `tmux ls`
- Check session name conflicts
- Verify binary builds: `make build`
- Increase wait times in `WaitForText()`

### Debug Logs Not Appearing

- Verify `-debug` flag passed
- Check `/tmp/tmux-process-monitor.log` permissions
- Ensure `slog.SetDefault()` called before any logging

### Lint Errors

```bash
golangci-lint run ./...
# Fix issues, then re-run
```

---

## Debugging with Delve

[Delve](https://github.com/go-delve/delve) is the Go debugger. It understands Go's runtime, goroutines, and data structures.

### Installation

```bash
go install github.com/go-delve/delve/cmd/dlv@latest
dlv version  # verify
```

### Debugging Tests

Run from the package directory containing `*_test.go` files:

```bash
# Debug a specific test in current package
cd ui
dlv test -run TestTeatest_WindowSelectionByIndex

# Debug all tests in package
dlv test

# Debug tests in specific package from repo root
dlv test github.com/den-tanui/tmux-process-monitor/ui -run TestTeatest_WindowSelectionByIndex
```

### Delve Commands

| Command | Short | Purpose |
|---------|-------|---------|
| `break` | `b` | Set breakpoint (file:line or function) |
| `continue` | `c` | Resume execution |
| `next` | `n` | Step over (next line) |
| `step` | `s` | Step into function |
| `stepout` | `so` | Step out of current function |
| `print` | `p` | Print variable/expression |
| `vars` | | List local variables |
| `goroutines` | `gr` | List all goroutines |
| `stack` | `bt` | Print stack trace |
| `list` | `l` | Show source code around current line |
| `quit` | `q` | Exit debugger |

### Common Debugging Workflows

#### 1. Break at Test Start

```bash
dlv test -run TestTeatest_WindowSelectionByIndex
(dlv) break e2e_test.go:507  # test function line
(dlv) continue
```

#### 2. Break at Specific Function

```bash
(dlv) break github.com/den-tanui/tmux-process-monitor/ui.applyWindowData
(dlv) continue
```

#### 3. Inspect Variables at Breakpoint

```bash
(dlv) print m.initialWinIdx
(dlv) print m.windows
(dlv) print len(m.windows)
(dlv) vars  # all locals
```

#### 4. Debug Goroutines

```bash
(dlv) goroutines
(dlv) goroutine 5  # switch to goroutine 5
(dlv) stack  # show stack for current goroutine
```

#### 5. Debug Panic

```bash
(dlv) break runtime/debug.Stack
(dlv) continue
# When panic occurs, inspect state
(dlv) print m
(dlv) stack
```

### Debugging TUI Applications

For TUI apps (bubbletea), debugging is trickier because the terminal is in raw mode:

1. **Use `dlv test` for unit tests** — runs in normal terminal
2. **Use `dlv debug` for binary** — but TUI takes over terminal
3. **Use `dlv exec` with pre-built binary** — same issue

**Workaround for TUI debugging:**
- Add `slog.Debug()` statements and use `-debug` flag
- Write unit tests that exercise Model.Update() directly (no TUI)
- Use `teatest` for integration tests (runs in virtual terminal)

### Delve with tmux

If debugging a test that launches tmux:

```bash
# In one terminal: start delve
dlv test -run TestTmuxE2E

# In another terminal: attach to tmux session to see UI
tmux attach -t test-session
```

### References

- [Delve Documentation](https://github.com/go-delve/delve/tree/master/Documentation)
- [dlv test docs](https://github.com/go-delve/delve/blob/master/Documentation/usage/dlv_test.md)
- [Delve Cheatsheet](https://appliedgo.net/spotlight/delve-cheat-sheet/)

---

## teatest — Full Program Integration Testing

`teatest` (from `github.com/charmbracelet/x/exp/teatest`) runs a real `tea.Program` in a virtual terminal, enabling end-to-end testing of the full TUI lifecycle including commands, async messages, and rendering.

### Installation

```bash
go get github.com/charmbracelet/x/exp/teatest@latest
```

### API Overview

```go
// Create a test model wrapping your tea.Model
tm := teatest.NewTestModel(t, m, 
    teatest.WithInitialTermSize(80, 24),  // optional terminal size
)

// Send messages to the program (key presses, ticks, etc.)
tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
tm.Send(tickMsg{})

// Type strings directly (convenience for key sequences)
tm.Type("hello")

// Get current output as io.Reader (for assertions)
output := tm.Output()

// Wait for program to finish (with timeout)
tm.WaitFinished(t)

// Get final model state for inspection
finalModel := tm.FinalModel(t)

// Get final output
finalOutput := tm.FinalOutput(t)

// Clean up
tm.Quit()
```

### Key Methods

| Method | Purpose |
|--------|---------|
| `NewTestModel(tb, model, opts...)` | Creates test wrapper |
| `Send(msg tea.Msg)` | Sends message to program |
| `Type(s string)` | Sends rune keys for string |
| `Output()` | Returns current output reader |
| `WaitFinished(tb, opts...)` | Blocks until program exits |
| `FinalModel(tb, opts...)` | Returns final model state |
| `FinalOutput(tb, opts...)` | Returns final output reader |
| `Quit()` | Stops the program |
| `GetProgram()` | Access underlying tea.Program |

### WaitFor Helper

```go
// Wait for output to satisfy condition
teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
    return bytes.Contains(bts, []byte("expected text"))
}, teatest.WithDuration(2*time.Second))
```

### Test Patterns

#### 1. Full Flow Test

```go
func TestFullFlow(t *testing.T) {
    m := createRealModel(t) // with real collector
    tm := teatest.NewTestModel(t, m)
    defer tm.Quit()

    // Wait for initial render
    teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
        return bytes.Contains(bts, []byte("Session:"))
    })

    // Navigate
    tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
    tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

    // Open detail view
    tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

    // Verify detail view rendered
    teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
        return bytes.Contains(bts, []byte("Process Detail"))
    })

    // Close detail
    tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})

    // Verify back to main
    finalModel := tm.FinalModel(t)
    if finalModel.(ui.Model).mode != ui.ViewMain {
        t.Errorf("expected ViewMain, got %v", finalModel.(ui.Model).mode)
    }
}
```

#### 2. Output Assertion Test

```go
func TestOutputContains(t *testing.T) {
    m := testModel()
    tm := teatest.NewTestModel(t, m)
    defer tm.Quit()

    tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

    output := tm.Output()
    bts, _ := io.ReadAll(output)
    
    if !bytes.Contains(bts, []byte("expected process")) {
        t.Errorf("output missing expected text: %s", string(bts))
    }
}
```

#### 3. State Inspection Test

```go
func TestModelState(t *testing.T) {
    m := testModel()
    tm := teatest.NewTestModel(t, m)
    defer tm.Quit()

    tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
    tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

    finalModel := tm.FinalModel(t)
    model := finalModel.(ui.Model)
    
    if model.selectedProc != 2 {
        t.Errorf("expected selectedProc=2, got %d", model.selectedProc)
    }
    if !model.browsingProcs {
        t.Error("expected browsingProcs=true")
    }
}
```

### Options

```go
// Terminal size
teatest.WithInitialTermSize(width, height)

// WaitFor options
teatest.WithDuration(5 * time.Second)        // max wait time
teatest.WithCheckInterval(50 * time.Millisecond) // poll interval

// FinalModel/FinalOutput options
teatest.WithFinalTimeout(3 * time.Second)
teatest.WithTimeoutFn(func(tb testing.TB) { tb.Fatal("timeout") })
```

### Integration with trace_test.go

```go
func TestTickCycleTrace(t *testing.T) {
    if testing.Short() {
        t.Skip("trace test requires tmux")
    }
    
    m := createRealModel(t)
    tm := teatest.NewTestModel(t, m)
    defer tm.Quit()
    
    // Enable tracing
    f, _ := os.Create("trace.tee")
    defer f.Close()
    trace.Start(f)
    defer trace.Stop()
    
    // Run for N ticks
    for i := 0; i < 10; i++ {
        tm.Send(tickMsg{})
        time.Sleep(100 * time.Millisecond)
    }
    
    // Analyze: go tool trace trace.tee
}
```

### Best Practices

1. **Always defer `tm.Quit()`** — prevents orphaned goroutines
2. **Use `WaitFor` for async operations** — don't sleep blindly
3. **Test at the right layer** — 80% direct model tests, 15% golden files, 5% teatest
4. **Keep tests fast** — teatest is slower than direct model tests
5. **Use `FinalModel` for state assertions** — more reliable than output parsing

### Common Issues

| Issue | Solution |
|-------|----------|
| Test hangs | Ensure `tea.Quit` returned on quit keys; check for deadlocks |
| Output empty | Wait for initial render with `WaitFor` before sending keys |
| Flaky tests | Increase `WithDuration` and `WithCheckInterval` |
| Terminal size | Set `WithInitialTermSize` for consistent rendering |

### References

- [teatest Source](https://github.com/charmbracelet/x/tree/main/exp/teatest)
- [Charm Blog: Writing Bubble Tea Tests](https://charm.sh/blog/teatest/)
- [teatest v2 Discussion](https://github.com/charmbracelet/bubbletea/discussions/1528)

---

## References

- [BubbleTea Tutorial](https://github.com/charmbracelet/bubbletea/tree/main/tutorials/basics)
- [BubbleTea Examples](https://github.com/charmbracelet/bubbletea/tree/main/examples)
- [slog Documentation](https://pkg.go.dev/log/slog)
- [Go Testing Package](https://pkg.go.dev/testing)
- [teatest](https://github.com/charmbracelet/x/exp/teatest)
- [teatest Blog Post](https://charm.sh/blog/teatest/)