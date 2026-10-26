//go:build integration && linux && cgo

package pastetrail

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This test creates its own display and clipboard; it never uses the desktop's
// DISPLAY. Dependencies: Xvfb, xterm, xclip, xdotool, and xprop.
func TestX11Integration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, tool := range []string{"Xvfb", "xterm", "xclip", "xdotool", "xprop"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("integration dependency %s: %v", tool, err)
		}
	}
	start := func(cmd *exec.Cmd) {
		t.Helper()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	}
	run := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v %s", name, args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	server := exec.CommandContext(ctx, "Xvfb", "-displayfd", "3", "-screen", "0", "800x600x24", "-nolisten", "tcp", "+extension", "RECORD")
	server.ExtraFiles = []*os.File{write}
	start(server)
	_ = write.Close()
	display := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(read)
		if scanner.Scan() {
			display <- scanner.Text()
		} else {
			display <- ""
		}
	}()
	select {
	case number := <-display:
		if number == "" {
			t.Fatal("Xvfb did not allocate a display")
		}
		t.Setenv("DISPLAY", ":"+number)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	t.Setenv("XDG_SESSION_TYPE", "x11")
	t.Setenv("WAYLAND_DISPLAY", "")
	terminal := exec.CommandContext(ctx, "xterm", "-title", "PasteTrail-fixture", "-e", "cat")
	start(terminal)
	window := strings.Split(run("xdotool", "search", "--sync", "--onlyvisible", "--name", "PasteTrail-fixture"), "\n")[0]
	run("xdotool", "windowfocus", "--sync", window)
	run("xprop", "-id", window, "-f", "_NET_WM_PID", "32c", "-set", "_NET_WM_PID", strconv.Itoa(terminal.Process.Pid))
	host, _ := os.Hostname()
	run("xprop", "-id", window, "-f", "WM_CLIENT_MACHINE", "8s", "-set", "WM_CLIENT_MACHINE", host)

	monitor, err := New(Options{DisableTerminalRelay: true})
	if err != nil {
		t.Fatal(err)
	}
	records := make(chan Record, 128)
	done := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		done <- monitor.Run(ctx, func(r Record) error {
			select {
			case records <- r:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	t.Cleanup(func() { cancel(); <-stopped })
	clipboard := func(selection, target, value string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "xclip", "-selection", selection, "-target", target, "-in", "-quiet")
		cmd.Stdin = strings.NewReader(value)
		start(cmd)
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			out, err := exec.CommandContext(ctx, "xclip", "-selection", selection, "-target", target, "-out").Output()
			if err == nil && string(out) == value {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("fixture clipboard owner did not become ready")
	}
	attempt := func(args []string, match func(Event) bool) Event {
		t.Helper()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				run("xdotool", args...)
			case r := <-records:
				if r.Event != nil && match(*r.Event) {
					return *r.Event
				}
			case err := <-done:
				t.Fatalf("monitor stopped: %v", err)
			case <-deadline.C:
				t.Fatal("expected paste observation was not delivered")
			}
		}
	}
	hasRepresentation := func(media, value string) func(Event) bool {
		return func(e Event) bool {
			for _, r := range e.Content.Representations {
				if r.MediaType == media && r.Data == value {
					return true
				}
			}
			return false
		}
	}
	clipboard("clipboard", "UTF8_STRING", "PasteTrail fixture ✓")
	e := attempt([]string{"key", "ctrl+v"}, hasRepresentation("text/plain", "PasteTrail fixture ✓"))
	if e.Trigger.Action != "ctrl+v" || e.Destination.Application.PID.Value != terminal.Process.Pid || e.Destination.Application.WindowTitle.Value != "PasteTrail-fixture" {
		t.Fatalf("incorrect destination or trigger: %+v", e)
	}
	clipboard("clipboard", "text/html", "<b>fixture</b>")
	attempt([]string{"key", "ctrl+shift+v"}, hasRepresentation("text/html", "<b>fixture</b>"))
	file := filepath.Join(t.TempDir(), "reference.txt")
	if err := os.WriteFile(file, []byte("file contents must not enter events"), 0600); err != nil {
		t.Fatal(err)
	}
	clipboard("clipboard", "text/uri-list", FileReference(file).URI+"\r\n")
	attempt([]string{"key", "shift+Insert"}, func(e Event) bool {
		return len(e.Content.Files) == 1 && e.Content.Files[0].Path == file && e.Content.Files[0].Size != nil
	})
	clipboard("primary", "UTF8_STRING", "primary fixture")
	e = attempt([]string{"click", "2"}, hasRepresentation("text/plain", "primary fixture"))
	if e.Trigger.Kind != "middle_click" || e.Destination.Application.PID.Evidence != Unavailable {
		t.Fatalf("incorrect middle-click attribution: %+v", e)
	}
	if err := server.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || errors.Is(err, context.Canceled) {
			t.Fatalf("display disconnect must be reported: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("display disconnect did not stop the monitor")
	}
}
