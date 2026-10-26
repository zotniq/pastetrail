//go:build cgo && (darwin || linux)

package relay

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zotniq/pastetrail/internal/enrollment"
	"github.com/zotniq/pastetrail/internal/process"
	"github.com/zotniq/pastetrail/internal/pty"
	"github.com/zotniq/pastetrail/internal/shellquote"
)

// Prepare returns a literal exec command for the shell currently sourcing its startup file.
func Prepare(ctx context.Context, pid int, state, token, zdotdir, startup string) (string, error) {
	tty := pty.CurrentName()
	if tty == "" || os.Getenv("PASTETRAIL_SESSION_TTY") == tty {
		return "", nil
	}
	socket, err := enrollment.Active(state, token)
	if err != nil {
		return "", nil
	}
	conn, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(ctx, "unix", socket)
	if err != nil {
		return "", nil
	}
	conn.Close()
	command, err := process.ReadCommand(pid)
	if err != nil {
		return "", fmt.Errorf("read shell launch arguments: %w", err)
	}
	if len(command.Args) == 0 {
		return "", fmt.Errorf("shell argument list is empty")
	}
	name := strings.TrimPrefix(filepath.Base(command.Args[0]), "-")
	if name != "zsh" && name != "bash" {
		return "", nil
	}
	for _, arg := range command.Args[1:] {
		if arg == "--command" || (strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg, "c")) {
			return "", nil
		}
	}
	binary, err := os.Executable()
	if err != nil {
		return "", err
	}
	q := shellquote.Quote
	var out strings.Builder
	if name == "zsh" {
		if integration := os.Getenv("JETBRAINS_INTELLIJ_ZSH_DIR"); integration != "" {
			candidate := filepath.Join(integration, "zdotdir")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				zdotdir = candidate
			}
		}
		if zdotdir != "" {
			fmt.Fprintf(&out, "export ZDOTDIR=%s; ", q(zdotdir))
		}
	} else {
		out.WriteString(bashRestart(command.Args, startup))
	}
	fmt.Fprintf(&out, "builtin exec %s _relay --socket %s --argv0 %s -- %s", q(binary), q(socket), q(command.Args[0]), q(command.Path))
	for _, arg := range command.Args[1:] {
		out.WriteByte(' ')
		out.WriteString(q(arg))
	}
	out.WriteByte('\n')
	return out.String(), nil
}

func bashRestart(args []string, startup string) string {
	for i, arg := range args {
		if (arg != "--rcfile" && arg != "--init-file") || i+1 >= len(args) {
			continue
		}
		switch filepath.Base(args[i+1]) {
		case "bash-integration.bash":
			// JetBrains clears its login flag before sourcing the user's profile.
			if startup != "" && filepath.Base(startup) != ".bashrc" {
				return "export LOGIN_SHELL=1; "
			}
		case "shellIntegration-bash.sh":
			return "builtin unset VSCODE_SHELL_INTEGRATION; "
		}
	}
	return ""
}
