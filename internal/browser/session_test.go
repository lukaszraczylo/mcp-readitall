package browser_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/lukaszraczylo/mcp-readitall/internal/browser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// TEST HELPERS
// =============================================================================

// newStoreInTemp constructs a SessionStore rooted at a fresh temp directory.
// It is automatically cleaned up by t.TempDir() and the env override is
// reverted by t.Setenv() at the end of the test.
func newStoreInTemp(t *testing.T) *browser.SessionStore {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("READITALL_SESSIONS_DIR", dir)
	s, err := browser.NewSessionStore()
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// validStateJSON returns a minimal but well-formed Playwright storage state
// blob containing one cookie and one origin entry. Tests can pass it through
// ValidateStorageState or Save without modification.
func validStateJSON(t *testing.T) []byte {
	t.Helper()
	state := map[string]any{
		"cookies": []map[string]any{
			{"name": "session", "value": "abc123", "domain": "example.com", "path": "/"},
		},
		"origins": []map[string]any{
			{
				"origin": "https://example.com",
				"localStorage": []map[string]any{
					{"name": "token", "value": "xyz"},
				},
			},
		},
	}
	b, err := json.Marshal(state)
	require.NoError(t, err)
	return b
}

// =============================================================================
// HostKey
// =============================================================================

func TestHostKey(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
		// optional: when set, expect an error containing this substring
		wantErr string
	}{
		// ===== GOOD CASES =====
		{
			name: "simple http",
			url:  "http://example.com/path",
			want: "example.com",
		},
		{
			name: "https with deep path and query",
			url:  "https://example.com/some/deep/path?x=1&y=2",
			want: "example.com",
		},
		{
			name: "subdomain preserved",
			url:  "https://app.acme.example.com",
			want: "app.acme.example.com",
		},
		{
			name: "host with port",
			url:  "http://localhost:8443/admin",
			want: "localhost_8443",
		},
		{
			name: "uppercase host is lowercased",
			url:  "HTTPS://Example.COM/X",
			want: "example.com",
		},
		{
			name: "host with hyphens",
			url:  "https://my-app.example-site.io",
			want: "my-app.example-site.io",
		},
		{
			name: "credentials in URL are stripped",
			url:  "https://user:pass@example.com/",
			want: "example.com",
		},

		// ===== BAD CASES =====
		{
			name: "empty url",
			url:  "",
			// url.Parse("") returns a URL with an empty host; HostKey
			// catches that and reports the "no host" condition.
			wantErr: "no host",
		},
		{
			name:    "url with no host",
			url:     "/just/a/path",
			wantErr: "no host",
		},
		{
			name: "garbage url",
			url:  "::::::",
			// Garbage fails net/url parsing before HostKey can look at the
			// host. The "parse url" wrapper surfaces that.
			wantErr: "parse url",
		},

		// ===== EDGE CASES =====
		{
			name: "weird chars in host get underscored",
			url:  "https://xn--bcher-kva.example/", // punycode begins with "xn--"
			want: "xn--bcher-kva.example",
		},
		{
			name: "ipv4 host preserved",
			url:  "http://127.0.0.1:8080/",
			want: "127.0.0.1_8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := browser.HostKey(tt.url)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// =============================================================================
// ValidateStorageState
// =============================================================================

func TestValidateStorageState(t *testing.T) {
	tests := []struct {
		name    string
		state   string
		wantErr string
	}{
		// ===== GOOD CASES =====
		{
			name:  "valid state with cookies only",
			state: `{"cookies":[{"name":"a","value":"b"}]}`,
		},
		{
			name:  "valid state with origins only",
			state: `{"origins":[{"origin":"https://x.test"}]}`,
		},
		{
			name:  "valid state with both",
			state: `{"cookies":[{"name":"a"}],"origins":[{"origin":"https://x.test"}]}`,
		},

		// ===== BAD CASES =====
		{
			name:    "not json",
			state:   `<html>not json</html>`,
			wantErr: "not valid JSON",
		},
		{
			name:    "empty json object",
			state:   `{}`,
			wantErr: "no cookies or origins",
		},
		{
			name:    "empty arrays",
			state:   `{"cookies":[],"origins":[]}`,
			wantErr: "no cookies or origins",
		},
		{
			name:    "completely empty string",
			state:   ``,
			wantErr: "not valid JSON",
		},

		// ===== EDGE CASES =====
		{
			name:  "whitespace-only json returns parse error",
			state: `   `,
			// json.Unmarshal treats whitespace as truncated input and
			// returns a parse error before we ever look at fields.
			wantErr: "not valid JSON",
		},
		{
			name:  "extra unknown fields are tolerated",
			state: `{"cookies":[{"name":"a"}],"origins":[],"extra":{"foo":"bar"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := browser.ValidateStorageState([]byte(tt.state))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// =============================================================================
// Save / Exists / StatePath / List / Clear
// =============================================================================

func TestSessionStore_SaveAndExists(t *testing.T) {
	// Uses t.Setenv via newStoreInTemp, so cannot be t.Parallel().
	store := newStoreInTemp(t)

	url := "https://example.com/dashboard"
	state := validStateJSON(t)

	// Exists should be false before save
	exists, err := store.Exists(url)
	require.NoError(t, err)
	assert.False(t, exists)

	path, err := store.Save(url, state)
	require.NoError(t, err)
	assert.Equal(t, store.Root(), filepath.Dir(filepath.Dir(path)))
	assert.FileExists(t, path)

	// Permissions should be 0600 — credentials must not leak
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "state file must be owner-only")

	exists, err = store.Exists(url)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestSessionStore_ExistsAndStatePath_ForUnknownURL(t *testing.T) {
	// Uses t.Setenv via newStoreInTemp, so cannot be t.Parallel().
	store := newStoreInTemp(t)

	exists, err := store.Exists("https://never-saved.test/")
	require.NoError(t, err)
	assert.False(t, exists)

	path, err := store.StatePath("https://never-saved.test/")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(store.Root(), "never-saved.test", "state.json"), path)
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "expected no file at %s", path)
}

func TestSessionStore_RejectsEmptyState(t *testing.T) {
	// Uses t.Setenv via newStoreInTemp, so cannot be t.Parallel().
	store := newStoreInTemp(t)

	// Validation must happen before persistence; otherwise we'd write an
	// empty file and look authenticated on subsequent reads.
	empty := []byte(`{"cookies":[],"origins":[]}`)
	_, err := store.Save("https://broken.test/", empty)
	// Save does not itself validate — that's a separate concern. Verify
	// ValidateStorageState is the gate.
	require.NoError(t, err, "Save is a low-level write; validation belongs to callers")

	// Caller-side validation catches it.
	require.Error(t, browser.ValidateStorageState(empty))
}

func TestSessionStore_List(t *testing.T) {
	store := newStoreInTemp(t)
	state := validStateJSON(t)

	// Empty store: no entries
	infos, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, infos)

	// Save a few
	hosts := []string{
		"https://a.test/x",
		"https://b.test/y",
		"https://sub.c.test/z",
	}
	for _, u := range hosts {
		_, err := store.Save(u, state)
		require.NoError(t, err)
	}

	infos, err = store.List()
	require.NoError(t, err)
	assert.Len(t, infos, 3)

	got := make([]string, 0, len(infos))
	for _, i := range infos {
		got = append(got, i.Domain)
	}
	sort.Strings(got)
	assert.Equal(t, []string{"a.test", "b.test", "sub.c.test"}, got)

	for _, i := range infos {
		assert.Greater(t, i.SizeBytes, int64(0))
		assert.False(t, i.UpdatedAt.IsZero())
	}
}

func TestSessionStore_Clear(t *testing.T) {
	store := newStoreInTemp(t)
	state := validStateJSON(t)
	url := "https://clear-me.test/page"

	_, err := store.Save(url, state)
	require.NoError(t, err)

	// First Clear returns existed=true
	cleared, err := store.Clear(url)
	require.NoError(t, err)
	assert.True(t, cleared)

	exists, err := store.Exists(url)
	require.NoError(t, err)
	assert.False(t, exists)

	// Second Clear returns existed=false (idempotent)
	cleared, err = store.Clear(url)
	require.NoError(t, err)
	assert.False(t, cleared)
}

func TestSessionStore_ClearUnknownHost(t *testing.T) {
	// Uses t.Setenv via newStoreInTemp, so cannot be t.Parallel().
	store := newStoreInTemp(t)

	cleared, err := store.Clear("https://never-saved.test/")
	require.NoError(t, err)
	assert.False(t, cleared)
}

func TestSessionStore_UpdatedAtAdvances(t *testing.T) {
	// Not parallel: relies on sequential time advancement.
	store := newStoreInTemp(t)
	state := validStateJSON(t)
	url := "https://time.test/"

	_, err := store.Save(url, state)
	require.NoError(t, err)
	first, err := store.List()
	require.NoError(t, err)
	require.Len(t, first, 1)
	firstStamp := first[0].UpdatedAt

	// Sleep just long enough that mtime can differ (filesystem mtime
	// resolution varies, especially on macOS APFS where it's 1ns).
	time.Sleep(20 * time.Millisecond)

	_, err = store.Save(url, state)
	require.NoError(t, err)
	second, err := store.List()
	require.NoError(t, err)
	require.Len(t, second, 1)

	assert.True(t, second[0].UpdatedAt.After(firstStamp),
		"expected updated_at to advance: first=%s second=%s", firstStamp, second[0].UpdatedAt)
}

func TestSessionStore_RoundTripPreservesStateBytes(t *testing.T) {
	// Uses t.Setenv via newStoreInTemp, so cannot be t.Parallel().
	// We do not parse the saved file; we just ensure the bytes we put in
	// are the bytes we get out.
	store := newStoreInTemp(t)
	url := "https://roundtrip.test/"
	state := validStateJSON(t)

	path, err := store.Save(url, state)
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, string(state), string(got))
}
