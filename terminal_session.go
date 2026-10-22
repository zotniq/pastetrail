package pastetrail

import (
	"context"
	"fmt"
	"strings"

	"github.com/zotniq/pastetrail/internal/platform"
)

func activeTerminalTTY(ctx context.Context, app Application) (string, error) {
	return queryTerminalTTY(ctx, app, platform.TerminalTTY)
}

func queryTerminalTTY(ctx context.Context, app Application, query func(int, bool) (string, int)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	name := strings.ToLower(app.Name.Value)
	iterm := name == "iterm2" || name == "iterm"
	if !iterm && name != "terminal" {
		return "", fmt.Errorf("this terminal does not expose a supported active-session query")
	}
	if app.PID.Value <= 0 {
		return "", fmt.Errorf("terminal PID is unavailable")
	}
	tty, status := query(app.PID.Value, iterm)
	if status != 0 {
		if status == -1743 || status == -1744 {
			return "", fmt.Errorf("macOS Automation access to %s is required for its active session; authorize a read-only session query from the terminal running PasteTrail", app.Name.Value)
		}
		return "", fmt.Errorf("active terminal session query failed (status %d)", status)
	}
	// Never open/read a TTY: it would consume input intended for the application.
	if !strings.HasPrefix(tty, "/dev/") || len(tty) <= 5 || strings.Contains(tty, "..") || strings.ContainsAny(tty, "\x00\r\n\t ") {
		return "", fmt.Errorf("terminal returned an invalid or unavailable TTY")
	}
	return strings.TrimPrefix(tty, "/dev/"), nil
}
