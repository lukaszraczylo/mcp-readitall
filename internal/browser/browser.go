// Package browser manages the chromedp driver lifecycle used to fetch
// pages, render SPAs, and capture storage state. The browser process is
// started lazily on first use and shared across all subsequent reads.
//
// chromedp is a pure-Go Chrome DevTools Protocol client: no Node.js bridge,
// no Playwright driver. It talks to a local Chromium (or any other CDP-
// compatible browser) over a websocket.
package browser

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Browser is a thin wrapper around a long-lived chromedp ExecAllocator and
// its single Chromium process. Each read (or login) creates a fresh
// incognito-like browser context on top, so state never leaks between
// calls.
type Browser struct {
	log *log.Logger

	startOnce sync.Once
	startErr  error

	allocCtx    context.Context
	allocCancel context.CancelFunc
	browserCtx  context.Context // base context that owns the browser allocation
}

// New returns a Browser manager. The underlying Chromium is not started
// until the first call that needs it; this keeps server startup cheap and
// gives a clear error path if Chrome is missing.
func New(_ context.Context, logger *log.Logger) (*Browser, error) {
	if logger == nil {
		logger = log.Default()
	}
	return &Browser{log: logger}, nil
}

// ensureAllocator starts a headless Chromium the first time it is needed
// and reuses it for every subsequent call. The flags mirror the old
// Playwright defaults: no sandbox (needed inside many CI / container
// environments), no default browser check, GPU disabled for headless.
//
// chromedp allocates the browser lazily on the first action; we eagerly
// run a no-op Navigate("about:blank") on a base context so the
// allocation is done once, up front, and the base context is ready to
// host WithNewBrowserContext children. Without this warmup,
// chromedp.NewContext(alloc, WithNewBrowserContext) panics because
// WithNewBrowserContext requires a *Browser to already be set on the
// parent chromedp context.
func (b *Browser) ensureAllocator() (context.Context, error) {
	b.startOnce.Do(func() {
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.NoSandbox,
			chromedp.NoDefaultBrowserCheck,
			chromedp.DisableGPU,
			chromedp.Flag("disable-dev-shm-usage", true),
		)
		b.allocCtx, b.allocCancel = chromedp.NewExecAllocator(context.Background(), opts...)

		browserCtx, cancel := chromedp.NewContext(b.allocCtx)
		if err := chromedp.Run(browserCtx, chromedp.Navigate("about:blank")); err != nil {
			b.startErr = fmt.Errorf("warm up browser: %w", err)
			cancel()
			return
		}
		// The base context must stay alive for the lifetime of the
		// browser. We intentionally do not call cancel() here; the
		// alloc's cancel will cascade when the Browser is closed.
		b.browserCtx = browserCtx
	})
	return b.allocCtx, b.startErr
}

// NewContext returns a fresh chromedp context ready for a read. The
// first call triggers the browser process startup; subsequent calls
// share the same Chromium process. The caller's cancel() tears down the
// chromedp context (closing its tab) but leaves the singleton browser
// alive. Cancellation or deadline on parentCtx is propagated to the
// chromedp context, so a timeout applied by the caller is honoured by
// the browser actions.
//
// State isolation: the previous read's cookies are wiped with
// network.ClearBrowserCookies before this call returns. This is the
// manual equivalent of chromedp.WithNewBrowserContext; we use it
// because WithNewBrowserContext is broken in chromedp v0.15.1 (the
// first child context fails with "no browser is open"). localStorage
// is origin-scoped so it is naturally wiped when we navigate to a new
// URL, except in the rare case where two reads target the same origin
// — that is handled by the reader's post-navigation re-apply step.
//
// If statePath points to a valid storage state file, its cookies are
// then applied to the (now empty) cookie jar via CDP before navigation.
// (The caller is responsible for applying localStorage post-navigation;
// see reader.applyLocalStorage.)
func (b *Browser) NewContext(parentCtx context.Context, statePath string) (context.Context, context.CancelFunc, error) {
	if _, err := b.ensureAllocator(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := chromedp.NewContext(b.browserCtx)

	// Wipe cookies from any previous call before applying the new
	// state. See the doc comment above for why we do this manually
	// instead of using WithNewBrowserContext.
	if err := ClearAllCookies(ctx); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("clear cookies: %w", err)
	}

	// Forward parentCtx cancellation into the chromedp context. The
	// goroutine exits as soon as either side is done, so it does not
	// leak even on the happy path.
	done := make(chan struct{})
	go func() {
		select {
		case <-parentCtx.Done():
			cancel()
		case <-done:
		}
	}()

	if statePath != "" {
		if err := ApplyStorageStateCookies(ctx, statePath); err != nil {
			cancel()
			close(done)
			return nil, nil, fmt.Errorf("apply storage state: %w", err)
		}
	}
	return ctx, func() {
		cancel()
		close(done)
	}, nil
}

// ClearAllCookies wipes every cookie in the current browser context via
// CDP. Exposed for the NewHeadedContext login flow, which uses it as
// the first step of a fresh login (otherwise stale session cookies from
// a previous attempt can mask the new login's success).
func ClearAllCookies(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		target := chromedp.FromContext(ctx).Target
		return network.ClearBrowserCookies().Do(cdp.WithExecutor(ctx, target))
	}))
}

// NewHeadedContext spawns a *separate* Chromium process with a visible
// window. It is used only by the login tool. Headed mode is intentionally
// isolated from the singleton headless allocator because chromedp cannot
// toggle the headless flag on a running browser.
//
// On a server without a display (no $DISPLAY on Linux, no logged-in user
// on macOS), Chrome will exit immediately. Callers should surface a clear
// error to the operator.
func (b *Browser) NewHeadedContext() (context.Context, context.CancelFunc, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", false),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)
	// Tie the alloc lifetime to the returned cancel so the headed browser
	// is shut down when the caller is done.
	wrapped := context.AfterFunc(ctx, allocCancel)
	origCancel := cancel
	cancel = func() {
		origCancel()
		wrapped()
	}
	return ctx, cancel, nil
}

// Close shuts down the singleton browser. Safe to call once.
func (b *Browser) Close() error {
	if b.allocCancel != nil {
		b.allocCancel()
		b.allocCancel = nil
	}
	return nil
}
