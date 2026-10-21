package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zotniq/pastetrail"
)

type mockMonitor struct{ err error }

func (mockMonitor) Capabilities(context.Context) []pastetrail.Capability {
	return []pastetrail.Capability{{Feature: "test", Status: "supported"}}
}
func (m mockMonitor) Run(_ context.Context, f func(pastetrail.Record) error) error {
	if err := f(pastetrail.Record{SchemaVersion: 1, Type: "capabilities"}); err != nil {
		return err
	}
	return m.err
}

type badWriter struct{}

func (badWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type logFile struct {
	bytes.Buffer
	closed bool
	err    error
}

func (f *logFile) Close() error { f.closed = true; return f.err }

func TestCommands(t *testing.T) {
	for _, tt := range []struct {
		args     []string
		code     int
		contains string
	}{
		{nil, 2, "usage:"}, {[]string{"version"}, 0, "v0.1.0"}, {[]string{"help"}, 0, "usage:"},
		{[]string{"wrong"}, 2, "unknown command"}, {[]string{"doctor"}, 0, "supported"},
		{[]string{"daemon"}, 0, "capabilities"}, {[]string{"daemon", "-h"}, 0, "Usage"},
		{[]string{"daemon", "--wrong"}, 2, "flag provided"}, {[]string{"daemon", "extra"}, 2, "unexpected"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			var out bytes.Buffer
			deps := Dependencies{New: func(pastetrail.Options) (Monitor, error) { return mockMonitor{}, nil }}
			if got := Run(context.Background(), tt.args, &out, &out, "v0.1.0", deps); got != tt.code {
				t.Fatalf("code %d: %s", got, &out)
			}
			if !strings.Contains(out.String(), tt.contains) {
				t.Fatal(out.String())
			}
		})
	}
}

func TestFailures(t *testing.T) {
	boom := errors.New("failure")
	for _, command := range []string{"doctor", "daemon"} {
		var out bytes.Buffer
		deps := Dependencies{New: func(pastetrail.Options) (Monitor, error) { return nil, boom }}
		if Run(context.Background(), []string{command}, &out, &out, "dev", deps) != 1 {
			t.Fatal("new error lost")
		}
		deps.New = func(pastetrail.Options) (Monitor, error) { return mockMonitor{}, nil }
		if Run(context.Background(), []string{command}, badWriter{}, &out, "dev", deps) != 1 {
			t.Fatal("write error lost")
		}
	}
	var out bytes.Buffer
	deps := Dependencies{New: func(pastetrail.Options) (Monitor, error) { return mockMonitor{err: boom}, nil }}
	if Run(context.Background(), []string{"daemon"}, &out, &out, "dev", deps) != 1 {
		t.Fatal("source error lost")
	}
	deps.OpenLog = func(string) (io.WriteCloser, error) { return nil, boom }
	if Run(context.Background(), []string{"daemon", "--log", "events"}, &out, &out, "dev", deps) != 1 {
		t.Fatal("open error lost")
	}
}

func TestLogAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, closeError := range []error{nil, io.ErrClosedPipe} {
		var out bytes.Buffer
		file := &logFile{err: closeError}
		deps := Dependencies{New: func(pastetrail.Options) (Monitor, error) { return mockMonitor{err: context.Canceled}, nil }, OpenLog: func(string) (io.WriteCloser, error) { return file, nil }}
		code := Run(ctx, []string{"daemon", "--log", "events"}, &out, &out, "dev", deps)
		if !file.closed || file.Len() == 0 {
			t.Fatal("log not written and closed")
		}
		if (closeError == nil && code != 0) || (closeError != nil && code != 1) {
			t.Fatalf("code=%d close=%v", code, closeError)
		}
	}
}

func TestDefaults(t *testing.T) {
	d := DefaultDependencies()
	if _, err := d.New(pastetrail.Options{}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := d.OpenLog(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("test")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "test" {
		t.Fatalf("%q %v", b, err)
	}
}
