package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/zotniq/pastetrail"
)

// Monitor is the subset used by the command, permitting isolated CLI tests.
type Monitor interface {
	Capabilities(context.Context) []pastetrail.Capability
	Run(context.Context, func(pastetrail.Record) error) error
}

type Dependencies struct {
	New     func(pastetrail.Options) (Monitor, error)
	OpenLog func(string) (io.WriteCloser, error)
}

func DefaultDependencies() Dependencies {
	return Dependencies{
		New: func(o pastetrail.Options) (Monitor, error) { return pastetrail.New(o) },
		OpenLog: func(name string) (io.WriteCloser, error) {
			return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		},
	}
}

// Run executes a command. daemon stays in the foreground so a service manager
// can supervise it; Ctrl+C cancels it cleanly.
func Run(ctx context.Context, args []string, out, errOut io.Writer, version string, deps Dependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: pastetrail <daemon|doctor|version> [flags]")
		return 2
	}
	if args[0] == "version" {
		fmt.Fprintln(out, version)
		return 0
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(out, "usage: pastetrail daemon [--log FILE] [--max-content-bytes N]\n       pastetrail doctor\n       pastetrail version")
		return 0
	}
	if args[0] != "daemon" && args[0] != "doctor" {
		fmt.Fprintf(errOut, "unknown command: %s\n", args[0])
		return 2
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(errOut)
	logPath := flags.String("log", "", "append JSONL records to this file (default: stdout)")
	limit := flags.Int("max-content-bytes", 1<<20, "maximum total text and rich-text bytes per event")
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
	m, err := deps.New(pastetrail.Options{MaxContentBytes: *limit})
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
	encoder := json.NewEncoder(out)
	err = m.Run(ctx, func(record pastetrail.Record) error { return encoder.Encode(record) })
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		err = nil
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
