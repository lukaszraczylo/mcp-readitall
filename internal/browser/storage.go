package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// storageState is the on-disk JSON shape for a saved session. It is
// intentionally identical to Playwright's storage state so sessions
// captured by either tool can be loaded by the other.
//
// Cookies and localStorage entries are stored as their raw JSON-encoded
// representation, not as strongly-typed fields, so the file remains
// tolerant of optional / future fields in the upstream format.
type storageState struct {
	Cookies []json.RawMessage `json:"cookies"`
	Origins []json.RawMessage `json:"origins"`
}

// localStorageEntry is the {name, value} pair shape Playwright (and we)
// use to persist per-origin localStorage. Defined at package level so
// buildLocalStorageScript can take a named slice type.
type localStorageEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// localStorageOrigin groups localStorage entries by origin.
type localStorageOrigin struct {
	Origin       string              `json:"origin"`
	LocalStorage []localStorageEntry `json:"localStorage"`
}

// readStorageState parses a storage state file into the in-memory shape.
func readStorageState(path string) (*storageState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var s storageState
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &s, nil
}

// ApplyStorageStateCookies sets every cookie in the storage state on the
// chromedp context. Cookies are domain-bound and can be set without being
// on the target origin, so we apply them pre-navigation.
//
// Exported so the Browser wrapper can call it from NewContext; not part
// of the public API for tool authors.
func ApplyStorageStateCookies(ctx context.Context, statePath string) error {
	state, err := readStorageState(statePath)
	if err != nil {
		return err
	}
	if len(state.Cookies) == 0 {
		return nil
	}

	cookies, err := decodeCookies(state.Cookies)
	if err != nil {
		return fmt.Errorf("decode cookies: %w", err)
	}
	if len(cookies) == 0 {
		return nil
	}

	_, err = chromedp.Call(ctx, network.SetCookies, network.SetCookiesParams{Cookies: cookies})
	return err
}

// ApplyLocalStorageOnCurrentOrigin sets every localStorage entry that
// belongs to the page's current origin. Must be called after the page has
// navigated, because localStorage is origin-scoped.
//
// Entries belonging to other origins are silently skipped: they cannot be
// set from the wrong origin anyway.
//
// Exported so the reader can call it after navigation.
func ApplyLocalStorageOnCurrentOrigin(ctx context.Context, statePath string) error {
	state, err := readStorageState(statePath)
	if err != nil {
		return err
	}
	if len(state.Origins) == 0 {
		return nil
	}

	currentURL, err := chromedp.Run(ctx, chromedp.Location())
	if err != nil {
		return fmt.Errorf("read current origin: %w", err)
	}
	currentOrigin := normalizeOrigin(currentURL)
	if currentOrigin == "" {
		return nil
	}

	var entries []localStorageEntry
	for _, raw := range state.Origins {
		var o localStorageOrigin
		if err := json.Unmarshal(raw, &o); err != nil {
			continue
		}
		if normalizeOrigin(o.Origin) != currentOrigin {
			continue
		}
		entries = append(entries, o.LocalStorage...)
	}
	if len(entries) == 0 {
		return nil
	}

	_, err = chromedp.Run(ctx, chromedp.Evaluate[chromedp.Void](buildLocalStorageScript(entries)))
	return err
}

// CaptureStorageState reads every cookie set on the current target and
// every localStorage entry for the current origin, then marshals them to
// the on-disk JSON shape. It is intended to be called by the login tool
// after a successful interactive authentication.
func CaptureStorageState(ctx context.Context, statePath string) error {
	// 1) Cookies
	cookies, err := captureCDPCookies(ctx)
	if err != nil {
		return fmt.Errorf("get cookies: %w", err)
	}

	// 2) Current origin + localStorage
	currentURL, err := chromedp.Run(ctx, chromedp.Location())
	if err != nil {
		return fmt.Errorf("read location: %w", err)
	}
	origin := normalizeOrigin(currentURL)

	lsRaw, err := captureLocalStorage(ctx, origin)
	if err != nil {
		return fmt.Errorf("read localStorage: %w", err)
	}

	// 3) Build the on-disk shape.
	cookieMsgs := make([]json.RawMessage, 0, len(cookies))
	for _, c := range cookies {
		raw, err := json.Marshal(c)
		if err != nil {
			return fmt.Errorf("encode cookie: %w", err)
		}
		cookieMsgs = append(cookieMsgs, raw)
	}

	out := storageState{Cookies: cookieMsgs}
	if lsRaw != nil {
		out.Origins = []json.RawMessage{lsRaw}
	}

	// 4) Write atomically: tmp + rename, mode 0600.
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp := statePath + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, statePath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// captureCDPCookies asks the current target for its full cookie list via
// the network domain.
func captureCDPCookies(ctx context.Context) ([]*network.Cookie, error) {
	res, err := chromedp.Call(ctx, network.GetCookies, network.GetCookiesParams{})
	if err != nil {
		return nil, err
	}
	return res.Cookies, nil
}

// captureLocalStorage serialises the current page's localStorage to a
// JSON-encoded localStorageOrigin, or returns nil if there is no
// scannable origin / no entries.
func captureLocalStorage(ctx context.Context, origin string) (json.RawMessage, error) {
	if origin == "" {
		return nil, nil
	}
	const script = `JSON.stringify(Object.fromEntries(Object.keys(localStorage).map(function(k){return [k, localStorage.getItem(k)];})))`
	lsJSON, err := chromedp.Run(ctx, chromedp.Evaluate[string](script))
	if err != nil {
		return nil, err
	}
	if lsJSON == "" || lsJSON == "[]" || lsJSON == "{}" {
		return nil, nil
	}
	var kv [][2]string
	if err := json.Unmarshal([]byte(lsJSON), &kv); err != nil {
		return nil, fmt.Errorf("decode localStorage: %w", err)
	}
	if len(kv) == 0 {
		return nil, nil
	}
	entries := make([]localStorageEntry, 0, len(kv))
	for _, pair := range kv {
		entries = append(entries, localStorageEntry{Name: pair[0], Value: pair[1]})
	}
	o := localStorageOrigin{Origin: origin, LocalStorage: entries}
	raw, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// decodeCookies turns the on-disk json.RawMessage cookie list into CDP
// *network.CookieParam values ready for network.SetCookies. Session
// cookies (negative or zero Expires) are left with a nil Expires field,
// which CDP interprets as "session cookie".
func decodeCookies(raws []json.RawMessage) ([]*network.CookieParam, error) {
	out := make([]*network.CookieParam, 0, len(raws))
	for _, raw := range raws {
		var c onDiskCookie
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("cookie %s: %w", string(raw), err)
		}
		out = append(out, onDiskToCookieParam(c))
	}
	return out, nil
}

// onDiskCookie is the subset of cookie fields we care about when
// reading a stored state file. Anything else (priority, source scheme,
// partition keys…) is ignored.
type onDiskCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite"`
}

// onDiskToCookieParam converts our on-disk shape into a CDP
// network.CookieParam. Session cookies (Expires <= 0) keep a zero Expires;
// persistent cookies get a cdp.TimeSinceEpoch built from the Unix timestamp.
func onDiskToCookieParam(c onDiskCookie) *network.CookieParam {
	p := &network.CookieParam{
		Name:     c.Name,
		Value:    c.Value,
		Domain:   c.Domain,
		Path:     c.Path,
		Secure:   c.Secure,
		HTTPOnly: c.HTTPOnly,
		SameSite: parseSameSite(c.SameSite),
	}
	if c.Expires > 0 {
		p.Expires = cdp.TimeSinceEpoch(float64(int64(c.Expires)))
	}
	return p
}

// parseSameSite translates a Playwright-style SameSite string to a CDP
// enum. Unknown / empty values become the safe default (Lax) rather
// than None, to match the previous Playwright behaviour.
func parseSameSite(s string) network.CookieSameSite {
	switch strings.ToLower(s) {
	case "strict":
		return network.CookieSameSiteStrict
	case "lax":
		return network.CookieSameSiteLax
	case "none":
		return network.CookieSameSiteNone
	default:
		return network.CookieSameSiteLax
	}
}

// buildLocalStorageScript returns a JS snippet that, when run inside the
// page, sets every (k, v) pair in the page's localStorage.
func buildLocalStorageScript(entries []localStorageEntry) string {
	var b strings.Builder
	b.WriteString("(function(){")
	for _, e := range entries {
		b.WriteString("localStorage.setItem(")
		b.WriteString(jsonString(e.Name))
		b.WriteString(",")
		b.WriteString(jsonString(e.Value))
		b.WriteString(");")
	}
	b.WriteString("})();")
	return b.String()
}

// jsonString produces a JS-safe double-quoted string literal.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// normalizeOrigin returns the scheme+host[:port] of a URL, or "" if
// parsing fails. Used to match storage state origins against the page's
// current location.
func normalizeOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
