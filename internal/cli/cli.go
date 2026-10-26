package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/zotniq/pastetrail"
	"github.com/zotniq/pastetrail/internal/enrollment"
	"github.com/zotniq/pastetrail/internal/output"
	"github.com/zotniq/pastetrail/internal/relay"
	"github.com/zotniq/pastetrail/internal/terminalipc"
)

// Monitor is the collection interface used by the CLI.
type Monitor interface {
	Capabilities(context.Context) []pastetrail.Capability
	Run(context.Context, func(pastetrail.Record) error) error
}

type Dependencies struct {
	New      func(pastetrail.Options) (Monitor, error)
	OpenLog  func(string) (io.WriteCloser, error)
	RunShell func(context.Context, []string, string, io.Writer) (int, error)
	Enroll   func(string) (io.Closer, pastetrail.Capability, error)
}

func DefaultDependencies() Dependencies {
	return Dependencies{
		New:      func(o pastetrail.Options) (Monitor, error) { return pastetrail.New(o) },
		RunShell: relay.Run,
		Enroll:   enrollment.Enable,
		OpenLog: func(name string) (io.WriteCloser, error) {
			return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		},
	}
}

// Run executes a command. daemon stays in the foreground so a service manager
// can supervise it; Ctrl+C cancels it cleanly.
func Run(ctx context.Context, args []string, out, errOut io.Writer, version string, deps Dependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: pastetrail <daemon|shell|doctor|version> [flags]")
		return 2
	}
	if args[0] == "version" {
		fmt.Fprintln(out, version)
		return 0
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(out, "usage: pastetrail daemon [--log FILE] [--socket PATH] [--max-content-bytes N] [--terminal-hooks=false] [--detailed]\n       pastetrail shell [--socket PATH] -- [COMMAND ARG...]\n       pastetrail doctor\n       pastetrail version")
		return 0
	}
	if args[0] == "shell" {
		return runShell(ctx, args[1:], errOut, deps)
	}
	if args[0] == "_prepare-shell" || args[0] == "_relay" {
		return runInternal(ctx, args, out, errOut)
	}
	if args[0] != "daemon" && args[0] != "doctor" {
		fmt.Fprintf(errOut, "unknown command: %s\n", args[0])
		return 2
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(errOut)
	logPath := flags.String("log", "", "append JSONL records to this file (default: stdout)")
	limit := flags.Int("max-content-bytes", 1<<20, "maximum bytes per clipboard or terminal content snapshot")
	socket := flags.String("socket", "", "terminal relay socket (default: per-user cache directory)")
	hooks := flags.Bool("terminal-hooks", true, "temporarily enroll new zsh/bash sessions while the daemon runs")
	detailed := flags.Bool("detailed", false, "include evidence, accessibility metadata, and terminal delivery payloads")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errOut, "unexpected positional arguments")
		return 2
	}
	m, err := deps.New(pastetrail.Options{MaxContentBytes: *limit, TerminalSocket: *socket})
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if args[0] == "doctor" {
		if err := json.NewEncoder(out).Encode(m.Capabilities(ctx)); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return 0
	}
	var file io.WriteCloser
	if *logPath != "" {
		file, err = deps.OpenLog(*logPath)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		out = file
	}
	writer := output.NewWriter(out, errOut, *detailed)
	var activation io.Closer
	if *hooks && deps.Enroll != nil && ctx.Err() == nil {
		var capability pastetrail.Capability
		activation, capability, err = deps.Enroll(*socket)
		if err != nil {
			capability = pastetrail.Capability{Feature: "terminal_activation", Status: "unavailable", Reason: err.Error()}
		}
		if writeErr := writer.Write(pastetrail.Record{SchemaVersion: 1, Type: "capabilities", Time: time.Now().UTC(), Capabilities: []pastetrail.Capability{capability}}); writeErr != nil {
			if activation != nil {
				writeErr = errors.Join(writeErr, activation.Close())
			}
			if file != nil {
				writeErr = errors.Join(writeErr, file.Close())
			}
			fmt.Fprintln(errOut, writeErr)
			return 1
		}
	}
	err = m.Run(ctx, writer.Write)
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		err = nil
	}
	if activation != nil {
		err = errors.Join(err, activation.Close())
	}
	if file != nil {
		err = errors.Join(err, file.Close())
	}
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}

func runInternal(ctx context.Context, args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(errOut)
	socket := flags.String("socket", "", "")
	argv0 := flags.String("argv0", "", "")
	state := flags.String("state", "", "")
	token := flags.String("token", "", "")
	pid := flags.Int("pid", 0, "")
	zdotdir := flags.String("zdotdir", "", "")
	startup := flags.String("startup", "", "")
	if flags.Parse(args[1:]) != nil {
		return 2
	}
	if args[0] == "_prepare-shell" {
		command, err := relay.Prepare(ctx, *pid, *state, *token, *zdotdir, *startup)
		if err != nil {
			fmt.Fprintf(errOut, "pastetrail: terminal enrollment skipped: %v\n", err)
			return 1
		}
		if _, err = io.WriteString(out, command); err != nil {
			return 1
		}
		return 0
	}
	code, err := relay.Managed(ctx, flags.Args(), *argv0, *socket, errOut)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return code
}

func runShell(ctx context.Context, args []string, errOut io.Writer, deps Dependencies) int {
	flags := flag.NewFlagSet("shell", flag.ContinueOnError)
	flags.SetOutput(errOut)
	socket := flags.String("socket", "", "terminal relay socket")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *socket == "" {
		path, err := terminalipc.DefaultPath()
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		*socket = path
	}
	if deps.RunShell == nil {
		fmt.Fprintln(errOut, "terminal relay is unavailable")
		return 1
	}
	code, err := deps.RunShell(ctx, flags.Args(), *socket, errOut)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return code
}
