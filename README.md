# PasteTrail

[![Build and tests](https://github.com/zotniq/pastetrail/actions/workflows/ci.yml/badge.svg?branch=main&event=push)](https://github.com/zotniq/pastetrail/actions/workflows/ci.yml?query=branch%3Amain)
[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](go.mod)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

A Go library and standalone CLI for recording paste attempts and their destinations.

PasteTrail records pasted content and available application, browser, and terminal
context. For enrolled local terminals, it can identify a destination such as
**GoLand → terminal session → Codex**, without a PasteTrail IDE plugin.

**Status: early development.** Coverage depends on the platform, permissions, and
application. A paste record does not prove that the destination consumed the content.

## Contents

- [Install](#install)
- [Usage](#usage)
- [Output](#output)
- [Go library](#go-library)
- [Limitations](#limitations)
- [Data handling](#data-handling)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Contributing](#contributing)
- [License](#license)

## Install

### Requirements

Building requires Git, Go 1.26 or later, cgo, and a native C compiler. The native
build targets are macOS, Windows, and Linux on `amd64` and `arm64`.

| Platform | Build prerequisites | Capture requirements |
| --- | --- | --- |
| macOS | Xcode command-line tools | Input Monitoring for native capture; Accessibility for focused controls |
| Linux | GCC/Clang, XCB and XCB Record development libraries | An authorized X11 session with the RECORD extension for native capture |
| Windows | MinGW-w64 GCC or compatible Clang matching the target architecture | An interactive input desktop; session-zero services are unsupported |

On Debian/Ubuntu, the XCB development packages are `libxcb1-dev` and
`libxcb-record0-dev`. Binary users need the corresponding runtime libraries.
Terminal relay capture is available on macOS/Linux. Windows supports native capture.

### Build from source

On macOS/Linux, with Make installed:

```sh
git clone https://github.com/zotniq/pastetrail.git
cd pastetrail
make build
./bin/pastetrail doctor
```

Make enables cgo and finds Go on PATH, in standard install locations, or in the
local Go toolchain cache. Override it with `make build GO=/path/to/go` if needed.
Without Make, use `CGO_ENABLED=1 go build -o bin/pastetrail ./cmd/pastetrail`.

On Windows, clone the same repository and run from its root in PowerShell:

```powershell
$env:CGO_ENABLED = "1"
go build -o bin/pastetrail.exe ./cmd/pastetrail
.\bin\pastetrail.exe doctor
```

Go and the compiler must be on PATH. Set `$env:CC = "clang"` when using Clang.
Use `.\bin\pastetrail.exe` for the executable in the examples below.

## Usage

### CLI

From the repository root, start the daemon:

```sh
./bin/pastetrail daemon --log pastetrail.jsonl
```

On macOS/Linux, `make run` builds the binary and runs that command. The daemon
stays in the foreground; Ctrl+C stops it. It does not start automatically at login.
Capture runs only while the daemon runs.

For terminal attribution, start the daemon first, then **open a new interactive
zsh/bash terminal** in your IDE or terminal app. Use the shell, Codex, Claude, or
another CLI normally. Already-open, unwrapped terminals are not attached.

On macOS, grant **Input Monitoring** to the executable or host running PasteTrail
under System Settings → Privacy & Security. **Accessibility** supplies focused-control
metadata; browser URL lookup may also require **Automation** access.
`doctor` reports capabilities without starting capture or requesting permissions.
Restart the relevant host/daemon after changing permissions.

### Options

| Daemon flag | Default | Purpose |
| --- | --- | --- |
| `--log FILE` | stdout | Append JSONL records to a file; `make run` defaults to `pastetrail.jsonl` |
| `--max-content-bytes N` | `1048576` | Bytes per content snapshot; `0` selects the default, maximum 64 MiB; terminal capture is capped at 1 MiB |
| `--socket PATH` | Per-user cache | Override the local terminal relay socket |
| `--terminal-hooks` | `true` | Temporarily enroll new zsh/bash sessions on macOS/Linux |
| `--detailed` | `false` | Include evidence, accessibility metadata, delivery payloads, and capability records |

For example: `make run LOG=pastes.jsonl RUN_ARGS="--detailed"`.

The socket defaults to `pastetrail/relay.sock` under the user's cache directory.
`PASTETRAIL_SOCKET` overrides that default; an explicit `--socket` takes precedence.
The daemon and manually launched relays must use the same socket.

### Terminal enrollment

The daemon temporarily adds marked blocks to user zsh/bash startup files,
including applicable login files and known `ZDOTDIR` configurations. New sessions
that read those files enter a pseudo-terminal (PTY) relay, which forwards their
input and observes paste boundaries. IDE application files are not modified.

- Normal shutdown removes the inserted blocks and preserves unrelated edits.
  Conflicting edits are reported and retained for recovery.
- After a crash, leftover hooks require a live daemon lease; the next start
  attempts recovery before installing new hooks.
- Existing relay sessions keep working while the daemon is stopped. They discard
  monitoring input while disconnected and reconnect when it returns at the same
  socket. There is no offline capture or replay.

To manage enrollment explicitly, start the daemon with `--terminal-hooks=false`,
then launch a relay in the terminal you want to monitor:

```sh
./bin/pastetrail shell -- /bin/zsh -l
```

The shell runs normally without capture if the daemon is unavailable. Custom startup
files, shells that skip those files, and IDE integration changes can require explicit
enrollment. One daemon owns automatic enrollment per user.

## Output

The default CLI output is JSON Lines: one JSON object per line. An illustrative
macOS record, expanded for readability:

```json
{
  "schema_version": 1,
  "type": "paste_attempt",
  "time": "2026-10-26T06:22:04.940645Z",
  "event": {
    "id": "bdb02d9b089fca48afb21658def67458",
    "time": "2026-10-26T06:22:04.940645Z",
    "captured_at": "2026-10-26T06:22:04.947922Z",
    "os": "darwin",
    "trigger": {
      "kind": "keyboard",
      "action": "cmd+v",
      "source": "system_clipboard"
    },
    "content": {
      "representations": [
        {"media_type": "text/plain", "data": "hello", "encoding": "utf-8", "size": 5}
      ]
    },
    "destination": {
      "application": {
        "name": "GoLand",
        "pid": 30049,
        "bundle_id": "com.jetbrains.goland"
      },
      "contexts": [{
        "kind": "foreground_process",
        "name": "codex",
        "pid": 24579,
        "host": "workstation",
        "tty": "/dev/ttys014",
        "command": "codex",
        "working_directory": "/projects/pastetrail",
        "session": "c94246923fe2763a06e40e87c1471d2d",
        "process_group": 24579
      }]
    }
  }
}
```

- `application` identifies the host app. Available executable, bundle, window,
  bounds, and memory fields are retained, along with available browser/document details.
- `contexts` identifies the selected foreground job, or the shell at its prompt.
  Ambiguous jobs retain terminal metadata without a guessed process name or PID.
- `content` appears once, preferring native plain text, then terminal plain text,
  then another clipboard representation. File metadata and truncation flags remain.
- Unknown attributes are omitted. Capture diagnostics go to stderr.

Text remains a JSON string, so quotes and line breaks are escaped. Terminal input
can contain `\r` line endings. Pasting an earlier JSON record places that entire
record inside `data`; it is not extra metadata added to the pasted text.

`--detailed` and library callbacks use the [full event schema](internal/model/event.go),
including `observed`, `inferred`, and `unavailable` evidence, capability records,
and `event.delivery`. Delivery observes bytes forwarded to a PTY; foreground
process selection and native-event correlation are inferred.
See [the CLI projection](internal/output/record.go) for the default output fields.

## Go library

Add the dependency to your Go module with
`go get github.com/zotniq/pastetrail`. The same native prerequisites and permissions apply.

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
	m, err := pastetrail.New(pastetrail.Options{})
	if err != nil {
		log.Fatal(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	err = m.Run(ctx, func(r pastetrail.Record) error { return encoder.Encode(r) })
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
```

`Run` delivers records serially until cancellation or a callback error. The library
does not edit shell startup files; terminal capture requires explicitly launched
relays connected to its socket. `DisableTerminalRelay` selects native capture alone;
`TerminalSocket` selects the socket. `NewWithSource` accepts a custom observation
source. See [the public API and options](monitor.go).

## Limitations

- Native global capture supports macOS, Windows, and Linux X11. Wayland/XWayland
  global capture is unsupported; enrolled terminal relays can still capture bracketed pastes.
- Menu actions, custom shortcuts, secure input, and application-internal insertion
  can evade native capture. Bracketed terminal pastes are recognized from their
  protocol markers; other terminal text requires a unique native clipboard match.
- IDE file paths, editor positions, and GUI extension chat inputs are available
  only when exposed through accessibility. A terminal's TTY is not an IDE tab label.
- SSH, containers, and tmux expose the local foreground process at the outer PTY;
  remote processes and inner panes are not resolved. Windows has no PTY relay support.
- Correlation waits about 750 ms. Delayed, transformed, truncated, or ambiguous
  input can produce separate native and terminal records. Each socket tracks up
  to 32 sessions; queues and payloads are bounded and capture loss is reported.
- Image capture, arbitrary binary clipboard formats, and Linux X11 incremental
  selection transfers are unsupported. Available text, HTML/RTF, and file references
  are captured; the default output selects one representation.

## Data handling

Logs contain pasted content and available URLs, paths, command lines, and process
metadata. The CLI writes to stdout or an append-only local log. There is no built-in
redaction, encryption, rotation, or retention policy. File references include
metadata; PasteTrail does not read their file contents.

Ordinary terminal input is forwarded and briefly buffered for paste matching,
but is not logged as paste content without a match or paste framing. On macOS/Linux,
relay sockets require a private directory owned by the current user with mode `0700`.

## Troubleshooting

| Symptom | What to check |
| --- | --- |
| No terminal context; zero relay sessions | Start the daemon, then open a new zsh/bash terminal. Check stderr for enrollment errors or use explicit enrollment. |
| Native paste events are missing | Run `doctor`; check macOS permissions, the Windows input desktop, or Linux X11 access. Terminal capture may continue independently. |
| An IDE input has no file path or command | GUI inputs need exposed accessibility metadata; terminal commands need an enrolled relay. |
| `data` contains escaped JSON or `\r` | Decode the JSON string to read the captured text. A pasted log record remains part of the content. |
| Go is not found | Install Go 1.26+ or specify `make build GO=/path/to/go`. Make also searches standard locations and cached toolchains. |

## Development

Run these from the repository root. Make targets use a POSIX shell; on Windows,
use an appropriate environment such as MSYS2. Python 3 is required for tooling
tests, the coverage gate, and packaging.

| Command | Purpose |
| --- | --- |
| `make build` / `make run` / `make doctor` | Build, start the daemon, or report capabilities |
| `make test` / `make test-race` | Run Go tests, optionally with the race detector |
| `make test-integration` | Run Go tests including Linux X11 integration |
| `make test-tools` | Run Python tooling tests |
| `make coverage` / `make coverage-check` | Generate coverage reports, optionally enforce the CI threshold |
| `make fmt` / `make fmt-check` / `make vet` | Format source, check formatting, or run Go diagnostics |
| `make check` | Run formatting checks, vet, Go tests, and Python tests |
| `make package` | Build a native archive and SHA-256 sidecar in `dist/` |
| `make clean` | Remove build, package, and coverage artifacts; keep paste logs |

`make help` lists targets and overrides, including `GO`, `GOFMT`, `PYTHON`, and
`TEST_FLAGS`. Linux integration needs Xvfb, xterm, xclip, xdotool, and xprop.
Use `make test` without `-race` on Windows arm64, as configured in CI.

The [CI workflow](.github/workflows/ci.yml) defines native builds and checks for all
six OS/architecture combinations. Its coverage gate requires **greater than 95%**
Go statement coverage. Go coverage excludes C/Objective-C; CI has separate native
checks. Interactive capture also needs checks on permission-enabled desktops.

Packaging produces unsigned `.tar.gz` archives for macOS/Linux and `.zip` archives
for Windows. CI uploads workflow artifacts and does not publish GitHub releases.
Linux CI builds on Ubuntu 24.04; compatibility with older libc versions is not guaranteed.

The public API is at the module root. Implementations live under `internal/`:
`capture`/`platform` collect native events, `terminal`/`terminalipc` handle correlation
and transport, `pty`/`relay` forward terminal traffic, `process` resolves jobs,
`enrollment` manages startup hooks, and `output` projects CLI records.

## Contributing

Questions, bug reports, and pull requests are welcome through the
[GitHub repository](https://github.com/zotniq/pastetrail).
For [issues](https://github.com/zotniq/pastetrail/issues), include your OS/architecture,
`pastetrail version`, `doctor` output, the destination app, and reproduction steps.
Remove private pasted content, URLs, paths, and command arguments before sharing logs.

Keep changes focused, update affected documentation, and include regression
coverage for behavior changes. Run the relevant development checks and state which
platforms you verified in the pull request.

## License

Licensed under [Apache-2.0](LICENSE).
