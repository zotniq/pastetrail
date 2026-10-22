package capture

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zotniq/pastetrail/internal/content"
	"github.com/zotniq/pastetrail/internal/model"
)

func TestWindowMetadata(t *testing.T) {
	for _, method := range []string{"event_target_pid", "accessibility_focus", "unavailable"} {
		data, _ := json.Marshal(map[string]any{
			"key": "window", "time_ns": 1, "pid": 42, "bundle_id": "com.google.Chrome", "bundle_path": "/Applications/Google Chrome.app",
			"window": "123", "bounds": model.WindowBounds{X: -120, Y: 0, Width: 1000, Height: 800}, "content_bounds": model.WindowBounds{X: -120, Y: 24, Width: 1000, Height: 776}, "memory_usage": 123456,
			"target_method": method, "window_method": "frontmost_window_for_event_pid",
		})
		o, err := decodeSnapshot(data, 1024)
		if err != nil {
			t.Fatal(err)
		}
		content.NormalizeEvent(&o.Event, 1024)
		a := o.Event.Destination.Application
		if method == "unavailable" {
			if a.Bounds.Value != nil || a.MemoryUsage.Value != 0 || a.BundleID.Value != "" || o.Event.Destination.Browser != nil {
				t.Fatal(a)
			}
			continue
		}
		if a.BundleID.Value != "com.google.Chrome" || a.BundlePath.Value == "" || a.Bounds.Value.X != -120 || a.ContentBounds.Value.Height != 776 || a.MemoryUsage.Value != 123456 || a.WindowID.Evidence != model.Inferred || a.Bounds.Evidence != model.Inferred || o.Event.Destination.Browser == nil {
			t.Fatal(a)
		}
		if method == "accessibility_focus" && (a.BundleID.Evidence != model.Inferred || a.MemoryUsage.Evidence != model.Inferred) {
			t.Fatal(a)
		}
		encoded, _ := json.Marshal(o.Event)
		if !strings.Contains(string(encoded), `"memory_usage"`) {
			t.Fatal(string(encoded))
		}
	}
}
