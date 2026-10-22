package capture

import (
	"context"
	"time"

	"github.com/zotniq/pastetrail/internal/browser"
	"github.com/zotniq/pastetrail/internal/model"
	"github.com/zotniq/pastetrail/internal/platform"
	"github.com/zotniq/pastetrail/internal/terminal"
)

func enrichDestination(ctx context.Context, e *model.Event) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	browser.Enrich(ctx, e, platform.BrowserURL)
	terminal.Enrich(ctx, e)
}
