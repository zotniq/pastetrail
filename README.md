# PasteTrail

Go library and standalone CLI for observing paste attempts and their destination.
Apache-2.0. No Go module dependencies. Native capture uses small C/Objective-C adapters.

## Use

```sh
pastetrail doctor
pastetrail daemon --log pastetrail.jsonl
```

`daemon` runs in the foreground until interrupted, suitable for an interactive
user's service manager. It writes JSON Lines: startup/capability changes and
paste-attempt records. Each field distinguishes observed, inferred, and unavailable
data. A shortcut is evidence of an attempt, not proof that an application pasted.

Text and rich text have a total default limit of 1 MiB per event; change it with
`--max-content-bytes` (maximum 64 MiB). File references include names, paths/URIs,
and metadata, never file contents. Windows CF_HTML retains its native header as
`application/x-windows-cf-html`. Truncation, queue overflow, and capture failures
are reported. Logs contain the captured clipboard text.

```go
monitor, err := pastetrail.New(pastetrail.Options{})
if err != nil { return err }
encoder := json.NewEncoder(output)
return monitor.Run(ctx, func(record pastetrail.Record) error {
    return encoder.Encode(record)
})
```

Import `github.com/zotniq/pastetrail`. Callbacks run serially; return an error or
cancel the context to stop. One native monitor can run per process.
`NewWithSource` accepts application-provided observations for additional triggers.

## Detection limits

A standalone daemon cannot guarantee every paste attempt across all applications.
`doctor` reports build/session capabilities without requesting permissions.

| Feature | macOS | Windows | Linux X11 |
| --- | --- | --- | --- |
| Shortcut attempts | Cmd+V variants | Ctrl+V, Ctrl+Shift+V, Shift+Insert | Ctrl+V, Ctrl+Shift+V, Shift+Insert |
| Middle-click | iTerm2 saved clipboard/selection bindings; inferred | Unavailable | PRIMARY selection; recipient unavailable |
| Text, HTML, RTF, file references | Available formats | Available formats | Available formats; incremental transfers unavailable |
| Application/PID/window | Event target PID; available app/window attributes | Available OS attributes | Available window properties; local PID only |
| Browser | Window title; document URL when Accessibility exposes it | Window title | Window title |
| Terminal command, PID, cwd | iTerm2/Terminal active TTY; unambiguous local foreground process | Unavailable | Unambiguous local TTY |
| tmux | Active pane on default server socket | Unavailable | Active pane on default server socket |

Browser tab IDs, renderer PIDs and frames, menu/context-menu actions, app-internal/programmatic
pastes, and native Wayland capture are unavailable. Synthetic recognized shortcuts
may be captured, but their origin is not always identifiable. Clipboard and process
snapshots can race focus/content changes; custom key bindings and secure sessions
can be missed. Multiple terminal tabs or foreground processes may prevent attribution;
tmux synchronized panes are not mapped to all recipients.
SSH and container contexts identify recognizable **local** commands and destinations;
remote/container interior commands, PIDs, and working directories remain unavailable.

On macOS, middle-click detection matches iTerm2's saved Pointer bindings, including
modifiers and click counts. Runtime mouse reporting can override those bindings.
Selection pastes report `source: "iterm_selection"` with unavailable content: iTerm2's
most recent selection is private and may differ from the clipboard. Resolving the
clicked pane or retrieving that selection requires application integration. Custom
preferences folders, trackpad-only gestures, and other apps' mouse bindings are not supported.

macOS requires Input Monitoring. It resolves the keyboard event's target PID;
if absent, a fresh Accessibility focus query supplies an inferred destination.
If neither is available, the destination remains unavailable. Accessibility also
enables additional window/URL fields. No AppKit main run loop is required by the host.
Active iTerm2/Terminal sessions are queried through read-only Apple Events and may
require Automation access. To authorize iTerm2 from the terminal running the daemon:
`osascript -e 'tell application "iTerm2" to get tty of current session of current window'`.
For Terminal.app, use `osascript -e 'tell application "Terminal" to get tty of selected tab of front window'`.
The daemon reports denied access without requesting permission from its input callback.
Windows requires the interactive input desktop, not a session-zero service.
Linux requires an X11 session with RECORD and the `libxcb`/`libxcb-record` libraries.
Linux release jobs use Ubuntu 24.04; older libc versions are not a compatibility promise.

## Build and check

Requires Go 1.26+, cgo, and a native compiler: Xcode command-line tools on macOS;
MinGW-w64 GCC or LLVM on Windows; GCC/Clang plus `libxcb1-dev` and
`libxcb-record0-dev` on Debian/Ubuntu. End users do not need Go or a compiler.

```sh
go build -o bin/pastetrail ./cmd/pastetrail
go test -race -coverprofile=coverage.out ./...
python3 scripts/coverage.py
go tool cover -html=coverage.out -o coverage.html
go vet ./...
python3 scripts/package.py v0.1.0-dev
```

Packaging creates a binary archive and SHA-256 sidecar for the selected native
OS/architecture under `dist/`. CI builds separate macOS, Windows, and Linux artifacts
for amd64 and arm64. Artifacts are unsigned; the workflow does not publish releases.

The coverage gate requires **strictly greater than 95%** of all Go statements,
including the CLI. Go coverage excludes C/Objective-C and does not prove native
capture works. CI also exercises the native serializer and queue; macOS checks
event destination changes and focus fallback without posting input. Linux adds
isolated Xvfb clipboard/input/disconnection checks (`go test -tags=integration ./...`,
requiring Xvfb, xterm, xclip, xdotool, and xprop). macOS/Windows interactive capture
still needs verification on permission-enabled desktops. Go's race detector is
unavailable on Windows arm64; that job runs the regular suite.

See [LICENSE](LICENSE).
