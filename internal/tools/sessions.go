package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/lukaszraczylo/mcp-readitall/internal/browser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListSessionsInput is empty: the tool has no parameters.
type ListSessionsInput struct{}

// ListSessionsOutput lists every saved session.
type ListSessionsOutput struct {
	Sessions []browser.SessionInfo `json:"sessions"`
	Count    int                   `json:"count"`
}

// RegisterListSessions wires the list_sessions tool to the server.
func RegisterListSessions(s *mcp.Server, store *browser.SessionStore) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_sessions",
		Description: "List every host for which an authenticated browser session is currently saved. Each entry can be passed to clear_session to invalidate it.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ ListSessionsInput) (*mcp.CallToolResult, ListSessionsOutput, error) {
		sessions, err := store.List()
		if err != nil {
			return nil, ListSessionsOutput{}, fmt.Errorf("list sessions: %w", err)
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("%d saved session(s):\n", len(sessions)))
		for _, s := range sessions {
			b.WriteString("  - ")
			b.WriteString(s.PrettyPrint())
			b.WriteString("\n")
		}
		if len(sessions) == 0 {
			b.WriteString("  (none)\n")
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimRight(b.String(), "\n")}},
		}, ListSessionsOutput{Sessions: sessions, Count: len(sessions)}, nil
	})
}

// ClearSessionInput is the schema for clear_session.
type ClearSessionInput struct {
	URL string `json:"url" jsonschema:"URL whose host's saved session should be deleted"`
}

// ClearSessionOutput reports what was deleted.
type ClearSessionOutput struct {
	URL     string `json:"url"`
	Domain  string `json:"domain"`
	Cleared bool   `json:"cleared"`
	Message string `json:"message"`
}

// RegisterClearSession wires the clear_session tool to the server.
func RegisterClearSession(s *mcp.Server, store *browser.SessionStore) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "clear_session",
		Description: "Delete the saved authenticated session for the URL's host. Use this when a session has expired or is causing issues.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ClearSessionInput) (*mcp.CallToolResult, ClearSessionOutput, error) {
		if in.URL == "" {
			return nil, ClearSessionOutput{}, fmt.Errorf("url is required")
		}
		existed, err := store.Clear(in.URL)
		if err != nil {
			return nil, ClearSessionOutput{}, err
		}
		key, _ := browser.HostKey(in.URL)
		msg := fmt.Sprintf("cleared session for %s (existed=%v)", key, existed)
		return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: msg}},
			}, ClearSessionOutput{
				URL:     in.URL,
				Domain:  key,
				Cleared: existed,
				Message: msg,
			}, nil
	})
}
