package browser

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/zotniq/pastetrail/internal/model"
)

// These applications expose Chromium's active-tab URL or Safari's document URL.
func kindForBundle(bundle string) int {
	switch bundle {
	case "com.apple.Safari", "com.apple.SafariTechnologyPreview":
		return 2
	case "com.google.Chrome", "com.google.Chrome.beta", "com.google.Chrome.dev", "com.google.Chrome.canary",
		"com.brave.Browser", "com.brave.Browser.beta", "com.brave.Browser.nightly",
		"com.microsoft.edgemac", "com.microsoft.edgemac.Beta", "com.microsoft.edgemac.Dev", "com.microsoft.edgemac.Canary",
		"com.mighty.app", "com.ghostbrowser.gb1", "com.bookry.wavebox", "com.pushplaylabs.sidekick",
		"com.operasoftware.Opera", "com.operasoftware.OperaNext", "com.operasoftware.OperaDeveloper", "com.operasoftware.OperaGX",
		"com.vivaldi.Vivaldi", "company.thebrowser.Browser":
		return 1
	}
	return 0
}

func Enrich(ctx context.Context, e *model.Event, query func(int, int) (string, int)) {
	a := e.Destination.Application
	b := e.Destination.Browser
	kind := kindForBundle(a.BundleID.Value)
	if ctx.Err() != nil || b == nil || kind == 0 || a.PID.Value <= 0 {
		return
	}
	value, status := query(a.PID.Value, kind)
	if status == 0 {
		u, err := url.Parse(value)
		if err == nil && u.IsAbs() && ((u.Scheme != "http" && u.Scheme != "https") || u.Host != "") {
			b.URL = model.Field[string]{Value: value, Evidence: model.Inferred, Reason: "active tab URL queried from the event target PID through macOS Automation after input"}
			return
		}
	}
	// Preserve a usable Accessibility document URL if Automation is unavailable.
	if b.URL.Value != "" {
		return
	}
	reason := fmt.Sprintf("browser active-tab URL query failed (status %d)", status)
	if status == -1743 || status == -1744 {
		reason = "macOS Automation access to this browser is required; authorize a read-only URL query from the terminal running PasteTrail"
	} else if status == 0 {
		reason = "browser did not return an absolute URL"
	}
	b.URL = model.Missing[string](reason)
}

func matchesName(s string) bool {
	s = strings.ToLower(s)
	for _, name := range []string{"chrome", "chromium", "firefox", "safari", "msedge", "microsoft edge", "brave", "vivaldi", "opera"} {
		if strings.Contains(s, name) {
			return true
		}
	}
	return false
}

// FromApplication creates browser context when the attributed application is recognized.
func FromApplication(a model.Application, title, document string) *model.Browser {
	if kindForBundle(a.BundleID.Value) == 0 && !matchesName(a.Name.Value+" "+a.Executable.Value) {
		return nil
	}
	b := &model.Browser{}
	if title != "" {
		b.Title = model.Field[string]{Value: title, Evidence: model.Inferred, Reason: "window title of the attributed browser process"}
	}
	if strings.HasPrefix(document, "https://") || strings.HasPrefix(document, "http://") {
		b.URL = model.Field[string]{Value: document, Evidence: model.Inferred, Reason: "focused window accessibility document URL"}
	}
	return b
}
