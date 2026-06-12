// White-box tests for unexported helpers in the tools package. We use
// `package tools` (not `tools_test`) so we can reach readText, firstSelector,
// and hostOf directly. These helpers are the bits of logic that don't
// require a browser or MCP plumbing, so they're cheap to cover.
package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// readText
// =============================================================================

func TestReadText(t *testing.T) {
	tests := []struct {
		name     string
		in       ReadOutput
		mustHave []string
		mustNot  []string
	}{
		// ===== GOOD CASES =====
		{
			name: "title and body",
			in: ReadOutput{
				URL:      "https://example.test/",
				Title:    "Example Domain",
				Markdown: "Hello **world**.",
			},
			mustHave: []string{
				"# Example Domain",
				"Hello **world**.",
			},
		},
		{
			name: "falls back to URL when title empty",
			in: ReadOutput{
				URL:      "https://example.test/",
				Markdown: "body",
			},
			mustHave: []string{
				"# https://example.test/",
				"body",
			},
		},
		{
			name: "truncated flag is surfaced",
			in: ReadOutput{
				URL:       "https://x.test/",
				Title:     "T",
				Markdown:  "cut",
				Truncated: true,
			},
			mustHave: []string{
				"# T",
				"cut",
				"[truncated]",
			},
		},
		{
			name: "redirect is shown only when final URL differs",
			in: ReadOutput{
				URL:      "https://x.test/old",
				FinalURL: "https://x.test/new",
				Title:    "T",
				Markdown: "body",
			},
			mustHave: []string{
				"_Resolved to: https://x.test/new_",
			},
		},
		{
			name: "no redirect note when URLs match",
			in: ReadOutput{
				URL:      "https://x.test/",
				FinalURL: "https://x.test/",
				Title:    "T",
				Markdown: "body",
			},
			mustNot: []string{
				"Resolved to",
			},
		},
		{
			name: "session-used flag is surfaced",
			in: ReadOutput{
				URL:         "https://x.test/",
				Title:       "T",
				Markdown:    "body",
				UsedSession: true,
			},
			mustHave: []string{
				"_Used saved session._",
			},
		},

		// ===== EDGE CASES =====
		{
			name: "empty body still renders header",
			in: ReadOutput{
				URL:   "https://x.test/",
				Title: "T",
			},
			mustHave: []string{
				"# T",
			},
		},
		{
			name: "redirect + session flags combined",
			in: ReadOutput{
				URL:         "https://x.test/old",
				FinalURL:    "https://x.test/new",
				Title:       "T",
				Markdown:    "body",
				UsedSession: true,
				Truncated:   true,
			},
			mustHave: []string{
				"_Resolved to: https://x.test/new_",
				"_Used saved session._",
				"[truncated]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := readText(tt.in)
			for _, s := range tt.mustHave {
				assert.Contains(t, got, s, "missing %q in:\n%s", s, got)
			}
			for _, s := range tt.mustNot {
				assert.NotContains(t, got, s, "unexpected %q in:\n%s", s, got)
			}
		})
	}
}

// =============================================================================
// firstSelector
// =============================================================================

func TestFirstSelector(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// ===== GOOD CASES =====
		{
			name: "single selector",
			in:   "main",
			want: "main",
		},
		{
			name: "comma list returns first",
			in:   "main, article, [role=main]",
			want: "main",
		},
		{
			name: "whitespace around first entry is trimmed",
			in:   "  #content  , aside",
			want: "#content",
		},

		// ===== EDGE CASES =====
		{
			name: "trailing comma",
			in:   "main,",
			want: "main",
		},
		{
			name: "leading comma",
			in:   ",main",
			want: "main",
		},
		{
			name: "multiple consecutive commas",
			in:   ",,main,,article",
			want: "main",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
		{
			name: "only commas",
			in:   ",,,",
			want: "",
		},
		{
			name: "selector with internal spaces is preserved",
			in:   "div[data-role=main content], aside",
			want: "div[data-role=main content]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := firstSelector(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// hostOf
// =============================================================================

func TestHostOf(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// ===== GOOD CASES =====
		{
			name: "https with path",
			in:   "https://example.com/some/path",
			want: "example.com",
		},
		{
			name: "subdomain",
			in:   "https://app.example.co.uk",
			want: "app.example.co.uk",
		},
		{
			name: "with port",
			in:   "http://localhost:8080/x",
			want: "localhost:8080",
		},
		{
			name: "uppercase host is preserved (url.Parse does not lowercase)",
			in:   "https://Example.COM/x",
			want: "Example.COM",
		},

		// ===== BAD / EDGE CASES =====
		{
			name: "empty string returns unknown",
			in:   "",
			want: "unknown",
		},
		{
			name: "garbage returns unknown (does not panic)",
			in:   "::::",
			want: "unknown",
		},
		{
			name: "scheme-only returns unknown",
			in:   "https://",
			want: "unknown",
		},
		{
			name: "path-only returns unknown",
			in:   "/just/a/path",
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hostOf(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}
