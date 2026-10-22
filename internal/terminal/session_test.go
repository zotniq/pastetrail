package terminal

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/zotniq/pastetrail/internal/model"
	"github.com/zotniq/pastetrail/internal/platform"
)

func TestActiveTerminalSession(t *testing.T) {
	for _, name := range []string{"iTerm2", "iTerm", "Terminal"} {
		app := model.Application{Name: model.Known(name), PID: model.Known(1353)}
		tty, err := queryTerminalTTY(context.Background(), app, func(pid int, iterm bool) (string, int) {
			if pid != 1353 || iterm != (name != "Terminal") {
				t.Fatal(pid, iterm)
			}
			return "/dev/ttys003", 0
		})
		if err != nil || tty != "ttys003" {
			t.Fatal(tty, err)
		}
	}
	for _, tc := range []struct {
		name, tty   string
		pid, status int
	}{
		{"Other", "", 1, 0}, {"iTerm2", "", 0, 0},
		{"iTerm2", "", 1, -1743}, {"iTerm2", "", 1, -1744}, {"iTerm2", "", 1, -1712},
		{"Terminal", "", 1, 0}, {"Terminal", "ttys003", 1, 0}, {"Terminal", "/dev/", 1, 0},
		{"Terminal", "/dev/../file", 1, 0}, {"Terminal", "/dev/a\nb", 1, 0},
	} {
		_, err := queryTerminalTTY(context.Background(), model.Application{Name: model.Known(tc.name), PID: model.Known(tc.pid)}, func(int, bool) (string, int) { return tc.tty, tc.status })
		if err == nil {
			t.Fatal(tc)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := activeTerminalTTY(ctx, model.Application{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, iterm := range []bool{false, true} {
		if tty, status := platform.TerminalTTY(0, iterm); tty != "" || status == 0 {
			t.Fatal(tty, status)
		}
	}
}

func TestTerminalActiveTTYOverridesMultipleAndRestoredSessions(t *testing.T) {
	ps := []process{
		{pid: 10, parent: 1, group: 10, foreground: 20, tty: "a", name: "zsh"},
		{pid: 20, parent: 10, group: 20, foreground: 20, tty: "a", name: "vim"},
		{pid: 30, parent: 1, group: 30, foreground: 40, tty: "b", name: "zsh"},
		{pid: 40, parent: 30, group: 40, foreground: 40, tty: "b", name: "codex"},
		// Restored iTerm sessions need not descend from the current GUI process.
		{pid: 50, parent: 999, group: 50, foreground: 60, tty: "c", name: "bash"},
		{pid: 60, parent: 50, group: 60, foreground: 60, tty: "c", name: "codex"},
	}
	for _, tty := range []string{"b", "c", "a"} {
		in := inspector(ps)
		in.command = func(context.Context, int) (string, error) { return "codex", nil }
		in.activeTTY = func(context.Context, model.Application) (string, error) { return tty, nil }
		e := terminalEvent()
		enrichTerminal(context.Background(), &e, in)
		last := e.Destination.Contexts[len(e.Destination.Contexts)-1]
		want := "codex"
		if tty == "a" {
			want = "vim"
		}
		if last.Name.Value != want || last.TTY.Value != tty || e.Destination.Contexts[0].TTY.Value != tty {
			t.Fatal(e)
		}
	}
	in := inspector(ps)
	in.activeTTY = func(context.Context, model.Application) (string, error) { return "", errors.New("Automation denied") }
	e := terminalEvent()
	enrichTerminal(context.Background(), &e, in)
	if !strings.Contains(e.Destination.Contexts[0].Command.Reason, "Automation denied") {
		t.Fatal(e)
	}
	in.activeTTY = func(context.Context, model.Application) (string, error) { return "missing", nil }
	e = terminalEvent()
	enrichTerminal(context.Background(), &e, in)
	if e.Destination.Contexts[1].Name.Evidence != model.Unavailable {
		t.Fatal(e)
	}
}

func TestMouseDoesNotGuessKeyboardActivePane(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS pointer attribution")
	}
	e := terminalEvent()
	e.Trigger = model.Trigger{Kind: "mouse", Action: "middle_click"}
	enrichTerminal(context.Background(), &e, inspector(nil))
	if !strings.Contains(e.Destination.Contexts[0].Command.Reason, "clicked terminal pane") {
		t.Fatal(e)
	}
}
