package terminal

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/zotniq/pastetrail/internal/model"
	"github.com/zotniq/pastetrail/internal/platform"
)

func terminalName(s string) bool {
	s = strings.ToLower(s)
	for _, n := range []string{"terminal", "iterm", "wezterm", "alacritty", "kitty", "konsole", "xterm", "urxvt", "ghostty", "warp", "conhost", "mintty"} {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func Enrich(ctx context.Context, e *model.Event) {
	if !terminalName(e.Destination.Application.Name.Value + " " + e.Destination.Application.Executable.Value) {
		return
	}
	enrichTerminal(ctx, e, processInspector{
		activeTTY: activeTerminalTTY,
		list:      systemProcesses,
		command: func(ctx context.Context, pid int) (string, error) {
			return commandOutput(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "command=")
		},
		cwd: platform.CWD, tmux: inspectTmux, host: os.Hostname,
	})
}

func inferred[T any](value T) model.Field[T] {
	return model.Field[T]{Value: value, Evidence: model.Inferred, Reason: "process context sampled after the native input observation"}
}

func enrichTerminal(ctx context.Context, e *model.Event, in processInspector) {
	base := model.Context{Kind: "terminal", Name: e.Destination.Application.Name, PID: e.Destination.Application.PID}
	if host, err := in.host(); err == nil {
		base.Host = model.Known(host)
	}
	e.Destination.Contexts = append(e.Destination.Contexts, base)
	if e.Trigger.Kind == "mouse" && e.Trigger.Action == "middle_click" && runtime.GOOS == "darwin" {
		e.Destination.Contexts[0].Command = model.Missing[string]("clicked terminal pane cannot be identified from the keyboard-active session")
		return
	}
	processes, err := in.list(ctx)
	if err != nil {
		e.Destination.Contexts[0].Command = model.Missing[string](err.Error())
		return
	}
	byPID := make(map[int]process, len(processes))
	for _, p := range processes {
		byPID[p.pid] = p
	}
	var tty string
	var sessionErr error
	if in.activeTTY != nil {
		tty, sessionErr = in.activeTTY(ctx, e.Destination.Application)
	}
	if tty == "" {
		ttys := descendantTTYs(processes, byPID, e.Destination.Application.PID.Value)
		if len(ttys) != 1 {
			reason := "active terminal tab is ambiguous or its TTY is unavailable"
			if sessionErr != nil {
				reason += ": " + sessionErr.Error()
			}
			e.Destination.Contexts[0].TTY = model.Missing[string](reason)
			e.Destination.Contexts[0].Command = model.Missing[string](reason)
			return
		}
		for value := range ttys {
			tty = value
		}
	}
	e.Destination.Contexts[0].TTY = inferred(tty)
	visited := map[string]bool{}
	for depth := 0; depth < 4 && !visited[tty]; depth++ {
		visited[tty] = true
		var candidates []process
		for _, p := range processes {
			if p.tty == tty && p.group > 0 && p.group == p.foreground {
				candidates = append(candidates, p)
			}
		}
		if len(candidates) != 1 {
			e.Destination.Contexts = append(e.Destination.Contexts, model.Context{Kind: "command", TTY: inferred(tty), Host: base.Host, Name: model.Missing[string]("foreground process group contains zero or multiple possible recipients")})
			return
		}
		p := candidates[0]
		var ancestors []process
		seen := map[int]bool{p.pid: true}
		for parent := p.parent; parent > 0 && !seen[parent]; parent = byPID[parent].parent {
			seen[parent] = true
			q, ok := byPID[parent]
			if !ok || q.tty != tty {
				break
			}
			name := strings.TrimPrefix(filepath.Base(q.name), "-")
			if name == "bash" || name == "zsh" || name == "sh" || name == "fish" || name == "dash" {
				ancestors = append(ancestors, q)
			}
		}
		for _, q := range slices.Backward(ancestors) {
			e.Destination.Contexts = append(e.Destination.Contexts, model.Context{Kind: "shell", Name: inferred(strings.TrimPrefix(filepath.Base(q.name), "-")), PID: inferred(q.pid), Host: base.Host, TTY: inferred(tty)})
		}
		name := strings.TrimPrefix(filepath.Base(p.name), "-")
		c := model.Context{Kind: "command", Name: inferred(name), PID: inferred(p.pid), Host: base.Host, TTY: inferred(tty)}
		if command, err := in.command(ctx, p.pid); err == nil {
			c.Command = inferred(command)
		}
		if cwd, err := in.cwd(p.pid); err == nil {
			c.WorkingDirectory = inferred(cwd)
		}
		e.Destination.Contexts = append(e.Destination.Contexts, c)
		if strings.HasPrefix(name, "tmux") {
			pane, err := in.tmux(ctx, p.pid)
			if err != nil {
				e.Destination.Contexts = append(e.Destination.Contexts, model.Context{Kind: "tmux", Pane: model.Missing[string](err.Error())})
				return
			}
			e.Destination.Contexts = append(e.Destination.Contexts, model.Context{Kind: "tmux", Name: inferred(pane.command), Host: base.Host, Session: inferred(pane.session + ":" + pane.window), Pane: inferred(pane.pane), TTY: inferred(pane.tty), WorkingDirectory: inferred(pane.cwd)})
			tty = pane.tty
			continue
		}
		if name == "ssh" {
			remote := model.Context{Kind: "ssh", Name: model.Missing[string]("remote foreground command is not exposed by SSH"), PID: model.Missing[int]("remote PID is unavailable")}
			if host := sshDestination(c.Command.Value); host != "" {
				remote.Host = model.Field[string]{Value: host, Evidence: model.Inferred, Reason: "destination argument of the local SSH command; may be a configured alias"}
			}
			e.Destination.Contexts = append(e.Destination.Contexts, remote)
		}
		if container := containerDestination(c.Command.Value); container != "" {
			e.Destination.Contexts = append(e.Destination.Contexts, model.Context{Kind: "container", Container: model.Field[string]{Value: container, Evidence: model.Inferred, Reason: "container argument of a local exec command"}, PID: model.Missing[int]("container process identity is unavailable")})
		}
		return
	}
}

func descendantTTYs(processes []process, byPID map[int]process, appPID int) map[string]bool {
	ttys := map[string]bool{}
	for _, p := range processes {
		if p.tty == "?" || p.tty == "??" || p.tty == "" {
			continue
		}
		seen := map[int]bool{}
		for parent := p.parent; parent > 0 && !seen[parent]; parent = byPID[parent].parent {
			seen[parent] = true
			if parent == appPID {
				ttys[p.tty] = true
				break
			}
		}
	}
	return ttys
}
