package reader

import (
	"context"
	"fmt"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

// extractHTML returns the outer HTML of the first element matching
// selector, or the full document HTML when selector is empty. It runs as
// a separate chromedp.Run so a slow extraction does not re-do navigation.
func (r *Reader) extractHTML(ctx context.Context, selector string) (string, error) {
	if selector == "" {
		var html string
		if err := chromedp.Run(ctx, chromedp.OuterHTML("html", &html, chromedp.ByQuery)); err != nil {
			return "", fmt.Errorf("read document: %w", err)
		}
		return html, nil
	}

	// Verify the selector matches at least one node; an empty result is
	// almost always a caller mistake, not an empty page.
	var nodes []*cdp.Node
	if err := chromedp.Run(ctx, chromedp.Nodes(selector, &nodes, chromedp.ByQuery)); err != nil {
		return "", fmt.Errorf("locate %q: %w", selector, err)
	}
	if len(nodes) == 0 {
		return "", fmt.Errorf("selector %q matched no elements", selector)
	}

	var html string
	if err := chromedp.Run(ctx, chromedp.OuterHTML(selector, &html, chromedp.ByQuery)); err != nil {
		return "", fmt.Errorf("extract %q: %w", selector, err)
	}
	return html, nil
}
