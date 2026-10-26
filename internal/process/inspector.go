package process

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zotniq/pastetrail/internal/model"
	"github.com/zotniq/pastetrail/internal/platform"
)

type entry struct {
	pid, parent, group int
	tty, name          string
}

func list(ctx context.Context) ([]entry, error) {
	data, err := exec.CommandContext(ctx, "ps", "-ww", "-axo", "pid=,ppid=,pgid=,tty=,comm=").Output()
	if err != nil {
		return nil, err
	}
	var entries []entry
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		parent, e2 := strconv.Atoi(f[1])
		group, e3 := strconv.Atoi(f[2])
		if e1 != nil || e2 != nil || e3 != nil || pid <= 0 {
			continue
		}
		entries = append(entries, entry{pid, parent, group, f[3], strings.Join(f[4:], " ")})
	}
	return entries, nil
}

func inferred[T any](value T) model.Field[T] {
	return model.Field[T]{Value: value, Evidence: model.Inferred, Reason: "process metadata sampled after PTY input forwarding"}
}

func Ancestors(ctx context.Context, pid int) ([]int, model.Application) {
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	entries, err := list(ctx)
	if err != nil {
		return nil, model.Application{}
	}
	byPID := make(map[int]entry, len(entries))
	for _, p := range entries {
		byPID[p.pid] = p
	}
	var ids []int
	var app model.Application
	seen := map[int]bool{}
	for pid > 1 && !seen[pid] && len(ids) < 64 {
		seen[pid] = true
		ids = append(ids, pid)
		p, ok := byPID[pid]
		if !ok {
			break
		}
		if i := strings.Index(p.name, ".app/Contents/"); i >= 0 {
			bundle := p.name[:i+4]
			if app.PID.Value == 0 || (bundle == app.BundlePath.Value && strings.HasPrefix(p.name, bundle+"/Contents/MacOS/")) {
				reason := "application found in the relay's process ancestry at session startup"
				app.PID = model.Field[int]{Value: p.pid, Evidence: model.Inferred, Reason: reason}
				app.Name = model.Field[string]{Value: strings.TrimSuffix(filepath.Base(bundle), ".app"), Evidence: model.Inferred, Reason: reason}
				app.Executable = model.Field[string]{Value: p.name, Evidence: model.Inferred, Reason: reason}
				app.BundlePath = model.Field[string]{Value: bundle, Evidence: model.Inferred, Reason: reason}
			}
		}
		pid = p.parent
	}
	return ids, app
}

func Enrich(ctx context.Context, e *model.Event) {
	if e.Delivery == nil || len(e.Destination.Contexts) < 2 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	base := e.Destination.Contexts[0]
	shell := &e.Destination.Contexts[1]
	entries, err := list(ctx)
	if err != nil {
		e.Destination.Contexts = append(e.Destination.Contexts, model.Context{Kind: "command", Name: model.Missing[string](err.Error()), TTY: base.TTY, Host: base.Host, ProcessGroup: base.ProcessGroup})
		return
	}
	var foreground []entry
	for _, p := range entries {
		if p.pid == shell.PID.Value {
			shell.Name = inferred(strings.TrimPrefix(filepath.Base(p.name), "-"))
		}
		if base.ProcessGroup.Value > 0 && p.group == base.ProcessGroup.Value && strings.TrimPrefix(p.tty, "/dev/") == strings.TrimPrefix(base.TTY.Value, "/dev/") {
			foreground = append(foreground, p)
		}
	}
	p, resolved := foregroundApp(foreground, entries, shell.PID.Value)
	if !resolved {
		reason := "foreground process unavailable or changed before metadata sampling"
		if len(foreground) > 1 {
			reason = "foreground processes do not identify a single application"
		}
		e.Destination.Contexts = append(e.Destination.Contexts, model.Context{Kind: "command", Name: model.Missing[string](reason), TTY: base.TTY, Host: base.Host, ProcessGroup: base.ProcessGroup})
		return
	}
	c := model.Context{Kind: "command", Name: inferred(strings.TrimPrefix(filepath.Base(p.name), "-")), PID: inferred(p.pid), TTY: base.TTY, Host: base.Host, ProcessGroup: base.ProcessGroup, Session: base.Session}
	if len(foreground) > 1 {
		c.Name.Reason = "foreground job leader is the common ancestor of its sampled processes; the individual input reader is not confirmed"
	}
	if data, err := exec.CommandContext(ctx, "ps", "-ww", "-p", strconv.Itoa(p.pid), "-o", "command=").Output(); err == nil {
		c.Command = inferred(strings.TrimSpace(string(data)))
	}
	if cwd, err := platform.CWD(p.pid); err == nil {
		c.WorkingDirectory = inferred(cwd)
	}
	e.Destination.Contexts = append(e.Destination.Contexts, c)
}
