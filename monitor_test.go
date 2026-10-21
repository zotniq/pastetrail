package pastetrail

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeSource struct {
	run func(context.Context, func(Observation) error) error
}

func (s fakeSource) Capabilities(context.Context) []Capability {
	return []Capability{{Feature: "test", Status: "supported"}}
}
func (s fakeSource) Run(c context.Context, f func(Observation) error) error { return s.run(c, f) }

func TestOptions(t *testing.T) {
	for _, o := range []Options{{MaxContentBytes: -1}, {DedupWindow: -1}, {MaxDedupEntries: -1}} {
		if _, err := New(o); err == nil {
			t.Fatal("negative options accepted")
		}
		if _, err := NewWithSource(fakeSource{}, o); err == nil {
			t.Fatal("negative options accepted by injected source")
		}
	}
	if _, err := NewWithSource(nil, Options{}); err == nil {
		t.Fatal("nil source accepted")
	}
	m, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Capabilities(context.Background())) == 0 {
		t.Fatal("missing capabilities")
	}
	if m.options.MaxContentBytes != 1<<20 || m.options.MaxDedupEntries != 4096 || m.options.DedupWindow != 2*time.Second {
		t.Fatal(m.options)
	}
}

func TestMonitorRecords(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := fakeSource{run: func(ctx context.Context, emit func(Observation) error) error {
		for _, key := range []string{"one", "one", "two", ""} {
			if err := emit(Observation{Key: key, Event: Event{Content: Content{Evidence: Observed, Representations: []Representation{{Data: "same"}}}}}); err != nil {
				return err
			}
		}
		return emit(Observation{Capabilities: []Capability{{Feature: "test", Status: "unavailable", Reason: "permission revoked"}}})
	}}
	m, _ := NewWithSource(s, Options{})
	m.now = func() time.Time { return now }
	var records []Record
	err := m.Run(context.Background(), func(r Record) error { records = append(records, r); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 5 || records[0].Type != "capabilities" || records[4].Type != "capabilities" {
		t.Fatalf("records: %+v", records)
	}
	ids := map[string]bool{}
	for _, r := range records[1:4] {
		if r.SchemaVersion != 1 || r.Type != "paste_attempt" || r.Event.Time != now || r.Event.CapturedAt != now || r.Event.OS == "" {
			t.Fatal(r)
		}
		if len(r.Event.ID) != 32 || ids[r.Event.ID] {
			t.Fatal("invalid or repeated ID")
		}
		ids[r.Event.ID] = true
		if r.Event.Content.Representations[0].Data != "same" {
			t.Fatal("content changed")
		}
	}
}

func TestDedupEvictionAndExpiry(t *testing.T) {
	now := time.Unix(100, 0)
	s := fakeSource{run: func(ctx context.Context, emit func(Observation) error) error {
		for _, key := range []string{"a", "b", "c", "a"} {
			if err := emit(Observation{Key: key}); err != nil {
				return err
			}
			now = now.Add(time.Millisecond)
		}
		now = now.Add(time.Second)
		return emit(Observation{Key: "a"})
	}}
	m, _ := NewWithSource(s, Options{MaxDedupEntries: 2, DedupWindow: time.Second})
	m.now = func() time.Time { return now }
	n := 0
	if err := m.Run(context.Background(), func(r Record) error {
		if r.Event != nil {
			n++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("got %d events", n)
	}
}

func TestMonitorFailures(t *testing.T) {
	boom := errors.New("sink failure")
	m, _ := NewWithSource(fakeSource{run: func(ctx context.Context, emit func(Observation) error) error { return emit(Observation{}) }}, Options{})
	if err := m.Run(nil, func(Record) error { return nil }); err == nil {
		t.Fatal("nil context")
	}
	if err := m.Run(context.Background(), nil); err == nil {
		t.Fatal("nil sink")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Run(ctx, func(Record) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := m.Run(context.Background(), func(Record) error { return boom }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if err := m.Run(context.Background(), func(r Record) error {
		if r.Event != nil {
			return boom
		}
		return nil
	}); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	m.newID = func() (string, error) { return "", boom }
	if err := m.Run(context.Background(), func(Record) error { return nil }); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	if err := m.Run(ctx, func(Record) error { cancel(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestConcurrentRunAndRestart(t *testing.T) {
	started := make(chan struct{}, 2)
	m, _ := NewWithSource(fakeSource{run: func(ctx context.Context, _ func(Observation) error) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}}, Options{})
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- m.Run(ctx, func(Record) error { return nil }) }()
		<-started
		if err := m.Run(context.Background(), func(Record) error { return nil }); !errors.Is(err, ErrRunning) {
			t.Fatal(err)
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
}

func TestPreservesCaptureTime(t *testing.T) {
	at := time.Now().Add(-time.Second)
	m, _ := NewWithSource(fakeSource{run: func(ctx context.Context, emit func(Observation) error) error {
		return emit(Observation{Event: Event{Time: at, CapturedAt: at}})
	}}, Options{})
	if err := m.Run(context.Background(), func(r Record) error {
		if r.Event != nil && (!r.Event.Time.Equal(at) || !r.Event.CapturedAt.Equal(at)) {
			t.Fatal(r)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUnavailableSource(t *testing.T) {
	s := unavailableSource{reason: "test"}
	if !reflect.DeepEqual(s.Capabilities(context.Background()), []Capability{{Feature: "monitoring", Status: "unavailable", Reason: "test"}}) {
		t.Fatal("capabilities")
	}
	if !errors.Is(s.Run(context.Background(), nil), ErrUnsupported) {
		t.Fatal("missing unsupported error")
	}
}
