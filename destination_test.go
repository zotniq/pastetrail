package pastetrail

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestProcessParsing(t *testing.T) {
	p := parseProcesses("10 1 10 20 ttys000 /bin/zsh\n20 10 20 20 ttys000 /path with spaces/vim\ninvalid\nx 1 1 1 ?? bad\n0 0 0 0 ?? zero")
	if len(p) != 2 || p[1].name != "/path with spaces/vim" || p[1].foreground != 20 {
		t.Fatal(p)
	}
	if !terminalName("iTerm2") || terminalName("Safari") {
		t.Fatal("classification")
	}
}

func inspector(ps []process) processInspector {
	return processInspector{list: func(context.Context) ([]process, error) { return ps, nil }, command: func(_ context.Context, pid int) (string, error) {
		return map[int]string{20: "vim notes.txt", 30: "ssh -p 22 user@host", 40: "docker exec -it container bash", 50: "tmux"}[pid], nil
	}, cwd: func(int) (string, error) { return "/work", nil }, host: func() (string, error) { return "local", nil }, tmux: func(context.Context, int) (paneInfo, error) {
		return paneInfo{session: "$1", window: "@2", pane: "%3", tty: "pts/2", command: "vim", cwd: "/work", pid: 60}, nil
	}}
}
func terminalEvent() Event {
	return Event{Destination: Destination{Application: Application{Name: Known("Terminal"), PID: Known(1)}}}
}

func TestTerminalAttribution(t *testing.T) {
	for _, tt := range []struct {
		pid        int
		name, kind string
	}{{20, "/usr/bin/vim", "command"}, {30, "ssh", "ssh"}, {40, "docker", "container"}} {
		ps := []process{{pid: 10, parent: 1, group: 10, foreground: tt.pid, tty: "ttys000", name: "-zsh"}, {pid: tt.pid, parent: 10, group: tt.pid, foreground: tt.pid, tty: "ttys000", name: tt.name}}
		e := terminalEvent()
		enrichTerminal(context.Background(), &e, inspector(ps))
		c := e.Destination.Contexts
		if len(c) < 3 || c[1].Kind != "shell" || c[2].PID.Value != tt.pid || c[2].WorkingDirectory.Value != "/work" || c[len(c)-1].Kind != tt.kind {
			t.Fatalf("%+v", c)
		}
		if tt.kind == "ssh" && c[len(c)-1].Host.Value != "user@host" {
			t.Fatal(c)
		}
		if tt.kind == "container" && c[len(c)-1].Container.Value != "container" {
			t.Fatal(c)
		}
	}
}

func TestTerminalAmbiguity(t *testing.T) {
	for _, ps := range [][]process{
		nil,
		{{pid: 10, parent: 1, tty: "a"}, {pid: 11, parent: 1, tty: "b"}},
		{{pid: 10, parent: 1, group: 10, foreground: 20, tty: "a"}, {pid: 20, parent: 10, group: 20, foreground: 20, tty: "a"}, {pid: 21, parent: 10, group: 20, foreground: 20, tty: "a"}},
		{{pid: 10, parent: 11, tty: "a"}, {pid: 11, parent: 10, tty: "a"}, {pid: 12, parent: 1, tty: "??"}},
	} {
		e := terminalEvent()
		enrichTerminal(context.Background(), &e, inspector(ps))
		last := e.Destination.Contexts[len(e.Destination.Contexts)-1]
		if last.Command.Evidence != Unavailable && last.Name.Evidence != Unavailable {
			t.Fatal(e.Destination)
		}
	}
	boom := errors.New("denied")
	in := inspector(nil)
	in.list = func(context.Context) ([]process, error) { return nil, boom }
	in.host = func() (string, error) { return "", boom }
	e := terminalEvent()
	enrichTerminal(context.Background(), &e, in)
	if e.Destination.Contexts[0].Command.Reason != "denied" {
		t.Fatal(e)
	}
}

func TestTmuxContext(t *testing.T) {
	ps := []process{{pid: 10, parent: 1, group: 10, foreground: 50, tty: "a", name: "zsh"}, {pid: 50, parent: 10, group: 50, foreground: 50, tty: "a", name: "tmux: client"}, {pid: 60, parent: 70, group: 60, foreground: 20, tty: "pts/2", name: "bash"}, {pid: 20, parent: 60, group: 20, foreground: 20, tty: "pts/2", name: "vim"}}
	e := terminalEvent()
	enrichTerminal(context.Background(), &e, inspector(ps))
	c := e.Destination.Contexts
	if len(c) != 6 || c[3].Kind != "tmux" || c[3].Pane.Value != "%3" || c[5].Name.Value != "vim" {
		t.Fatalf("%+v", c)
	}
	in := inspector(ps)
	in.tmux = func(context.Context, int) (paneInfo, error) { return paneInfo{}, errors.New("socket missing") }
	in.command = func(context.Context, int) (string, error) { return "", errors.New("gone") }
	in.cwd = func(int) (string, error) { return "", errors.New("gone") }
	e = terminalEvent()
	enrichTerminal(context.Background(), &e, in)
	if c := e.Destination.Contexts; c[len(c)-1].Pane.Reason != "socket missing" {
		t.Fatal(c)
	}
}

func TestTmuxParsing(t *testing.T) {
	output := "0\t%1\t22\t/dev/pts/0\tbash\t/work\t@1\n1\t%2\t23\t/dev/pts/1\tvim\t/a path\t@1"
	p, err := parsePane("$1", output)
	if err != nil || p.pane != "%2" || p.cwd != "/a path" || p.tty != "pts/1" {
		t.Fatalf("%+v %v", p, err)
	}
	for _, s := range []string{"bad", "1\t%1\tbad\t/dev/a\tsh\t/a\t@1", output + "\n" + output} {
		if _, err := parsePane("$1", s); err == nil {
			t.Fatal("invalid pane accepted")
		}
	}
	for _, mode := range []string{"ok", "client error", "pane error", "missing"} {
		calls := 0
		run := func(_ context.Context, name string, args ...string) (string, error) {
			calls++
			if name != "tmux" {
				t.Fatal(name)
			}
			if mode == "client error" || (mode == "pane error" && calls == 2) {
				return "", errors.New("error")
			}
			if calls == 1 {
				if mode == "missing" {
					return "other", nil
				}
				return "99\t$2\n50\t$1", nil
			}
			return output, nil
		}
		p, err := readTmux(context.Background(), 50, run)
		if mode == "ok" {
			if err != nil || p.session != "$1" {
				t.Fatalf("%+v %v", p, err)
			}
		} else if err == nil {
			t.Fatal(mode)
		}
	}
}

func TestCommandDestinations(t *testing.T) {
	for command, want := range map[string]string{"ssh host": "host", "ssh -p 22 -i key -vv user@host": "user@host", "ssh -- host": "host", "ssh --": "", "ssh -p": "", "bash": "", "ssh 'bad host'": "", "/usr/bin/ssh -o ProxyJump=jump target": "target"} {
		if got := sshDestination(command); got != want {
			t.Errorf("%q: %q != %q", command, got, want)
		}
	}
	for command, want := range map[string]string{"docker exec -it box sh": "box", "podman exec -u root -w /app box vim": "box", "docker exec --env=A=B box sh": "box", "docker ps all": "", "docker exec -it": "", "docker exec 'odd box' sh": "", "ssh host": "", "docker exec -e KEY=value box sh": "box"} {
		if got := containerDestination(command); got != want {
			t.Errorf("%q: %q != %q", command, got, want)
		}
	}
}

func TestSystemInspectorErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := systemProcesses(ctx); err == nil {
		t.Fatal("cancelled process command succeeded")
	}
	if _, err := inspectTmux(ctx, 123); err == nil {
		t.Fatal("cancelled tmux command succeeded")
	}
	if _, err := commandOutput(ctx, "pastetrail-command-that-does-not-exist"); err == nil {
		t.Fatal("missing command succeeded")
	}
	if _, err := processCWD(-1); err == nil {
		t.Fatal("invalid process accepted")
	}
	if runtime.GOOS != "windows" {
		if cwd, err := processCWD(os.Getpid()); err == nil && !strings.HasPrefix(cwd, "/") {
			t.Fatal(cwd)
		}
	}
	e := Event{}
	enrichDestination(ctx, &e)
	if len(e.Destination.Contexts) != 0 {
		t.Fatal(e)
	}
	e = terminalEvent()
	enrichDestination(ctx, &e)
	if len(e.Destination.Contexts) == 0 {
		t.Fatal("missing terminal limitation")
	}
}

func TestAmbiguousCommandOptions(t *testing.T) {
	for command, want := range map[string]string{
		"ssh -vvp 22 host": "host", "ssh -vvp22 host": "host", "ssh -P tag host": "host",
		"ssh --unknown value host": "", "ssh -- 'quoted host'": "", "ssh -Z value host": "",
		"ssh -t -A host": "host", "ssh -Jjump host": "host",
	} {
		if got := sshDestination(command); got != want {
			t.Errorf("%q: %q != %q", command, got, want)
		}
	}
	for command, want := range map[string]string{
		"podman exec --preserve-fds 3 box sh": "box", "docker exec --privileged box sh": "box",
		"docker exec --unknown value box sh": "", "docker exec -Z box sh": "",
		"docker exec -ituroot box sh": "box", "docker exec -- box sh": "box",
		"docker exec --": "", "docker exec -- 'quoted box' sh": "", "podman exec --latest sh": "",
	} {
		if got := containerDestination(command); got != want {
			t.Errorf("%q: %q != %q", command, got, want)
		}
	}
}
