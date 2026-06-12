package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lukaszraczylo/mcp-readitall/internal/reader"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ExtractInput is the schema for the extract tool. It is essentially
// read_url with a default selector set to <main> when omitted, so common CMS
// layouts work without ceremony.
type ExtractInput struct {
	URL            string `json:"url" jsonschema:"Absolute http(s) URL to read"`
	Selector       string `json:"selector,omitempty" jsonschema:"CSS selector to extract. Defaults to 'main, article, [role=main]' which works for most content sites."`
	WaitFor        string `json:"wait_for,omitempty" jsonschema:"Optional CSS selector to wait for before extracting (useful for SPAs)"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Overall timeout in seconds (1-300, default 30)"`
	MaxChars       int    `json:"max_chars,omitempty" jsonschema:"Truncate returned markdown to roughly this many characters (100-500000)"`
}

// ExtractOutput is the structured result.
type ExtractOutput struct {
	URL         string `json:"url"`
	FinalURL    string `json:"final_url"`
	Title       string `json:"title"`
	Markdown    string `json:"markdown"`
	Selector    string `json:"selector"`
	UsedSession bool   `json:"used_session"`
	Truncated   bool   `json:"truncated"`
	Bytes       int    `json:"bytes"`
}

// RegisterExtract wires the extract tool to the server.
func RegisterExtract(s *mcp.Server, r *reader.Reader) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "extract",
		Description: "Fetch a URL and return only the content under one (or more, comma-separated) CSS selectors, converted to Markdown. This is the right tool when you want the page's main body without nav, headers, footers, and sidebars. Works on SPA and SSR sites.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ExtractInput) (*mcp.CallToolResult, ExtractOutput, error) {
		if in.URL == "" {
			return nil, ExtractOutput{}, fmt.Errorf("url is required")
		}
		selector := in.Selector
		if selector == "" {
			selector = "main, article, [role=main]"
		}
		timeout := time.Duration(in.TimeoutSeconds) * time.Second

		res, err := r.Read(ctx, reader.ReadOptions{
			URL:          in.URL,
			Selector:     firstSelector(selector),
			WaitSelector: in.WaitFor,
			Timeout:      timeout,
			MaxChars:     in.MaxChars,
		})
		if err != nil {
			return nil, ExtractOutput{}, err
		}
		out := ExtractOutput{
			URL:         res.URL,
			FinalURL:    res.FinalURL,
			Title:       res.Title,
			Markdown:    res.Markdown,
			Selector:    selector,
			UsedSession: res.UsedSession,
			Truncated:   res.Truncated,
			Bytes:       res.Bytes,
		}
		var b strings.Builder
		b.WriteString("# ")
		b.WriteString(out.Title)
		if out.Title == "" {
			b.WriteString(out.URL)
		}
		b.WriteString("\n\n")
		b.WriteString(out.Markdown)
		if out.Truncated {
			b.WriteString("\n\n[truncated]")
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: b.String()}},
		}, out, nil
	})
}

// firstSelector picks the first non-empty selector from a comma-separated
// list. The reader is intentionally simple and only supports a single
// selector today; we surface the first to keep the schema natural for
// humans ("main, article, [role=main]") without pretending to support
// union selectors internally. Empty entries are skipped so leading or
// consecutive commas (" , main") do not produce a blank selector.
func firstSelector(selectorList string) string {
	for _, p := range strings.Split(selectorList, ",") {
		if s := strings.TrimSpace(p); s != "" {
			return s
		}
	}
	return ""
}
