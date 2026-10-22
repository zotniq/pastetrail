package pastetrail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

type nativeBridge interface {
	probe() string
	open(int) error
	next() ([]byte, error)
	close()
}

type nativeSource struct {
	bridge    nativeBridge
	options   Options
	enrich    func(context.Context, *Event)
	queueSize int
}

var nativeRunning atomic.Bool

func newNativeSource(b nativeBridge, o Options) *nativeSource {
	// Budget encoded snapshots as well as their decoded contents.
	queueSize := max(1, min(64, (32<<20)/max(1, o.MaxContentBytes*2)))
	return &nativeSource{bridge: b, options: o, queueSize: queueSize, enrich: enrichDestination}
}

func (s *nativeSource) Capabilities(context.Context) []Capability {
	status, reason, ok := strings.Cut(s.bridge.probe(), "|")
	if !ok {
		reason = "native backend returned an invalid capability response"
		status = "unavailable"
	}
	if status == "supported" {
		status = "partial"
	}
	return []Capability{
		{Feature: "keyboard_paste_attempts", Status: status, Reason: reason},
		{Feature: "menu_context_menu_programmatic_and_app_internal_pastes", Status: "unavailable", Reason: "applications do not expose a universal external paste-attempt notification"},
		{Feature: "middle_click", Status: middleClickStatus(status), Reason: "X11 PRIMARY selection; macOS iTerm2 saved clipboard/selection middle-button bindings only; iTerm selection content and clicked pane identity unavailable"},
		{Feature: "clipboard_content", Status: status, Reason: "text, HTML, RTF, and file references when exposed; snapshots can race clipboard changes"},
		{Feature: "application_context", Status: status, Reason: "macOS event target PID or inferred Accessibility focus; foreground context on other platforms; available metadata depends on permissions and applications"},
		{Feature: "browser_context", Status: "partial", Reason: "macOS Safari/Chromium active-tab URL via Automation with Accessibility document fallback; window title elsewhere; tab IDs, renderer PIDs and frames unavailable"},
		{Feature: "terminal_context", Status: "partial", Reason: "macOS iTerm2/Terminal active TTY via Automation; local foreground TTY processes and tmux when unambiguous; Windows console mapping and mouse-selected panes unavailable"},
		{Feature: "ssh_remote_context", Status: "partial", Reason: "local SSH command and destination only; remote foreground process, PID, and working directory unavailable"},
		{Feature: "container_context", Status: "partial", Reason: "container identity inferred from recognized local commands; interior process information unavailable"},
		{Feature: "attempt_confirmation", Status: "unavailable", Reason: "shortcut observation indicates possible intent; insertion, clipboard format consumption, and application remapping are not observable"},
	}
}

func middleClickStatus(status string) string {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		return status
	}
	return "unavailable"
}

type nativeSnapshot struct {
	Key           string        `json:"key"`
	TimeNS        int64         `json:"time_ns"`
	CapturedNS    int64         `json:"captured_ns"`
	Action        string        `json:"action"`
	Kind          string        `json:"kind"`
	Automated     *bool         `json:"automated"`
	Source        string        `json:"source"`
	TriggerReason string        `json:"trigger_reason"`
	Button        int           `json:"button"`
	Clicks        int           `json:"clicks"`
	Modifiers     []string      `json:"modifiers"`
	PID           int           `json:"pid"`
	App           string        `json:"app"`
	Executable    string        `json:"executable"`
	Window        string        `json:"window"`
	Title         string        `json:"title"`
	Document      string        `json:"document"`
	TargetMethod  string        `json:"target_method"`
	BundleID      string        `json:"bundle_id"`
	BundlePath    string        `json:"bundle_path"`
	Bounds        *WindowBounds `json:"bounds"`
	ContentBounds *WindowBounds `json:"content_bounds"`
	MemoryUsage   *uint64       `json:"memory_usage"`
	WindowMethod  string        `json:"window_method"`
	Formats       []struct {
		Type      string `json:"type"`
		Data      []byte `json:"data"`
		Size      int    `json:"size"`
		Truncated bool   `json:"truncated"`
	} `json:"formats"`
	Files   []string `json:"files"`
	Issue   string   `json:"issue"`
	Dropped uint64   `json:"dropped"`
}

type decodedObservation struct {
	Observation
	dropped uint64
}

func decodeSnapshot(data []byte, limit int) (decodedObservation, error) {
	var n nativeSnapshot
	if err := json.Unmarshal(data, &n); err != nil {
		return decodedObservation{}, fmt.Errorf("decode native event: %w", err)
	}
	if n.Key == "" || n.TimeNS <= 0 {
		return decodedObservation{}, errors.New("native event is missing identity or capture time")
	}
	e := Event{Time: time.Unix(0, n.TimeNS), CapturedAt: time.Unix(0, n.CapturedNS), Trigger: Trigger{Kind: n.Kind, Action: n.Action, Evidence: Inferred, Source: n.Source}, Content: Content{Evidence: Observed}}
	e.Trigger.Reason, e.Trigger.Button, e.Trigger.ClickCount, e.Trigger.Modifiers = n.TriggerReason, n.Button, n.Clicks, n.Modifiers
	if n.CapturedNS <= 0 {
		e.CapturedAt = e.Time
	}
	if n.Automated != nil {
		e.Trigger.Automated = Known(*n.Automated)
	}
	if n.Issue != "" {
		e.Content.Incomplete = true
		e.Content.Reason = n.Issue
	}
	for _, r := range n.Formats {
		media := richMediaType(r.Type)
		if media == "" {
			continue
		}
		rep := textRepresentation(media, r.Data, limit)
		if r.Size > rep.Size {
			rep.Size = r.Size
		}
		rep.Truncated = rep.Truncated || r.Truncated
		e.Content.Representations = append(e.Content.Representations, rep)
	}
	for _, f := range n.Files {
		e.Content.Files = append(e.Content.Files, FileReference(f))
	}
	if len(e.Content.Files) == 0 && len(e.Content.Representations) == 0 {
		e.Content.Evidence = Unavailable
		if e.Content.Reason == "" {
			e.Content.Reason = "no supported clipboard representation was available"
		}
	}
	a := &e.Destination.Application
	if n.PID > 0 {
		a.PID = Known(n.PID)
	}
	if n.App != "" {
		a.Name = Known(n.App)
	}
	if n.Executable != "" {
		a.Executable = Known(n.Executable)
	}
	if n.Window != "" {
		a.WindowID = Known(n.Window)
	}
	if n.Title != "" {
		a.WindowTitle = Known(n.Title)
	}
	if n.BundleID != "" {
		a.BundleID = Known(n.BundleID)
	}
	if n.BundlePath != "" {
		a.BundlePath = Known(n.BundlePath)
	}
	if n.Bounds != nil {
		a.Bounds = Known(n.Bounds)
	}
	if n.ContentBounds != nil {
		a.ContentBounds = Known(n.ContentBounds)
	}
	if n.MemoryUsage != nil {
		a.MemoryUsage = Known(*n.MemoryUsage)
	}
	if n.WindowMethod == "frontmost_window_for_event_pid" {
		reason := "frontmost visible window belonging to the input event target PID"
		a.WindowID.Evidence, a.WindowID.Reason = Inferred, reason
		a.Bounds.Evidence, a.Bounds.Reason = Inferred, reason
	}
	applyTargetEvidence(a, n.TargetMethod)
	if browserKind(a.BundleID.Value) != 0 || browserName(a.Name.Value+" "+a.Executable.Value) {
		b := &Browser{}
		if n.Title != "" {
			b.Title = Field[string]{Value: n.Title, Evidence: Inferred, Reason: "window title of the attributed browser process"}
		}
		if strings.HasPrefix(n.Document, "https://") || strings.HasPrefix(n.Document, "http://") {
			b.URL = Field[string]{Value: n.Document, Evidence: Inferred, Reason: "focused window accessibility document URL"}
		}
		e.Destination.Browser = b
	}
	return decodedObservation{Observation: Observation{Key: n.Key, Event: e}, dropped: n.Dropped}, nil
}

func applyTargetEvidence(a *Application, method string) {
	switch method {
	case "event_target_pid":
		a.PID.Reason = "Core Graphics event target process ID"
	case "accessibility_focus":
		reason := "event target PID unavailable; focused application sampled through Accessibility after input"
		a.PID.Evidence, a.PID.Reason = Inferred, reason
		for _, f := range []*Field[string]{&a.Name, &a.Executable, &a.WindowID, &a.WindowTitle, &a.BundleID, &a.BundlePath} {
			if f.Evidence != "" {
				f.Evidence, f.Reason = Inferred, reason
			}
		}
		if a.Bounds.Value != nil {
			a.Bounds.Evidence, a.Bounds.Reason = Inferred, reason
		}
		if a.ContentBounds.Value != nil {
			a.ContentBounds.Evidence, a.ContentBounds.Reason = Inferred, reason
		}
		if a.MemoryUsage.Evidence != "" {
			a.MemoryUsage.Evidence, a.MemoryUsage.Reason = Inferred, reason
		}
	case "unavailable":
		reason := "event target PID and live Accessibility focus are unavailable; no cached application was used"
		*a = Application{
			PID: Missing[int](reason), Name: Missing[string](reason),
			Executable: Missing[string](reason), WindowID: Missing[string](reason), WindowTitle: Missing[string](reason),
		}
	}
}

func browserName(s string) bool {
	s = strings.ToLower(s)
	for _, name := range []string{"chrome", "chromium", "firefox", "safari", "msedge", "microsoft edge", "brave", "vivaldi", "opera"} {
		if strings.Contains(s, name) {
			return true
		}
	}
	return false
}

// Run keeps the native event pump on one OS thread and decouples it from a slow
// event consumer. Overflow is reported; an unbounded clipboard queue is avoided.
func (s *nativeSource) Run(ctx context.Context, emit func(Observation) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !nativeRunning.CompareAndSwap(false, true) {
		return ErrRunning
	}
	defer nativeRunning.Store(false)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan []byte, s.queueSize)
	done := make(chan error, 1)
	stopped := make(chan struct{})
	var dropped atomic.Uint64
	go func() {
		defer close(stopped)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := s.bridge.open(s.options.MaxContentBytes); err != nil {
			done <- err
			return
		}
		defer s.bridge.close()
		for ctx.Err() == nil {
			data, err := s.bridge.next()
			if err != nil {
				done <- err
				return
			}
			if len(data) == 0 {
				continue
			}
			select {
			case queue <- data:
			default:
				dropped.Add(1)
			}
		}
		done <- ctx.Err()
	}()
	defer func() { cancel(); <-stopped }()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			if ctx.Err() == nil {
				sinkErr := emit(Observation{Capabilities: []Capability{{Feature: "monitoring", Status: "unavailable", Reason: fmt.Sprintf("native capture stopped: %v; %d buffered observations undelivered", err, uint64(len(queue))+dropped.Load())}}})
				return errors.Join(err, sinkErr)
			}
			return err
		case data := <-queue:
			if count := dropped.Swap(0); count > 0 {
				if err := emit(Observation{Capabilities: []Capability{{Feature: "event_delivery", Status: "partial", Reason: fmt.Sprintf("%d observations dropped because the consumer could not keep up", count)}}}); err != nil {
					return err
				}
			}
			o, err := decodeSnapshot(data, s.options.MaxContentBytes)
			if err != nil {
				return err
			}
			if o.dropped > 0 {
				if err := emit(Observation{Capabilities: []Capability{{Feature: "native_event_delivery", Status: "partial", Reason: fmt.Sprintf("%d native observations dropped", o.dropped)}}}); err != nil {
					return err
				}
			}
			s.enrich(ctx, &o.Event)
			if err := emit(o.Observation); err != nil {
				return err
			}
		}
	}
}
