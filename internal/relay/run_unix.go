//go:build cgo && (darwin || linux)

package relay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/zotniq/pastetrail/internal/process"
	"github.com/zotniq/pastetrail/internal/pty"
	"github.com/zotniq/pastetrail/internal/terminal"
	"github.com/zotniq/pastetrail/internal/terminalipc"
)

func Run(ctx context.Context, args []string, socket string, errOut io.Writer) (int, error) {
	return launch(ctx, args, socket, errOut, "")
}

func Managed(ctx context.Context, args []string, argv0, socket string, errOut io.Writer) (int, error) {
	return launch(ctx, args, socket, errOut, argv0)
}

func launch(ctx context.Context, args []string, socket string, errOut io.Writer, argv0 string) (int, error) {
	if len(args) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		args = []string{shell, "-l"}
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		return 1, err
	}
	if own, err := os.Executable(); err == nil {
		a, e1 := os.Stat(own)
		b, e2 := os.Stat(path)
		if e1 == nil && e2 == nil && os.SameFile(a, b) {
			return 1, errors.New("relay command must be a shell or CLI, not pastetrail itself")
		}
	}
	argv := append([]string(nil), args...)
	if argv0 != "" {
		argv[0] = argv0
	}
	fallback := func(err error) (int, error) {
		if ctx.Err() != nil {
			return 1, ctx.Err()
		}
		env := shellEnvironment(pty.CurrentName(), "")
		if execErr := syscall.Exec(path, argv, env); execErr != nil {
			err = errors.Join(err, execErr)
		}
		return 1, err
	}
	client, err := terminalipc.Connect(ctx, socket)
	if err != nil {
		return fallback(err)
	}
	defer client.Close()
	tty, err := pty.Open()
	if err != nil {
		return fallback(err)
	}
	defer tty.Close()
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return fallback(err)
	}
	host, _ := os.Hostname()
	session := terminal.Session{ID: hex.EncodeToString(id[:]), TTY: tty.Name, RelayPID: os.Getpid(), Host: host, StartedAt: time.Now().UTC()}
	session.Ancestors, session.Application = process.Ancestors(ctx, os.Getpid())
	cmd := exec.Command(path, args[1:]...)
	cmd.Args = argv
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty.Slave, tty.Slave, tty.Slave
	cmd.Env = shellEnvironment(tty.Name, session.ID)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := ctx.Err(); err != nil {
		return 1, err
	}
	if err := cmd.Start(); err != nil {
		return 1, err
	}
	tty.Slave.Close()
	if err := tty.Raw(); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return 1, err
	}
	session.ShellPID = cmd.Process.Pid
	client.Start(session, func(err error) { fmt.Fprintf(errOut, "pastetrail: %v\r\n", err) })
	return run(ctx, cmd, tty, client)
}

func shellEnvironment(tty, sessionID string) []string {
	env := os.Environ()
	kept := env[:0]
	for _, entry := range env {
		if !strings.HasPrefix(entry, "PASTETRAIL_SESSION_TTY=") && !strings.HasPrefix(entry, "PASTETRAIL_SESSION_ID=") {
			kept = append(kept, entry)
		}
	}
	kept = append(kept, "PASTETRAIL_SESSION_TTY="+tty)
	if sessionID != "" {
		kept = append(kept, "PASTETRAIL_SESSION_ID="+sessionID)
	}
	return kept
}

func run(ctx context.Context, cmd *exec.Cmd, tty *pty.Terminal, publisher terminal.Publisher) (int, error) {
	inputDone, outputDone, waitDone := make(chan error, 1), make(chan error, 1), make(chan error, 1)
	go func() { inputDone <- forwardInput(tty, publisher) }()
	go func() { _, err := io.Copy(tty.Outer, tty.Master); outputDone <- err }()
	go func() { waitDone <- cmd.Wait() }()
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer signal.Stop(resize)
	var cause, waitErr error
	var outputFinished, inputFinished bool
	waited := false
	for !waited && cause == nil {
		select {
		case <-ctx.Done():
			cause = ctx.Err()
		case <-resize:
			tty.Resize()
		case err := <-inputDone:
			inputFinished = true
			cause = err
			if cause == nil {
				cause = io.EOF
			}
		case err := <-outputDone:
			outputFinished = true
			if err != nil && !errors.Is(err, syscall.EIO) {
				cause = err
			}
		case waitErr = <-waitDone:
			waited = true
		}
	}
	if !waited {
		foreground := tty.Foreground()
		if foreground > 0 {
			_ = syscall.Kill(-foreground, syscall.SIGHUP)
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
		tty.Master.Close()
		select {
		case waitErr = <-waitDone:
		case <-time.After(500 * time.Millisecond):
			if foreground > 0 {
				_ = syscall.Kill(-foreground, syscall.SIGKILL)
			}
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			waitErr = <-waitDone
		}
	}
	if !outputFinished {
		select {
		case <-outputDone:
			outputFinished = true
		case <-time.After(200 * time.Millisecond):
		}
	}
	tty.Restore()
	tty.Outer.Close()
	tty.Master.Close()
	if !inputFinished {
		<-inputDone
	}
	if !outputFinished {
		<-outputDone
	}
	if cause != nil && !errors.Is(cause, io.EOF) && !errors.Is(cause, context.Canceled) {
		return 1, cause
	}
	if exit := new(exec.ExitError); errors.As(waitErr, &exit) {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal()), nil
		}
		return exit.ExitCode(), nil
	}
	if waitErr != nil {
		return 1, waitErr
	}
	return 0, nil
}

func forwardInput(tty *pty.Terminal, publisher terminal.Publisher) error {
	buffer := make([]byte, terminal.MaxInputBytes)
	for {
		n, err := tty.Outer.Read(buffer)
		if n > 0 {
			at := time.Now().UTC()
			for offset := 0; offset < n; {
				group := tty.Foreground()
				written, writeErr := tty.Master.Write(buffer[offset:n])
				if group != tty.Foreground() {
					group = 0
				}
				if written > 0 {
					publisher.Input(at, group, buffer[offset:offset+written])
					offset += written
				}
				if writeErr != nil {
					return writeErr
				}
				if written == 0 {
					return io.ErrShortWrite
				}
			}
		}
		if err != nil {
			return err
		}
	}
}
