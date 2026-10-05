// Package reader fetches pages through a headless browser, waits for the
// page to settle (which is what makes SPA support work), and converts the
// rendered HTML to markdown. It reuses per-host session state when
// available so authenticated pages render as the logged-in user.
package reader

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/lukaszraczylo/mcp-readitall/internal/browser"
)

// Default values exposed so tool handlers can reference them.
const (
	DefaultTimeout = 30 * time.Second
	MaxMarkdown    = 200_000 // hard ceiling on returned markdown size
)

// Reader is a thin facade over the Browser wrapper and session store.
type Reader struct {
	pw   *browser.Browser
	sess *browser.SessionStore
	log  *log.Logger
}

// New constructs a Reader. The store is consulted on every read to decide
// whether to attach a saved storage state to the new browser context.
func New(pw *browser.Browser, sess *browser.SessionStore, logger *log.Logger) *Reader {
	if logger == nil {
		logger = log.Default()
	}
	return &Reader{pw: pw, sess: sess, log: logger}
}

// ReadResult is the normalised output of a read operation.
type ReadResult struct {
	URL         string `json:"url"`
	FinalURL    string `json:"final_url"`
	Title       string `json:"title"`
	Markdown    string `json:"markdown"`
	UsedSession bool   `json:"used_session"`
	Truncated   bool   `json:"truncated"`
	Bytes       int    `json:"bytes"`
}

// ReadOptions controls a single Read call.
type ReadOptions struct {
	URL          string        // page to fetch
	Selector     string        // optional CSS selector; if set, content is extracted from the matching element(s) only
	WaitSelector string        // optional CSS selector to wait for before extracting (useful for SPA hydration)
	Timeout      time.Duration // overall timeout for navigation + waits
	MaxChars     int           // truncate markdown to roughly this many characters (0 = no truncation)
}

// Read fetches the URL through Chromium, waits for it to settle, optionally
// narrows the content with a CSS selector, and converts the resulting HTML
// to markdown.
func (r *Reader) Read(parentCtx context.Context, opts ReadOptions) (*ReadResult, error) {
	if opts.URL == "" {
		return nil, errors.New("url is required")
	}
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.MaxChars == 0 {
		opts.MaxChars = MaxMarkdown
	}

	timeoutCtx, cancel := context.WithTimeout(parentCtx, opts.Timeout)
	defer cancel()

	usedSession, statePath, err := r.resolveSession(opts.URL)
	if err != nil {
		return nil, err
	}

	cdpCtx, releaseBrowser, err := r.pw.NewContext(timeoutCtx, statePath)
	if err != nil {
		return nil, err
	}
	defer releaseBrowser()

	r.log.Printf("read %s (session=%v)", opts.URL, usedSession)

	var title, finalURL string
	var html string

	// Build the action chain. Cookies were already applied by NewContext
	// (if a session exists). localStorage is applied post-navigation
	// because it is origin-scoped.
	actions := []chromedp.Action[chromedp.Void]{
		chromedp.Navigate(opts.URL),
		chromedp.WaitReady("body"),
	}
	if statePath != "" {
		actions = append(actions, chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
			return browser.ApplyLocalStorageOnCurrentOrigin(ctx, statePath)
		}))
	}
	// Small grace period so post-load JS (lazy components, hydration) has
	// a chance to settle before we snapshot the HTML.
	actions = append(actions,
		chromedp.Sleep(500*time.Millisecond),
	)
	if opts.WaitSelector != "" {
		actions = append(actions, chromedp.WaitVisible(chromedp.CSS(opts.WaitSelector)))
	}
	actions = append(actions, chromedp.Func(func(ctx context.Context, t *chromedp.Target) (err error) {
		if title, err = chromedp.Title()(ctx, t); err != nil {
			return err
		}
		finalURL, err = chromedp.Location()(ctx, t)
		return err
	}))

	if err := chromedp.Do(cdpCtx, actions...); err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}

	html, err = r.extractHTML(cdpCtx, opts.Selector)
	if err != nil {
		return nil, err
	}

	md, err := HTMLToMarkdown(html, finalURL)
	if err != nil {
		return nil, fmt.Errorf("convert to markdown: %w", err)
	}

	truncated := false
	if len(md) > opts.MaxChars {
		md = md[:opts.MaxChars] + "\n\n…[truncated]"
		truncated = true
	}

	return &ReadResult{
		URL:         opts.URL,
		FinalURL:    finalURL,
		Title:       strings.TrimSpace(title),
		Markdown:    md,
		UsedSession: usedSession,
		Truncated:   truncated,
		Bytes:       len(md),
	}, nil
}

// resolveSession checks whether a saved session exists for the URL's host
// and returns its path. An empty path means "no session, use a fresh
// context".
func (r *Reader) resolveSession(rawURL string) (bool, string, error) {
	exists, err := r.sess.Exists(rawURL)
	if err != nil {
		return false, "", err
	}
	if !exists {
		return false, "", nil
	}
	path, err := r.sess.StatePath(rawURL)
	if err != nil {
		return false, "", err
	}
	return true, path, nil
}
