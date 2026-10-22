package terminal

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type paneInfo struct {
	session, window, pane, tty, command, cwd string
	pid                                      int
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
