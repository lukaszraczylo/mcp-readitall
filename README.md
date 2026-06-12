# mcp-readitall

A Go-based [Model Context Protocol](https://modelcontextprotocol.io) (MCP)
server that fetches any website — including modern SPAs that need a real
browser to render — and converts the page into clean Markdown. It also
supports per-host authenticated sessions so you can read content that lives
behind a login wall.

The server speaks MCP over **stdio** and is designed to be wired into any
MCP-aware client (Claude Desktop, Claude Code, Cursor, etc.).

## Features

- **Real browser rendering** via [chromedp] (a pure-Go Chrome DevTools
  Protocol client) + system Chromium — handles SPAs, JS hydration,
  lazy-loaded content, client-side routing. No Node.js bridge.
- **Markdown conversion** with the [html-to-markdown] plugin pipeline:
  tables, code blocks (with language detection), strikethrough, commonmark
  semantics, relative-URL resolution.
- **Per-host authenticated sessions** — the `login` tool opens a visible
  browser window for you to authenticate, then persists cookies +
  localStorage so future reads reuse the session automatically.
- **Session lifecycle tools** — `list_sessions` and `clear_session` for
  inspection and revocation.
- **Content extraction** — `extract` and the `selector` parameter on
  `read_url` let you pull just the part of a page you care about.
- **Cross-process safe** — session state is persisted to disk (0600) so the
  server can restart without losing auth.

[html-to-markdown]: https://github.com/JohannesKaufmann/html-to-markdown
[chromedp]: https://github.com/chromedp/chromedp

## Tools

| Tool | Purpose |
| --- | --- |
| `read_url` | Fetch a URL (with optional CSS selector), render JS, return Markdown. |
| `login` | Open a browser, let the operator log in, persist the session. |
| `list_sessions` | Show every host for which a session is saved. |
| `clear_session` | Delete a saved session. |
| `extract` | Read a URL and return only the part under a CSS selector. |

### `read_url` arguments

| Field | Type | Description |
| --- | --- | --- |
| `url` | string (required) | Absolute http(s) URL. |
| `selector` | string | Optional CSS selector to extract a region. |
| `wait_for` | string | Optional CSS selector to wait for (useful for SPAs that hydrate after `networkidle`). |
| `timeout_seconds` | int | Overall timeout. Default 30. |
| `max_chars` | int | Truncate the returned Markdown. Default ~200k. |

### `login` arguments

| Field | Type | Description |
| --- | --- | --- |
| `url` | string (required) | The URL to open (typically the login page). |
| `success_url` | string | Substring of the post-login URL (e.g. `/dashboard`). Polled every second. |
| `success_selector` | string | CSS selector that becomes visible once auth succeeds (e.g. an avatar menu). |
| `timeout_seconds` | int | How long to wait for the operator to complete login. Default 300. |

`success_url` **or** `success_selector` is required so the tool knows when
to stop waiting and save the session.

## Build & install

Requirements:

- Go 1.22+
- A Chromium-family browser on `$PATH` or installed at the standard
  location — `google-chrome`, `chromium`, `chromium-browser`, or Edge.
  macOS: `brew install --cask chromium` (or the standard Chrome install).
  Linux: `apt install chromium` / `dnf install chromium`.

```bash
# Build the server binary
go build -o bin/mcp-readitall ./cmd/readitall
```

> If the project is not under git you may need `-buildvcs=false`:
> `go build -buildvcs=false -o bin/mcp-readitall ./cmd/readitall`

chromedp will auto-discover Chromium via [`exec.LookPath`] and the usual
Chromium env vars (`CHROME_BIN`, `CHROMIUM_BIN`, etc.). If you have a
non-standard install, point at the binary directly:

```bash
CHROME_BIN=/Applications/Chromium.app/Contents/MacOS/Chromium ./bin/mcp-readitall
```

[`exec.LookPath`]: https://pkg.go.dev/os/exec#LookPath

## Wire into an MCP client

### Claude Desktop (`~/Library/Application Support/Claude/claude_desktop_config.json`)

```json
{
  "mcpServers": {
    "readitall": {
      "command": "/absolute/path/to/mcp-readitall/bin/mcp-readitall"
    }
  }
}
```

### Claude Code (`.mcp.json` in your project)

```json
{
  "mcpServers": {
    "readitall": {
      "command": "/absolute/path/to/mcp-readitall/bin/mcp-readitall"
    }
  }
}
```

Restart the client after editing the config.

## Usage flow

### 1. Read a public page

Ask your MCP client:

> "Read https://news.ycombinator.com and summarize the top 5 stories"

Under the hood the model calls `read_url`. The server launches a
headless Chromium, navigates, waits for the body to be ready (plus a
short grace period for hydration, or a `wait_for` selector if supplied),
converts the HTML to Markdown, and returns it.

### 2. Read an authenticated page

> "Log me into GitHub, then read my notifications"

The model first calls:

```json
{
  "name": "login",
  "arguments": {
    "url": "https://github.com/login",
    "success_url": "/",
    "timeout_seconds": 300
  }
}
```

A real browser window pops up on your machine. Log in normally. When the
URL changes to contain `/`, the tool saves the session and closes the
window. Subsequent calls to `read_url` for `github.com` will reuse the
saved cookies + localStorage automatically.

To invalidate a session:

> "Clear my session for github.com"

The model calls `clear_session`.

## Session storage

Saved sessions live in:

- `~/.mcp-readitall/sessions/<host>/state.json` by default
- Override with `READITALL_SESSIONS_DIR=/some/path`

State files are written with mode `0600` (owner read/write only) and
use Playwright's standard storage state shape (`{cookies:[…], origins:[{origin,
localStorage:[…]}]}`) — sessions captured by Playwright can be loaded by
us, and vice versa. Treat them as secrets — they grant access to your
authenticated sessions.

## Tests

```bash
go test -race ./...
```

The suite is hermetic — no browser is launched. It covers:

- Hostname normalization (good / bad / edge).
- Storage-state validation.
- Session-store CRUD and concurrency safety.
- HTML→Markdown conversion (headings, lists, tables, code, entities, URL
  resolution, security-sensitive stripping of `<script>` / `<style>` /
  comments).
- Tool helpers (URL host extraction, comma-separated selector parsing,
  output formatter).

## Limitations

- The `login` tool needs a **visible** browser window. Running it on a
  remote / headless host with no display (and no VNC / X-forwarding) will
  not work — Chrome exits immediately when it has no window to draw to.
- Requires a Chromium-family browser on the host. `chromedp` does not
  bundle one.
- The reader is single-tab per call. Reading multiple pages in parallel
  inside one tool call is not supported yet.
- We treat `networkidle` as "body is ready + 500 ms grace" (or whatever
  you pass in `wait_for`). Sites that keep firing background requests
  indefinitely may need a `wait_for` selector rather than rely on
  automatic settling.

## License

MIT.
