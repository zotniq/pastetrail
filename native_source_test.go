package pastetrail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testBridge struct {
	status           string
	openErr, nextErr error
	data             chan []byte
	reads            atomic.Int64
	closed           atomic.Bool
}

func (b *testBridge) probe() string  { return b.status }
func (b *testBridge) open(int) error { return b.openErr }
func (b *testBridge) close()         { b.closed.Store(true) }
func (b *testBridge) next() ([]byte, error) {
	if b.nextErr != nil {
		return nil, b.nextErr
	}
	select {
	case d := <-b.data:
		b.reads.Add(1)
		return d, nil
	case <-time.After(time.Millisecond):
		return nil, nil
	}
}
func sourceForTest(b *testBridge) *nativeSource {
	o, _ := (Options{}).defaults()
	s := newNativeSource(b, o)
	s.enrich = func(context.Context, *Event) {}
	return s
}
func snapshot(key string) []byte {
	return []byte(`{"key":"` + key + `","time_ns":1000000000,"action":"ctrl+v"}`)
}

func TestNativeCapabilities(t *testing.T) {
	for _, status := range []string{"supported|test", "permission_required|missing", "broken"} {
		s := sourceForTest(&testBridge{status: status})
		caps := s.Capabilities(context.Background())
		if len(caps) < 8 || caps[0].Reason == "" {
			t.Fatal(caps)
		}
		if status == "supported|test" && caps[0].Status != "partial" {
			t.Fatal(caps)
		}
		if status == "broken" && caps[0].Status != "unavailable" {
			t.Fatal(caps)
		}
	}
}

func TestDecodeSnapshot(t *testing.T) {
	file := t.TempDir() + "/example.txt"
	if err := os.WriteFile(file, []byte("never read"), 0600); err != nil {
		t.Fatal(err)
	}
	raw := map[string]any{"key": "1", "time_ns": 1000000000, "captured_ns": 2000000000, "kind": "keyboard", "action": "ctrl+v", "automated": true, "source": "system_clipboard", "pid": 42, "app": "Chrome", "executable": "/bin/chrome", "window": "10", "title": "Example", "document": "https://example.com/", "files": []string{file}, "issue": "clipboard changed", "formats": []map[string]any{
		{"type": "text/plain", "data": base64.StdEncoding.EncodeToString([]byte("hello")), "size": 8, "truncated": true},
		{"type": "image/png", "data": base64.StdEncoding.EncodeToString([]byte{0, 1})},
	}}
	b, _ := json.Marshal(raw)
	o, err := decodeSnapshot(b, 4)
	if err != nil {
		t.Fatal(err)
	}
	e := o.Event
	if e.Trigger.Automated.Value != true || e.Destination.Application.PID.Value != 42 || e.Destination.Browser.URL.Value != "https://example.com/" {
		t.Fatal(e)
	}
	if len(e.Content.Representations) != 1 || e.Content.Representations[0].Data != "hell" || e.Content.Representations[0].Size != 8 || !e.Content.Incomplete || len(e.Content.Files) != 1 {
		t.Fatal(e.Content)
	}
	if e.CapturedAt.Unix() != 2 || e.Time.Unix() != 1 {
		t.Fatal(e)
	}
	o, err = decodeSnapshot(snapshot("2"), 100)
	if err != nil || o.Event.Content.Evidence != Unavailable || o.Event.Content.Reason == "" {
		t.Fatalf("%+v %v", o, err)
	}
	for _, b := range []string{"invalid", `{}`, `{"key":"a","time_ns":0}`} {
		if _, err := decodeSnapshot([]byte(b), 100); err == nil {
			t.Fatal("invalid accepted", b)
		}
	}
	for _, document := range []string{"", "file:///example"} {
		b, _ := json.Marshal(map[string]any{"key": "x", "time_ns": 1, "app": "Safari", "document": document, "issue": "unavailable"})
		o, err := decodeSnapshot(b, 100)
		if err != nil || o.Event.Destination.Browser.URL.Evidence != "" || o.Event.Content.Reason != "unavailable" {
			t.Fatalf("%+v %v", o, err)
		}
	}
	if browserName("Terminal") {
		t.Fatal("terminal identified as browser")
	}
}

func TestNativePumpErrors(t *testing.T) {
	boom := errors.New("failure")
	for _, b := range []*testBridge{{openErr: boom}, {nextErr: boom}} {
		s := sourceForTest(b)
		if err := s.Run(context.Background(), func(Observation) error { return nil }); !errors.Is(err, boom) {
			t.Fatal(err)
		}
		if b.openErr == nil && !b.closed.Load() {
			t.Fatal("native driver not closed before Run returned")
		}
	}
	for _, data := range [][]byte{snapshot("1"), []byte("bad")} {
		b := &testBridge{data: make(chan []byte, 1)}
		b.data <- data
		s := sourceForTest(b)
		err := s.Run(context.Background(), func(Observation) error { return boom })
		if err == nil || (!strings.Contains(err.Error(), "decode") && !errors.Is(err, boom)) {
			t.Fatal(err)
		}
		if !b.closed.Load() {
			t.Fatal("native resources leaked")
		}
	}
}

func TestNativePumpCancellationAndSingleton(t *testing.T) {
	b := &testBridge{data: make(chan []byte, 1)}
	b.data <- snapshot("1")
	s := sourceForTest(b)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.Run(ctx, func(o Observation) error {
		if err := s.Run(ctx, func(Observation) error { return nil }); !errors.Is(err, ErrRunning) {
			t.Fatal(err)
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || !b.closed.Load() {
		t.Fatalf("%v closed=%v", err, b.closed.Load())
	}
}

func TestNativeOverflow(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "report", true: "sink failure"}[reject], func(t *testing.T) {
			b := &testBridge{data: make(chan []byte, 8)}
			s := sourceForTest(b)
			s.queueSize = 1
			b.data <- snapshot("1")
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			gotGap := false
			boom := errors.New("gap sink")
			err := s.Run(ctx, func(o Observation) error {
				if o.Capabilities != nil {
					gotGap = true
					if reject {
						return boom
					}
					cancel()
					return nil
				}
				if o.Key == "1" {
					for i := 0; i < 5; i++ {
						b.data <- snapshot("next")
					}
					deadline := time.Now().Add(500 * time.Millisecond)
					for b.reads.Load() < 6 && time.Now().Before(deadline) {
						runtime.Gosched()
					}
				}
				return nil
			})
			if !gotGap {
				t.Fatal("overflow not reported", err)
			}
			if reject && !errors.Is(err, boom) {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeReportedOverflow(t *testing.T) {
	for _, fail := range []bool{false, true} {
		b := &testBridge{data: make(chan []byte, 1)}
		b.data <- []byte(`{"key":"a","time_ns":1,"dropped":3}`)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		reported := false
		boom := errors.New("sink")
		err := sourceForTest(b).Run(ctx, func(o Observation) error {
			if o.Capabilities != nil {
				reported = true
				if fail {
					return boom
				}
			} else {
				cancel()
			}
			return nil
		})
		cancel()
		if !reported || (fail && !errors.Is(err, boom)) {
			t.Fatalf("%v %v", reported, err)
		}
	}
}

func TestNativeStoppedCapabilities(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &testBridge{}
	if err := sourceForTest(b).Run(ctx, func(Observation) error { t.Fatal("unexpected emit"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	backendErr, sinkErr := errors.New("backend"), errors.New("sink")
	err := sourceForTest(&testBridge{openErr: backendErr}).Run(context.Background(), func(o Observation) error {
		if len(o.Capabilities) != 1 || o.Capabilities[0].Feature != "monitoring" || o.Capabilities[0].Status != "unavailable" {
			t.Fatal(o)
		}
		return sinkErr
	})
	if !errors.Is(err, backendErr) || !errors.Is(err, sinkErr) {
		t.Fatal(err)
	}
}

func TestMacDestinationAttribution(t *testing.T) {
	for _, method := range []string{"event_target_pid", "accessibility_focus", "unavailable"} {
		t.Run(method, func(t *testing.T) {
			b := &testBridge{data: make(chan []byte, 4)}
			apps := []string{"Google Chrome", "Codex", "Terminal", "Google Chrome"}
			for i, app := range apps {
				data, err := json.Marshal(map[string]any{
					"key": app + string(rune('0'+i)), "time_ns": i + 1, "pid": 100 + i,
					"app": app, "executable": "/Applications/" + app, "window": "fixture", "title": app,
					"target_method": method,
				})
				if err != nil {
					t.Fatal(err)
				}
				b.data <- data
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			m, err := NewWithSource(sourceForTest(b), Options{})
			if err != nil {
				t.Fatal(err)
			}
			seen := 0
			err = m.Run(ctx, func(r Record) error {
				if r.Event == nil {
					return nil
				}
				e := r.Event
				a := e.Destination.Application
				if method == "unavailable" {
					if a.PID.Evidence != Unavailable || a.Name.Value != "" || a.PID.Value != 0 || a.PID.Reason == "" || e.Destination.Browser != nil {
						t.Fatalf("unavailable destination retained stale context: %+v", e.Destination)
					}
				} else {
					want := Observed
					if method == "accessibility_focus" {
						want = Inferred
					}
					if a.PID.Value != 100+seen || a.Name.Value != apps[seen] || a.PID.Evidence != want || a.Name.Evidence != want || a.PID.Reason == "" {
						t.Fatalf("wrong recipient after app switch: %+v", a)
					}
					if (e.Destination.Browser != nil) != (apps[seen] == "Google Chrome") {
						t.Fatalf("browser context leaked across app switch: %+v", e.Destination)
					}
				}
				seen++
				if seen == len(apps) {
					cancel()
				}
				return nil
			})
			if !errors.Is(err, context.Canceled) || seen != len(apps) {
				t.Fatalf("received %d events: %v", seen, err)
			}
		})
	}
}
