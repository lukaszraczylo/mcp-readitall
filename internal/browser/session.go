package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionStore persists browser storage state per host. State files live
// under a configurable root directory (default ~/.mcp-readitall/sessions/<host>/
// state.json) and contain cookies + localStorage entries that authenticate
// subsequent browser contexts against the same host. The on-disk JSON shape
// is the same as Playwright's storage state so sessions are interchangeable.
type SessionStore struct {
	root string
}

// SessionInfo describes a persisted session, returned to the MCP client by
// list_sessions.
type SessionInfo struct {
	Domain    string    `json:"domain"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	SizeBytes int64     `json:"size_bytes"`
}

// NewSessionStore creates a store rooted at the default location
// ($HOME/.mcp-readitall/sessions) unless overridden via READITALL_SESSIONS_DIR.
func NewSessionStore() (*SessionStore, error) {
	root := os.Getenv("READITALL_SESSIONS_DIR")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("locate home dir: %w", err)
		}
		root = filepath.Join(home, ".mcp-readitall", "sessions")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create sessions root %q: %w", root, err)
	}
	return &SessionStore{root: root}, nil
}

// Root returns the directory where session files are stored.
func (s *SessionStore) Root() string { return s.root }

// HostKey normalises a URL to a safe directory name. It lower-cases the host
// and replaces any non-alphanumeric characters (except . and -) with _.
func HostKey(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	host := strings.ToLower(u.Host)
	if host == "" {
		return "", fmt.Errorf("url has no host: %q", rawURL)
	}
	// Strip credentials if present.
	if i := strings.Index(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	// Use a separate var to satisfy go vet's "loop variable captured" check
	// even though we only iterate once.
	safe := make([]rune, 0, len(host))
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '.', r == '-':
			safe = append(safe, r)
		default:
			safe = append(safe, '_')
		}
	}
	return string(safe), nil
}

// StatePath returns the path where storage state for the given URL's host is
// (or will be) stored. The file may not exist yet.
func (s *SessionStore) StatePath(rawURL string) (string, error) {
	key, err := HostKey(rawURL)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.root, key, "state.json"), nil
}

// Exists reports whether a session is stored for the given URL's host.
func (s *SessionStore) Exists(rawURL string) (bool, error) {
	path, err := s.StatePath(rawURL)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

// Save writes the JSON-encoded storage state (cookies + origins) to the
// canonical path for the URL's host. The state value is expected to be the
// raw JSON object matching the standard {cookies, origins} storage state
// shape used by Playwright and chromedp.
func (s *SessionStore) Save(rawURL string, stateJSON []byte) (string, error) {
	key, err := HostKey(rawURL)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(s.root, key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create session dir: %w", err)
	}
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, stateJSON, 0o600); err != nil {
		return "", fmt.Errorf("write state: %w", err)
	}
	return path, nil
}

// List returns metadata for every stored session, sorted by host.
func (s *SessionStore) List() ([]SessionInfo, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []SessionInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(s.root, e.Name(), "state.json")
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		out = append(out, SessionInfo{
			Domain:    e.Name(),
			Path:      path,
			UpdatedAt: info.ModTime(),
			SizeBytes: info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}

// Clear removes the saved session for the given host. It returns true when a
// file was deleted, false when nothing existed.
func (s *SessionStore) Clear(rawURL string) (bool, error) {
	key, err := HostKey(rawURL)
	if err != nil {
		return false, err
	}
	dir := filepath.Join(s.root, key)
	// Stat first so we can report whether anything was actually there, then
	// remove. We cannot rely on os.RemoveAll's return value to distinguish
	// "missing" from "removed" — both succeed.
	existed := false
	if _, statErr := os.Stat(dir); statErr == nil {
		existed = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, fmt.Errorf("stat session: %w", statErr)
	}
	if !existed {
		return false, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, fmt.Errorf("remove session: %w", err)
	}
	return true, nil
}

// Close releases any resources held by the store. Currently a no-op kept
// for symmetry with the Browser wrapper.
func (s *SessionStore) Close() error { return nil }

// PrettyPrint returns a human-readable summary of a session, used in tool
// responses.
func (s SessionInfo) PrettyPrint() string {
	age := time.Since(s.UpdatedAt).Truncate(time.Second)
	return fmt.Sprintf("%s (%d bytes, updated %s ago)", s.Domain, s.SizeBytes, age)
}

// storageStateShape is used only to validate that a value looks like a
// Playwright storage state blob before we persist it.
type storageStateShape struct {
	Cookies []json.RawMessage `json:"cookies"`
	Origins []json.RawMessage `json:"origins"`
}

// ValidateStorageState does a cheap structural check on the JSON so we do
// not silently store an empty or malformed blob.
func ValidateStorageState(stateJSON []byte) error {
	var s storageStateShape
	if err := json.Unmarshal(stateJSON, &s); err != nil {
		return fmt.Errorf("storage state is not valid JSON: %w", err)
	}
	if len(s.Cookies) == 0 && len(s.Origins) == 0 {
		return fmt.Errorf("storage state has no cookies or origins; auth likely failed")
	}
	return nil
}

// ValidateStorageStateStruct is the equivalent of ValidateStorageState for
// callers that already hold a parsed value (e.g. a slice of CDP
// *network.Cookie or a Playwright *StorageState). It round-trips through
// JSON and only inspects the cookies / origins fields, so the exact
// concrete type does not matter as long as the JSON shape is correct.
func ValidateStorageStateStruct(state any) error {
	if state == nil {
		return fmt.Errorf("storage state is nil; auth likely failed")
	}
	// We type-switch on the concrete shape because the input may be a
	// third-party type that we cannot import directly; we only care about
	// cookies and origins here.
	type shaped struct {
		Cookies []any `json:"cookies"`
		Origins []any `json:"origins"`
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode storage state: %w", err)
	}
	var s shaped
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("storage state has unexpected shape: %w", err)
	}
	if len(s.Cookies) == 0 && len(s.Origins) == 0 {
		return fmt.Errorf("storage state has no cookies or origins; auth likely failed")
	}
	return nil
}
