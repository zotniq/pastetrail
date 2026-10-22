package pastetrail

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBrowserAutomation(t *testing.T) {
	for _, bundle := range []string{"com.google.Chrome", "com.apple.Safari", "com.apple.SafariTechnologyPreview", "company.thebrowser.Browser", "com.microsoft.edgemac", "com.brave.Browser"} {
		for _, value := range []string{"https://example.com/path?q=1", "file:///tmp/page.html", "about:blank", "chrome://newtab/"} {
			e := Event{Destination: Destination{Application: Application{PID: Known(42), BundleID: Known(bundle)}, Browser: &Browser{}}}
			enrichBrowser(context.Background(), &e, func(pid, kind int) (string, int) {
				if pid != 42 || kind != browserKind(bundle) {
					t.Fatal(pid, kind)
				}
				return value, 0
			})
			if e.Destination.Browser.URL.Value != value || e.Destination.Browser.URL.Evidence != Inferred {
				t.Fatal(e)
			}
		}
	}
	for _, tc := range []struct {
		value  string
		status int
	}{{"", -1743}, {"", -1744}, {"", -1712}, {"", 0}, {"https://", 0}, {"search query", 0}, {"http://%", 0}} {
		for _, fallback := range []string{"", "https://fallback.example/"} {
			e := Event{Destination: Destination{Application: Application{PID: Known(42), BundleID: Known("com.google.Chrome")}, Browser: &Browser{URL: Field[string]{Value: fallback}}}}
			enrichBrowser(context.Background(), &e, func(int, int) (string, int) { return tc.value, tc.status })
			if e.Destination.Browser.URL.Value != fallback || (fallback == "" && e.Destination.Browser.URL.Reason == "") {
				t.Fatal(e)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, e := range []Event{{}, {Destination: Destination{Browser: &Browser{}}}, {Destination: Destination{Application: Application{BundleID: Known("com.google.Chrome")}, Browser: &Browser{}}}} {
		enrichBrowser(context.Background(), &e, func(int, int) (string, int) { t.Fatal("unexpected query"); return "", 0 })
		enrichBrowser(ctx, &e, func(int, int) (string, int) { t.Fatal("query after cancellation"); return "", 0 })
	}
	if value, status := nativeBrowserURL(0, 1); value != "" || status == 0 {
		t.Fatal(value, status)
	}
}

func TestWindowMetadata(t *testing.T) {
	for _, method := range []string{"event_target_pid", "accessibility_focus", "unavailable"} {
		data, _ := json.Marshal(map[string]any{
			"key": "window", "time_ns": 1, "pid": 42, "bundle_id": "com.google.Chrome", "bundle_path": "/Applications/Google Chrome.app",
			"window": "123", "bounds": WindowBounds{-120, 0, 1000, 800}, "content_bounds": WindowBounds{-120, 24, 1000, 776}, "memory_usage": 123456,
			"target_method": method, "window_method": "frontmost_window_for_event_pid",
		})
		o, err := decodeSnapshot(data, 1024)
		if err != nil {
			t.Fatal(err)
		}
		normalizeEvent(&o.Event, 1024)
		a := o.Event.Destination.Application
		if method == "unavailable" {
			if a.Bounds.Value != nil || a.MemoryUsage.Value != 0 || a.BundleID.Value != "" || o.Event.Destination.Browser != nil {
				t.Fatal(a)
			}
			continue
		}
		if a.BundleID.Value != "com.google.Chrome" || a.BundlePath.Value == "" || a.Bounds.Value.X != -120 || a.ContentBounds.Value.Height != 776 || a.MemoryUsage.Value != 123456 || a.WindowID.Evidence != Inferred || a.Bounds.Evidence != Inferred || o.Event.Destination.Browser == nil {
			t.Fatal(a)
		}
		if method == "accessibility_focus" && (a.BundleID.Evidence != Inferred || a.MemoryUsage.Evidence != Inferred) {
			t.Fatal(a)
		}
		encoded, _ := json.Marshal(o.Event)
		if !strings.Contains(string(encoded), `"memory_usage"`) {
			t.Fatal(string(encoded))
		}
	}
}
