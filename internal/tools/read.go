// Package tools registers each MCP tool with the server. One file per tool
// keeps the schemas, handlers, and tool registration together so changes are
// easy to audit.
package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lukaszraczylo/mcp-readitall/internal/reader"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ReadInput is the JSON schema for the read_url tool.
type ReadInput struct {
	URL            string `json:"url" jsonschema:"Absolute http(s) URL to read"`
	Selector       string `json:"selector,omitempty" jsonschema:"Optional CSS selector to extract only a region of the page"`
	WaitFor        string `json:"wait_for,omitempty" jsonschema:"Optional CSS selector to wait for before extracting (useful for SPAs that hydrate after networkidle)"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Overall timeout in seconds (1-300, default 30)"`
	MaxChars       int    `json:"max_chars,omitempty" jsonschema:"Truncate returned markdown to roughly this many characters (100-500000)"`
}

// ReadOutput is returned to the MCP client. The Markdown field is the primary
// payload; other fields give the model enough context to decide what to do
// next.
type ReadOutput struct {
	URL         string `json:"url"`
	FinalURL    string `json:"final_url"`
	Title       string `json:"title"`
	Markdown    string `json:"markdown"`
	UsedSession bool   `json:"used_session"`
	Truncated   bool   `json:"truncated"`
	Bytes       int    `json:"bytes"`
}

// ReadResult is the text-side companion of ReadOutput, surfaced via the
// CallToolResult's Content for clients that ignore structured content.
func readText(o ReadOutput) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(o.Title)
	if o.Title == "" {
		b.WriteString(o.URL)
	}
	b.WriteString("\n\n")
	b.WriteString(o.Markdown)
	if o.Truncated {
		b.WriteString("\n\n[truncated]")
	}
	if o.FinalURL != "" && o.FinalURL != o.URL {
		b.WriteString("\n\n_Resolved to: ")
		b.WriteString(o.FinalURL)
		b.WriteString("_")
	}
	if o.UsedSession {
		b.WriteString("\n\n_Used saved session._")
	}
	return b.String()
}

// RegisterRead wires the read_url tool to the server.
func RegisterRead(s *mcp.Server, r *reader.Reader) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_url",
		Description: "Fetch a URL through a headless browser (full SPA support), render the page, and return its content as Markdown. If a session has previously been saved for the URL's host via the login tool, it is used automatically so authenticated pages render as the logged-in user.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ReadInput) (*mcp.CallToolResult, ReadOutput, error) {
		if in.URL == "" {
			return nil, ReadOutput{}, fmt.Errorf("url is required")
		}
		timeout := time.Duration(in.TimeoutSeconds) * time.Second
		res, err := r.Read(ctx, reader.ReadOptions{
			URL:          in.URL,
			Selector:     in.Selector,
			WaitSelector: in.WaitFor,
			Timeout:      timeout,
			MaxChars:     in.MaxChars,
		})
		if err != nil {
			return nil, ReadOutput{}, err
		}
		out := ReadOutput{
			URL:         res.URL,
			FinalURL:    res.FinalURL,
			Title:       res.Title,
			Markdown:    res.Markdown,
			UsedSession: res.UsedSession,
			Truncated:   res.Truncated,
			Bytes:       res.Bytes,
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: readText(out)}},
		}, out, nil
	})
}
