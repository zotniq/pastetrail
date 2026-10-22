# PasteTrail

A Go library and standalone CLI that observes paste attempts, snapshots available
clipboard content, and reports the destination application, browser, or terminal context.

**Status: early development.** Detection varies by OS and application. PasteTrail
cannot observe every paste attempt or confirm that content was inserted.

## Features

- Plain text, HTML, and RTF snapshots, with media type, encoding, size, and truncation metadata.
- File names, paths/URIs, and available size, directory, and modification-time metadata. File contents are never read.
- Trigger kind, shortcut/action, clipboard source, and evidence. iTerm2 mouse attempts also include button, click count, and modifiers; Windows injected shortcuts can be identified.
- Application name, executable, PID, window ID/title, bounds, and resident memory where exposed; macOS bundle ID/path and Windows content bounds.
- Versioned JSON Lines records with event IDs and timestamps, startup capabilities, and reports of truncation, queue overflow, or capture failure.
- An embeddable Go API with cancellation, serial callbacks, custom observation sources, bounded buffering, and duplicate-event tracking.

### Platform support

Native build targets are macOS (`darwin`), Windows, and Linux, each on `amd64` and
`arm64`. Entries describe implemented collection paths; permissions, application
behavior, and ambiguous focus can leave individual fields unavailable.

| Feature | macOS | Windows | Linux X11 |
| --- | --- | --- | --- |
| Keyboard attempts | Cmd+V, with Shift/Option variants | Ctrl+V, Ctrl+Shift+V, Shift+Insert | Ctrl+V, Ctrl+Shift+V, Shift+Insert |
| Middle-click attempts | iTerm2 saved clipboard/selection bindings | Unavailable | PRIMARY selection; recipient unavailable |
| Text, HTML, RTF, file references | Available clipboard formats | Available clipboard formats | Available selection formats; no incremental transfers |
| Application/window attribution | Event target PID; Accessibility focus fallback | Foreground application/window | Available window properties; local PID only |
| Browser context | Window title; active URL via Automation or Accessibility document fallback | Window title | Window title |
| Terminal command, PID, cwd | iTerm2/Terminal active TTY; unambiguous local foreground process | Unavailable | Unambiguous local TTY/foreground process |
| tmux context | Active pane on default server socket | Unavailable | Active pane on default server socket |
| SSH context | Local command and destination | Unavailable | Local command and destination |
| Container context | Container argument of a recognized local Docker/Podman exec command | Unavailable | Container argument of a recognized local Docker/Podman exec command |

macOS browser Automation supports Safari and Chromium families, including Chrome,
Edge, Brave, Vivaldi, Opera, and Arc. The [browser implementation](internal/browser/browser.go)
lists recognized bundle IDs. Firefox has no active-tab Automation integration.

## Installation

### Build from source

Requires Git, **Go 1.26+**, `CGO_ENABLED=1`, and a native compiler for your architecture.
There are no third-party Go module dependencies; capture uses C/Objective-C adapters.

| Platform | Native prerequisites |
| --- | --- |
| macOS | Xcode command-line tools |
| Windows | MinGW-w64 GCC on `PATH`, or compatible Clang configured with `CC=clang` |
| Linux | GCC/Clang and XCB development libraries (`libxcb1-dev`, `libxcb-record0-dev` on Debian/Ubuntu) |

```sh
git clone https://github.com/zotniq/pastetrail.git
cd pastetrail
```

macOS/Linux:

```sh
CGO_ENABLED=1 go build -o bin/pastetrail ./cmd/pastetrail
```

Windows PowerShell:

```powershell
$env:CGO_ENABLED = "1"
go build -o bin/pastetrail.exe ./cmd/pastetrail
```

### Binary distribution

[CI](.github/workflows/ci.yml) is configured to build one binary archive per
OS/architecture: `.tar.gz` for macOS/Linux and `.zip` for Windows, with SHA-256
sidecars. Artifacts are unsigned; CI uploads workflow artifacts without publishing
GitHub releases. Packaging locally is covered under [Development](#development).
Binary users do not need Go or a compiler. Linux still requires the `libxcb` and
`libxcb-record` runtime libraries.

## Quick start

From the repository directory:

```sh
./bin/pastetrail doctor
./bin/pastetrail daemon --log pastetrail.jsonl
```

On Windows, use `.\bin\pastetrail.exe` in place of `./bin/pastetrail`.
If `doctor` reports `permission_required`, complete the [permissions setup](#permissions-and-configuration).
It reports capabilities without requesting permissions; it does not verify every capture path.

`daemon` stays in the foreground. Paste `hello` into an application, then press
Ctrl+C in the daemon's terminal to stop. Omit `--log` to write to stdout.
The first JSONL record contains capabilities; subsequent records describe paste
attempts or capability changes.

Example macOS event excerpt, formatted for readability with other fields omitted:

```json
{
  "schema_version": 1,
  "type": "paste_attempt",
  "event": {
    "trigger": {"kind": "keyboard", "action": "cmd+v", "evidence": "inferred", "source": "system_clipboard"},
    "content": {
      "representations": [{"media_type": "text/plain", "data": "hello", "encoding": "utf-8", "size": 5}],
      "evidence": "observed"
    },
    "destination": {
      "application": {"name": {"value": "iTerm2", "evidence": "observed"}}
    }
  }
}
```

Evidence is `observed` for a direct observation, `inferred` for an attribution or
possible paste intent, and `unavailable` when a field cannot be obtained, with a
reason. A clipboard snapshot does not establish which representation an application consumed.
See the [event schema](internal/model/event.go) for all fields.

## Use as a Go library

In your own Go module, run `go get github.com/zotniq/pastetrail`. Save this as
`main.go` and run it with `go run .`, with `CGO_ENABLED=1` and the same native
prerequisites and permissions:

```go
package main

import (
    "context"
    "encoding/json"
    "errors"
    "log"
    "os"
    "os/signal"

    "github.com/zotniq/pastetrail"
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()

    monitor, err := pastetrail.New(pastetrail.Options{})
    if err != nil {
        log.Fatal(err)
    }
    encoder := json.NewEncoder(os.Stdout)
    err = monitor.Run(ctx, func(record pastetrail.Record) error {
        return encoder.Encode(record)
    })
    if err != nil && !errors.Is(err, context.Canceled) {
        log.Fatal(err)
    }
}
```

Callbacks run serially; a callback error or context cancellation stops collection.
One native monitor can run per process. `NewWithSource` accepts an application-provided
`Source` for additional triggers. No AppKit main run loop is required by the host.

## Permissions and configuration

### macOS permissions

Grant **Input Monitoring** to the terminal or installed executable running PasteTrail
in System Settings → Privacy & Security. **Accessibility** enables focus fallback
and additional window/document fields. Restart the host after changing permissions.

**Automation** access enables active terminal TTY and browser URL queries. From the
terminal that runs PasteTrail, run the relevant read-only query and authorize it:

```sh
# iTerm2 or Terminal.app
osascript -e 'tell application "iTerm2" to get tty of current session of current window'
osascript -e 'tell application "Terminal" to get tty of selected tab of front window'

# Chrome or Safari
osascript -e 'tell application "Google Chrome" to get URL of active tab of front window'
osascript -e 'tell application "Safari" to get URL of front document'
```

The daemon queries the attributed process and reports denied access without opening
a permission prompt from its input callback. Missing command/URL fields include a reason.

### Windows and Linux sessions

Windows requires an interactive input desktop; session-zero services are unsupported.
Linux requires an interactive X11 session with the RECORD extension. Wayland sessions,
including attempts to rely on XWayland for global capture, are unsupported.

### Options and data handling

| Daemon flag | Default | Behavior |
| --- | --- | --- |
| `--log FILE` | stdout | Append JSONL to a file |
| `--max-content-bytes N` | `1048576` (1 MiB) | Total text/rich-text payload limit per event; maximum 64 MiB; `0` selects the default |

The Go `Options` also provides `DedupWindow` (default 2 seconds) and
`MaxDedupEntries` (default 4096). Deduplication uses event identity; repeated pastes
of the same content remain separate attempts.

Logs contain captured text and available URLs, paths, and command lines. There is
no built-in redaction, encryption, or log rotation. File references use metadata
inspection without opening contents; non-local URIs are preserved without fetching
them. Non-UTF-8 representation bytes use base64. Windows CF_HTML retains its native
header as `application/x-windows-cf-html`.

## Missing features and current limits

- **Every paste attempt:** menu/context-menu actions, application-internal pastes, and direct programmatic insertion are unobservable through the native adapters. Recognized synthetic shortcuts may be captured, but automation origin is often unavailable.
- **Guaranteed attribution:** clipboard, focus, and process state can change during sampling. Multiple tabs or foreground processes can prevent identifying a recipient. Custom shortcuts, keyboard layouts, secure input, and elevated destinations can be missed.
- **Browser details:** tab IDs, renderer PIDs, and frame identity are unavailable. Active URL collection is macOS-only.
- **Terminal depth:** Windows command mapping, arbitrary terminal active-tab integrations, custom tmux server sockets, and attribution to every synchronized tmux pane are missing. SSH/container interior commands, PIDs, and working directories are unavailable.
- **Middle-click coverage:** macOS support is limited to iTerm2's saved Pointer bindings. Its selection content and clicked pane are unavailable; runtime mouse reporting can override the binding. Custom preference folders, trackpad-only gestures, and other apps' mouse mappings are unsupported. Linux middle-click does not identify the recipient.
- **Additional clipboard formats:** images and arbitrary binary formats are unsupported. Linux X11 incremental (`INCR`) selection transfers are unsupported; incomplete content is reported.
- **Service installation:** the CLI does not detach, install a background service, or manage startup. Use an interactive user's service manager to supervise it.

An unavailable field does not mean that a paste did not occur.

## Development

From the repository root, with cgo enabled and the native prerequisites installed:

```sh
go test -race -coverpkg=./... -coverprofile=coverage.out ./...
python3 scripts/coverage.py
go tool cover -html=coverage.out -o coverage.html
go vet ./...
python3 -m unittest discover -s scripts -p 'test_*.py'
python3 scripts/package.py
```

Use `python` if that is your Python 3 command. Omit `-race` on Windows arm64.
The coverage gate requires **strictly greater than 95% of all Go statements**,
including the CLI. Go coverage excludes C/Objective-C and does not establish native
capture correctness. CI separately checks native serialization/queue handling,
macOS attribution/Automation fixtures, and Linux Xvfb integration. Interactive
macOS/Windows behavior still needs checks on permission-enabled desktops.

For Linux integration, install Xvfb, xterm, xclip, xdotool, and xprop, then run
`go test -tags=integration ./...`. Packaging builds the current OS/architecture under
`dist/`, including the executable, README, licenses, archive, and checksum. Linux
CI uses Ubuntu 24.04; compatibility with older libc versions is not guaranteed.

### Source layout

The module root exposes the public API. Implementations are under `internal/`:

| Directory | Responsibility |
| --- | --- |
| [`application/`](internal/application), [`browser/`](internal/browser), [`terminal/`](internal/terminal) | Destination attribution; terminal helpers include tmux, SSH, and containers |
| [`capture/`](internal/capture) | Snapshot decoding, delivery, and destination enrichment |
| [`platform/`](internal/platform) | `macos/`, `windows/`, and `linux/` adapters; `native/` shares C serialization and the queue |
| [`content/`](internal/content), [`model/`](internal/model) | Clipboard formats, normalization, event types, and source contract |
| [`cli/`](internal/cli) | Command handling; [`cmd/pastetrail/`](cmd/pastetrail) is the executable entry point |

## Support and contributing

Open an [issue](https://github.com/zotniq/pastetrail/issues) with the OS/architecture,
`pastetrail version`, `doctor` output, application/terminal, trigger, and reproduction
steps. Remove captured content, private URLs, paths, and command arguments from examples.

For contributions, keep changes focused, add regression coverage for behavior changes,
run the relevant checks above, and update affected platform support/limitations here.
Include the platforms actually verified in your pull request.

## License

[Apache-2.0](LICENSE).
