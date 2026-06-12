package tools

import (
	"encoding/json"
	"net/url"
)

// jsQuote produces a JS-safe string literal from a CSS selector. We
// assume selectors do not contain backslashes or newlines (which would
// be invalid CSS) and just JSON-encode the value, which is a valid JS
// double-quoted string.
func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// hostOf extracts the scheme-less host (with port) from a URL string.
// It returns "unknown" for inputs that do not parse to a URL with a
// host, rather than panicking. Case is preserved — url.Parse does not
// normalise the host — so "Example.COM" stays "Example.COM".
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "unknown"
	}
	return u.Host
}
