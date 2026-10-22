package terminal

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/zotniq/pastetrail/internal/model"
)

type process struct {
	pid, parent, group, foreground int
	tty, name                      string
}
type processInspector struct {
	activeTTY func(context.Context, model.Application) (string, error)
	list      func(context.Context) ([]process, error)
	command   func(context.Context, int) (string, error)
	cwd       func(int) (string, error)
	tmux      func(context.Context, int) (paneInfo, error)
	host      func() (string, error)
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
