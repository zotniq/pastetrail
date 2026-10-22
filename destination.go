package pastetrail

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

type process struct {
	pid, parent, group, foreground int
	tty, name                      string
}
type processInspector struct {
	list    func(context.Context) ([]process, error)
	command func(context.Context, int) (string, error)
	cwd     func(int) (string, error)
	tmux    func(context.Context, int) (paneInfo, error)
	host    func() (string, error)
}
type paneInfo struct {
	session, window, pane, tty, command, cwd string
	pid                                      int
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	data, err := exec.CommandContext(ctx, name, args...).Output()
	return strings.TrimSpace(string(data)), err
}

func systemProcesses(ctx context.Context) ([]process, error) {
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("windows terminal pane-to-process mapping is unavailable")
	}
	out, err := commandOutput(ctx, "ps", "-axo", "pid=,ppid=,pgid=,tpgid=,tty=,comm=")
	if err != nil {
		return nil, err
	}
	return parseProcesses(out), nil
}

func parseProcesses(output string) []process {
	var result []process
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		var nums [4]int
		valid := true
		for i := range nums {
			n, err := strconv.Atoi(fields[i])
			if err != nil {
				valid = false
				break
			}
			nums[i] = n
		}
		if valid && nums[0] > 0 {
			result = append(result, process{nums[0], nums[1], nums[2], nums[3], fields[4], strings.Join(fields[5:], " ")})
		}
	}
	return result
}

func inspectTmux(ctx context.Context, pid int) (paneInfo, error) {
	return readTmux(ctx, pid, commandOutput)
}

func readTmux(ctx context.Context, pid int, run func(context.Context, string, ...string) (string, error)) (paneInfo, error) {
	clients, err := run(ctx, "tmux", "list-clients", "-F", "#{client_pid}\t#{session_id}")
	if err != nil {
		return paneInfo{}, err
	}
	for _, line := range strings.Split(clients, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) != 2 || parts[0] != strconv.Itoa(pid) {
			continue
		}
		out, err := run(ctx, "tmux", "list-panes", "-t", parts[1], "-F", "#{pane_active}\t#{pane_id}\t#{pane_pid}\t#{pane_tty}\t#{pane_current_command}\t#{pane_current_path}\t#{window_id}")
		if err != nil {
			return paneInfo{}, err
		}
		return parsePane(parts[1], out)
	}
	return paneInfo{}, fmt.Errorf("tmux client was not found on the default server socket")
}

func parsePane(session, output string) (paneInfo, error) {
	var found []paneInfo
	for _, line := range strings.Split(output, "\n") {
		p := strings.Split(line, "\t")
		if len(p) != 7 || p[0] != "1" {
			continue
		}
		pid, err := strconv.Atoi(p[2])
		if err != nil || pid <= 0 {
			continue
		}
		found = append(found, paneInfo{session: session, window: p[6], pane: p[1], tty: strings.TrimPrefix(p[3], "/dev/"), command: p[4], cwd: p[5], pid: pid})
	}
	if len(found) != 1 {
		return paneInfo{}, fmt.Errorf("tmux active pane could not be identified uniquely")
	}
	return found[0], nil
}

func terminalName(s string) bool {
	s = strings.ToLower(s)
	for _, n := range []string{"terminal", "iterm", "wezterm", "alacritty", "kitty", "konsole", "xterm", "urxvt", "ghostty", "warp", "conhost", "mintty"} {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func enrichDestination(ctx context.Context, e *Event) {
	if !terminalName(e.Destination.Application.Name.Value + " " + e.Destination.Application.Executable.Value) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	enrichTerminal(ctx, e, processInspector{
		list: systemProcesses,
		command: func(ctx context.Context, pid int) (string, error) {
			return commandOutput(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "command=")
		},
		cwd: processCWD, tmux: inspectTmux, host: os.Hostname,
	})
}

func inferred[T any](value T) Field[T] {
	return Field[T]{Value: value, Evidence: Inferred, Reason: "process context sampled after the native input observation"}
}

func enrichTerminal(ctx context.Context, e *Event, in processInspector) {
	base := Context{Kind: "terminal", Name: e.Destination.Application.Name, PID: e.Destination.Application.PID}
	if host, err := in.host(); err == nil {
		base.Host = Known(host)
	}
	e.Destination.Contexts = append(e.Destination.Contexts, base)
	processes, err := in.list(ctx)
	if err != nil {
		e.Destination.Contexts[0].Command = Missing[string](err.Error())
		return
	}
	byPID := make(map[int]process, len(processes))
	for _, p := range processes {
		byPID[p.pid] = p
	}
	ttys := map[string]bool{}
	for _, p := range processes {
		if p.tty == "?" || p.tty == "??" || p.tty == "" {
			continue
		}
		seen := map[int]bool{}
		for parent := p.parent; parent > 0 && !seen[parent]; parent = byPID[parent].parent {
			seen[parent] = true
			if parent == e.Destination.Application.PID.Value {
				ttys[p.tty] = true
				break
			}
		}
	}
	if len(ttys) != 1 {
		e.Destination.Contexts[0].Command = Missing[string]("active terminal tab is ambiguous or its TTY is unavailable")
		return
	}
	var tty string
	for value := range ttys {
		tty = value
	}
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
			e.Destination.Contexts = append(e.Destination.Contexts, Context{Kind: "command", TTY: inferred(tty), Host: base.Host, Name: Missing[string]("foreground process group contains zero or multiple possible recipients")})
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
			e.Destination.Contexts = append(e.Destination.Contexts, Context{Kind: "shell", Name: inferred(strings.TrimPrefix(filepath.Base(q.name), "-")), PID: inferred(q.pid), Host: base.Host, TTY: inferred(tty)})
		}
		name := strings.TrimPrefix(filepath.Base(p.name), "-")
		c := Context{Kind: "command", Name: inferred(name), PID: inferred(p.pid), Host: base.Host, TTY: inferred(tty)}
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
				e.Destination.Contexts = append(e.Destination.Contexts, Context{Kind: "tmux", Pane: Missing[string](err.Error())})
				return
			}
			e.Destination.Contexts = append(e.Destination.Contexts, Context{Kind: "tmux", Name: inferred(pane.command), Host: base.Host, Session: inferred(pane.session + ":" + pane.window), Pane: inferred(pane.pane), TTY: inferred(pane.tty), WorkingDirectory: inferred(pane.cwd)})
			tty = pane.tty
			continue
		}
		if name == "ssh" {
			remote := Context{Kind: "ssh", Name: Missing[string]("remote foreground command is not exposed by SSH"), PID: Missing[int]("remote PID is unavailable")}
			if host := sshDestination(c.Command.Value); host != "" {
				remote.Host = Field[string]{Value: host, Evidence: Inferred, Reason: "destination argument of the local SSH command; may be a configured alias"}
			}
			e.Destination.Contexts = append(e.Destination.Contexts, remote)
		}
		if container := containerDestination(c.Command.Value); container != "" {
			e.Destination.Contexts = append(e.Destination.Contexts, Context{Kind: "container", Container: Field[string]{Value: container, Evidence: Inferred, Reason: "container argument of a local exec command"}, PID: Missing[int]("container process identity is unavailable")})
		}
		return
	}
}

func sshDestination(command string) string {
	args := strings.Fields(command)
	if len(args) < 2 || filepath.Base(args[0]) != "ssh" {
		return ""
	}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if strings.ContainsAny(a, "'\"\\") {
			return ""
		}
		if a == "--" {
			if i+1 < len(args) && !strings.ContainsAny(args[i+1], "'\"\\") {
				return args[i+1]
			}
			return ""
		}
		if !strings.HasPrefix(a, "-") {
			return a
		}
		for j := 1; j < len(a); j++ {
			if strings.ContainsRune("BbcDEeFIiJLlmOoPpQRSWw", rune(a[j])) {
				if j == len(a)-1 {
					i++
				}
				break
			}
			if !strings.ContainsRune("1246AaCfGgKkMNnqsTtVvXxYy", rune(a[j])) {
				return ""
			}
		}
	}
	return ""
}

func containerDestination(command string) string {
	a := strings.Fields(command)
	if len(a) < 3 {
		return ""
	}
	name := filepath.Base(a[0])
	if (name != "docker" && name != "podman") || a[1] != "exec" {
		return ""
	}
	for i := 2; i < len(a); i++ {
		v := a[i]
		if strings.ContainsAny(v, "'\"\\") {
			return ""
		}
		if v == "--" {
			if i+1 < len(a) && !strings.ContainsAny(a[i+1], "'\"\\") {
				return a[i+1]
			}
			return ""
		}
		if !strings.HasPrefix(v, "-") {
			return v
		}
		if strings.HasPrefix(v, "--") {
			option, _, attached := strings.Cut(v, "=")
			switch option {
			case "--user", "--workdir", "--env", "--env-file", "--detach-keys", "--preserve-fds":
				if !attached {
					i++
				}
			case "--detach", "--interactive", "--tty", "--privileged":
			default:
				return ""
			}
			continue
		}
		for j := 1; j < len(v); j++ {
			if strings.ContainsRune("uwe", rune(v[j])) {
				if j == len(v)-1 {
					i++
				}
				break
			}
			if !strings.ContainsRune("dit", rune(v[j])) {
				return ""
			}
		}
	}
	return ""
}
