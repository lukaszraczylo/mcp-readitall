package tools

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
	"github.com/lukaszraczylo/mcp-readitall/internal/browser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// LoginInput is the schema for the login tool. The flow is:
//
//  1. A visible Chromium window opens and navigates to URL.
//  2. The human operator (you) authenticates by hand in that window.
//  3. The tool watches for SuccessURL (substring match against the
//     address bar) or SuccessSelector (a CSS selector that becomes
//     visible, e.g. an avatar dropdown) and, when matched, persists the
//     browser's storage state to the canonical session path for the
//     host.
//  4. Future read_url calls against this host will reuse the saved
//     state.
//
// On a headless host (e.g. a remote MCP server) the visible-window
// approach will not work; the operator must run the MCP server on a
// machine with a display. We surface a clear error in that case so the
// caller knows the limitation is environmental, not a bug.
type LoginInput struct {
	URL             string `json:"url" jsonschema:"Absolute http(s) URL to open (typically the login page)"`
	SuccessURL      string `json:"success_url,omitempty" jsonschema:"Substring of the URL bar that indicates login succeeded (e.g. /dashboard). Polled every second."`
	SuccessSelector string `json:"success_selector,omitempty" jsonschema:"CSS selector that becomes visible once login is complete (e.g. an avatar menu). Polled every second."`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty" jsonschema:"How long to wait for a successful login in seconds (5-3600, default 300)"`
}

// LoginOutput reports what happened.
type LoginOutput struct {
	Domain   string `json:"domain"`
	Saved    bool   `json:"saved"`
	Path     string `json:"path,omitempty"`
	FinalURL string `json:"final_url"`
	Message  string `json:"message"`
}

// RegisterLogin wires the login tool to the server.
func RegisterLogin(s *mcp.Server, pw *browser.Browser, sess *browser.SessionStore, logger *log.Logger) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "login",
		Description: "Open a visible browser to a URL, wait for the human operator to authenticate, then persist the resulting browser storage state (cookies + localStorage) for the host. Subsequent read_url calls against this host will use the saved state automatically. Useful for sites that require login to expose their content.",
	}, func(parentCtx context.Context, _ *mcp.CallToolRequest, in LoginInput) (*mcp.CallToolResult, LoginOutput, error) {
		if in.URL == "" {
			return nil, LoginOutput{}, fmt.Errorf("url is required")
		}
		if in.SuccessURL == "" && in.SuccessSelector == "" {
			return nil, LoginOutput{}, fmt.Errorf("success_url or success_selector is required so the tool knows when login is complete")
		}
		timeout := time.Duration(in.TimeoutSeconds) * time.Second
		if timeout == 0 {
			timeout = 5 * time.Minute
		}

		ctx, cancel := context.WithTimeout(parentCtx, timeout)
		defer cancel()

		browserCtx, releaseBrowser, err := pw.NewHeadedContext()
		if err != nil {
			return nil, LoginOutput{}, fmt.Errorf("launch headed browser: %w", err)
		}
		defer releaseBrowser()

		// Start from a clean cookie jar so a stale session from a
		// previous attempt cannot mask the new login's success.
		if err := browser.ClearAllCookies(browserCtx); err != nil {
			return nil, LoginOutput{}, fmt.Errorf("clear cookies: %w", err)
		}

		var title string
		if err := chromedp.Run(browserCtx,
			chromedp.Navigate(in.URL),
			chromedp.WaitReady("body"),
			chromedp.Title(&title),
		); err != nil {
			return nil, LoginOutput{}, fmt.Errorf("navigate: %w", err)
		}

		logger.Printf("login flow started for %s, waiting for success condition", in.URL)

		if err := waitForLogin(ctx, browserCtx, in, timeout); err != nil {
			return nil, LoginOutput{
				Domain:   hostOf(in.URL),
				FinalURL: currentLocation(browserCtx),
				Message:  err.Error(),
			}, fmt.Errorf("login did not complete: %w", err)
		}

		finalURL := currentLocation(browserCtx)

		statePath, err := sess.StatePath(in.URL)
		if err != nil {
			return nil, LoginOutput{}, err
		}
		if err := browser.CaptureStorageState(browserCtx, statePath); err != nil {
			return nil, LoginOutput{
				Domain:   hostOf(in.URL),
				FinalURL: finalURL,
				Message:  "success condition matched but capturing storage state failed",
			}, err
		}

		// Validate the captured state has at least one cookie or origin
		// entry, otherwise we would happily save an "authenticated" file
		// even when the success condition matched an unrelated selector.
		if err := validateCapturedState(statePath); err != nil {
			return nil, LoginOutput{
				Domain:   hostOf(in.URL),
				FinalURL: finalURL,
				Message:  "success condition matched but storage state is empty; auth likely failed",
			}, err
		}

		msg := fmt.Sprintf("session saved for %s", hostOf(in.URL))
		return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: msg}},
			}, LoginOutput{
				Domain:   hostOf(in.URL),
				Saved:    true,
				Path:     statePath,
				FinalURL: finalURL,
				Message:  msg,
			}, nil
	})
}

// waitForLogin polls the page once per second until the success
// condition is met or the timeout fires. It uses chromedp.Location and
// chromedp.Nodes (with a visibility check) for the two supported
// condition types.
func waitForLogin(ctx context.Context, pageCtx context.Context, in LoginInput, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	check := func() (bool, error) {
		if in.SuccessURL != "" {
			var current string
			if err := chromedp.Run(pageCtx, chromedp.Location(&current)); err != nil {
				return false, err
			}
			if strings.Contains(current, in.SuccessURL) {
				return true, nil
			}
		}
		if in.SuccessSelector != "" {
			var nodes []*cdp.Node
			if err := chromedp.Run(pageCtx, chromedp.Nodes(in.SuccessSelector, &nodes, chromedp.ByQuery)); err != nil {
				return false, err
			}
			if len(nodes) > 0 {
				var visible bool
				if err := chromedp.Run(pageCtx, chromedp.Evaluate(
					`(()=>{const e=document.querySelector(`+jsQuote(in.SuccessSelector)+`);return !!e && e.getBoundingClientRect().height>0;})()`,
					&visible,
				)); err != nil {
					return false, err
				}
				if visible {
					return true, nil
				}
			}
		}
		return false, nil
	}

	// Immediate check so an already-authenticated session is captured
	// without a 1-second wait.
	if ok, err := check(); err != nil {
		return err
	} else if ok {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timed out after %s waiting for login", timeout)
			}
			ok, err := check()
			if err != nil {
				return err
			}
			if ok {
				return nil
			}
		}
	}
}

// currentLocation returns the page's current URL or "" if the query
// fails (e.g. the browser has already been torn down).
func currentLocation(ctx context.Context) string {
	var s string
	if err := chromedp.Run(ctx, chromedp.Location(&s)); err != nil {
		return ""
	}
	return s
}

// validateCapturedState reads the just-saved storage state file and
// ensures it has at least one cookie or one localStorage entry. This
// guards against "login success" false positives (e.g. selector matches
// a generic header element that happens to be visible).
func validateCapturedState(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	return browser.ValidateStorageState(raw)
}
