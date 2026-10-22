package browser

import (
	"context"
	"testing"

	"github.com/zotniq/pastetrail/internal/model"
	"github.com/zotniq/pastetrail/internal/platform"
)

func TestBrowserAutomation(t *testing.T) {
	if matchesName("Terminal") {
		t.Fatal("terminal classified as browser")
	}
	for _, bundle := range []string{"com.google.Chrome", "com.apple.Safari", "com.apple.SafariTechnologyPreview", "company.thebrowser.Browser", "com.microsoft.edgemac", "com.brave.Browser"} {
		for _, value := range []string{"https://example.com/path?q=1", "file:///tmp/page.html", "about:blank", "chrome://newtab/"} {
			e := model.Event{Destination: model.Destination{Application: model.Application{PID: model.Known(42), BundleID: model.Known(bundle)}, Browser: &model.Browser{}}}
			Enrich(context.Background(), &e, func(pid, kind int) (string, int) {
				if pid != 42 || kind != kindForBundle(bundle) {
					t.Fatal(pid, kind)
				}
				return value, 0
			})
			if e.Destination.Browser.URL.Value != value || e.Destination.Browser.URL.Evidence != model.Inferred {
				t.Fatal(e)
			}
		}
	}
	for _, tc := range []struct {
		value  string
		status int
	}{{"", -1743}, {"", -1744}, {"", -1712}, {"", 0}, {"https://", 0}, {"search query", 0}, {"http://%", 0}} {
		for _, fallback := range []string{"", "https://fallback.example/"} {
			e := model.Event{Destination: model.Destination{Application: model.Application{PID: model.Known(42), BundleID: model.Known("com.google.Chrome")}, Browser: &model.Browser{URL: model.Field[string]{Value: fallback}}}}
			Enrich(context.Background(), &e, func(int, int) (string, int) { return tc.value, tc.status })
			if e.Destination.Browser.URL.Value != fallback || (fallback == "" && e.Destination.Browser.URL.Reason == "") {
				t.Fatal(e)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, e := range []model.Event{{}, {Destination: model.Destination{Browser: &model.Browser{}}}, {Destination: model.Destination{Application: model.Application{BundleID: model.Known("com.google.Chrome")}, Browser: &model.Browser{}}}} {
		Enrich(context.Background(), &e, func(int, int) (string, int) { t.Fatal("unexpected query"); return "", 0 })
		Enrich(ctx, &e, func(int, int) (string, int) { t.Fatal("query after cancellation"); return "", 0 })
	}
	if value, status := platform.BrowserURL(0, 1); value != "" || status == 0 {
		t.Fatal(value, status)
	}
}
