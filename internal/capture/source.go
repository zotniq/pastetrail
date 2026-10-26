package capture

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/zotniq/pastetrail/internal/model"
)

type nativeBridge interface {
	Probe() string
	Open(int) error
	Next() ([]byte, error)
	Close()
}

type nativeSource struct {
	bridge          nativeBridge
	maxContentBytes int
	enrich          func(context.Context, *model.Event)
	queueSize       int
}

var nativeRunning atomic.Bool

func newNativeSource(b nativeBridge, maxContentBytes int) *nativeSource {
	// Budget encoded snapshots as well as their decoded contents.
	queueSize := max(1, min(64, (32<<20)/max(1, maxContentBytes*2)))
	return &nativeSource{bridge: b, maxContentBytes: maxContentBytes, queueSize: queueSize, enrich: enrichDestination}
}

func (s *nativeSource) Capabilities(context.Context) []model.Capability {
	status, reason, ok := strings.Cut(s.bridge.Probe(), "|")
	if !ok {
		reason = "native backend returned an invalid capability response"
		status = "unavailable"
	}
	if status == "supported" {
		status = "partial"
	}
	return []model.Capability{
		{Feature: "keyboard_paste_attempts", Status: status, Reason: reason},
		{Feature: "menu_context_menu_programmatic_and_app_internal_pastes", Status: "unavailable", Reason: "applications do not expose a universal external paste-attempt notification"},
		{Feature: "middle_click", Status: middleClickStatus(status), Reason: "X11 PRIMARY selection; macOS iTerm2 saved clipboard/selection middle-button bindings only; iTerm selection content and clicked pane identity unavailable"},
		{Feature: "clipboard_content", Status: status, Reason: "text, HTML, RTF, and file references when exposed; snapshots can race clipboard changes"},
		{Feature: "application_context", Status: status, Reason: "macOS event target PID or inferred Accessibility focus; foreground context on other platforms; available metadata depends on permissions and applications"},
		focusCapability(),
		{Feature: "browser_context", Status: "partial", Reason: "macOS Safari/Chromium active-tab URL from the Accessibility web area (one grant, no per-browser Automation), with Automation and the window document as fallbacks; window title elsewhere; tab IDs, renderer PIDs and frames unavailable"},
		{Feature: "attempt_confirmation", Status: "unavailable", Reason: "shortcut observation indicates possible intent; insertion, clipboard format consumption, and application remapping are not observable"},
	}
}

func focusCapability() model.Capability {
	if runtime.GOOS == "darwin" {
		return model.Capability{Feature: "focused_control_context", Status: "partial", Reason: "requires Accessibility permission and app accessibility support; control metadata and focused document URI are recorded when exposed; terminal sessions are identified separately through the PTY relay"}
	}
	return model.Capability{Feature: "focused_control_context", Status: "unavailable", Reason: "focused control capture is currently implemented only on macOS"}
}

func middleClickStatus(status string) string {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		return status
	}
	return "unavailable"
}

// Run keeps the native event pump on one OS thread and decouples it from a slow
// event consumer. Overflow is reported; an unbounded clipboard queue is avoided.
func (s *nativeSource) Run(ctx context.Context, emit func(model.Observation) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !nativeRunning.CompareAndSwap(false, true) {
		return model.ErrRunning
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
		if err := s.bridge.Open(s.maxContentBytes); err != nil {
			done <- err
			return
		}
		defer s.bridge.Close()
		for ctx.Err() == nil {
			data, err := s.bridge.Next()
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
				sinkErr := emit(model.Observation{Capabilities: []model.Capability{{Feature: "monitoring", Status: "unavailable", Reason: fmt.Sprintf("native capture stopped: %v; %d buffered observations undelivered", err, uint64(len(queue))+dropped.Load())}}})
				return errors.Join(err, sinkErr)
			}
			return err
		case data := <-queue:
			if count := dropped.Swap(0); count > 0 {
				if err := emit(model.Observation{Capabilities: []model.Capability{{Feature: "event_delivery", Status: "partial", Reason: fmt.Sprintf("%d observations dropped because the consumer could not keep up", count)}}}); err != nil {
					return err
				}
			}
			o, err := decodeSnapshot(data, s.maxContentBytes)
			if err != nil {
				return err
			}
			if o.dropped > 0 {
				if err := emit(model.Observation{Capabilities: []model.Capability{{Feature: "native_event_delivery", Status: "partial", Reason: fmt.Sprintf("%d native observations dropped", o.dropped)}}}); err != nil {
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
