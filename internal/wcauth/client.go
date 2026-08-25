// Package wcauth talks to the Wyvencraft account server.
//
// It covers exactly what a launcher needs: sign in, keep the session alive,
// sign out, fetch the ticket-verification keys the game caches, and ask which
// game build is current. Registration is deliberately absent — accounts are
// created elsewhere.
//
// Every response uses the server's envelope, tagged on "status". Requests that
// never reach a handler do not: axum's own rejections (400, 408, 413, 415, 422)
// return plain text, so decoding tolerates a non-envelope body.
package wcauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultURL is the deployment the shipped game is built against — the game's
// release workflow bakes this same value in as WYVEN_AUTH_URL. Not localhost:
// that is a development default, and a player has no auth server of their own.
const DefaultURL = "http://llzdmervhd2eyewlrapa8jhi.100.94.237.98.sslip.io"

// requestTimeout bounds any single call. The server's own timeout layer cuts
// requests off at 30s; there is no point waiting longer than it will answer.
const requestTimeout = 30 * time.Second

// Client is a wcauthserver client. Safe for concurrent use.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a client for baseURL. An empty baseURL means [DefaultURL].
func New(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultURL
	}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// BaseURL is the server this client talks to.
func (c *Client) BaseURL() string { return c.baseURL }

// ---------------------------------------------------------------- errors

// Error is a refusal from the server: it answered, and the answer was no.
//
// Code is the stable contract ("invalid_credentials", "session_invalid", ...);
// Message is human-facing and may be reworded upstream, so branch on Code.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("the server answered %d (%s)", e.Status, e.Code)
}

// SessionExpired reports whether the refresh token is dead and the player must
// sign in again. Distinct from any other failure: retrying will not help, and
// the stored token should be discarded.
func (e *Error) SessionExpired() bool {
	return e.Code == "session_invalid"
}

// ErrUnreachable wraps a transport failure — no server answered.
//
// Kept apart from [Error] because the two call for opposite responses: a
// refusal means discard the session, an outage means keep it and try later.
var ErrUnreachable = errors.New("could not reach the account server")

// Unreachable reports whether err was a transport failure rather than a
// refusal.
func Unreachable(err error) bool { return errors.Is(err, ErrUnreachable) }

// ---------------------------------------------------------------- shapes

// Account is the server's view of a player.
type Account struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
	// NetcodeID is a u64 sent as a string — JSON numbers are doubles.
	NetcodeID string `json:"netcode_id"`
}

// Session is a signed-in session: an access token to use now, and a refresh
// token to get the next one with.
type Session struct {
	Account          Account `json:"account"`
	AccessToken      string  `json:"access_token"`
	AccessExpiresAt  string  `json:"access_expires_at"`
	RefreshToken     string  `json:"refresh_token"`
	RefreshExpiresAt string  `json:"refresh_expires_at"`
}

// AccessExpiry parses AccessExpiresAt. A zero time means it could not be read,
// which callers should treat as "expired" rather than "never expires".
func (s Session) AccessExpiry() time.Time {
	at, err := time.Parse(time.RFC3339, s.AccessExpiresAt)
	if err != nil {
		return time.Time{}
	}
	return at
}

// Key is one public key a host verifies join tickets with.
type Key struct {
	ID int `json:"id"`
	// PublicKey is the raw 32-byte Ed25519 key in standard padded base64 —
	// not SPKI, not PEM. It goes into authkeys.toml exactly as it arrives.
	PublicKey string `json:"public_key"`
	Active    bool   `json:"active"`
}

type keysResponse struct {
	Keys []Key `json:"keys"`
}

// Health is what /healthz reports. Used for feature detection.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	// UpdatesEnabled reports whether this server brokers game downloads. When
	// false, the launcher can say so instead of provoking a 501.
	UpdatesEnabled bool `json:"updates_enabled"`
	OAuthEnabled   bool `json:"oauth_enabled"`
}

// Release is a published game build.
type Release struct {
	Tag         string  `json:"tag"`
	Name        string  `json:"name"`
	Notes       string  `json:"notes"`
	PublishedAt string  `json:"published_at"`
	Prerelease  bool    `json:"prerelease"`
	Assets      []Asset `json:"assets"`
}

// Asset is one downloadable file attached to a [Release].
type Asset struct {
	// ID and Size are u64 sent as strings, for the same reason as NetcodeID.
	ID   string `json:"id"`
	Name string `json:"name"`
	Size string `json:"size"`
	// Digest is "sha256:<hex>", or empty when upstream published none.
	Digest string `json:"digest"`
}

// SizeBytes parses Size. Zero when absent or unreadable, which callers should
// treat as "unknown" rather than "empty file".
func (a Asset) SizeBytes() int64 {
	n, err := strconv.ParseInt(a.Size, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// SHA256 is the expected content hash as lowercase hex, or "" if the release
// did not publish one.
func (a Asset) SHA256() string {
	return strings.ToLower(strings.TrimPrefix(a.Digest, "sha256:"))
}

type releaseListResponse struct {
	Releases []Release `json:"releases"`
}

type downloadResponse struct {
	URL string `json:"url"`
}

// ---------------------------------------------------------------- calls

// Health reports what the server supports. Unauthenticated.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var out Health
	err := c.do(ctx, http.MethodGet, "/healthz", "", nil, &out)
	return out, err
}

// Login exchanges credentials for a session.
func (c *Client) Login(ctx context.Context, username, password string) (Session, error) {
	body := map[string]string{"username": username, "password": password}
	var out Session
	err := c.do(ctx, http.MethodPost, "/api/v1/auth/login", "", body, &out)
	return out, err
}

// Refresh trades a refresh token for a new session.
//
// Rotation is destructive and single-use: on return the token passed in is
// dead, and the caller must persist the new pair before doing anything else
// with it. Presenting a spent token revokes every session for the account.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	body := map[string]string{"refresh_token": refreshToken}
	var out Session
	err := c.do(ctx, http.MethodPost, "/api/v1/auth/refresh", "", body, &out)
	return out, err
}

// Logout revokes the whole token family. Always succeeds server-side, even for
// a token it has never seen.
func (c *Client) Logout(ctx context.Context, refreshToken string) error {
	body := map[string]string{"refresh_token": refreshToken}
	return c.do(ctx, http.MethodPost, "/api/v1/auth/logout", "", body, nil)
}

// Keys fetches the public keys a host verifies join tickets with.
// Unauthenticated by design: these are public keys.
func (c *Client) Keys(ctx context.Context) ([]Key, error) {
	var out keysResponse
	err := c.do(ctx, http.MethodGet, "/api/v1/keys", "", nil, &out)
	return out.Keys, err
}

// LatestRelease asks which game build is current.
func (c *Client) LatestRelease(ctx context.Context, accessToken string) (Release, error) {
	var out Release
	err := c.do(ctx, http.MethodGet, "/api/v1/releases/latest", accessToken, nil, &out)
	return out, err
}

// Releases lists every published build, newest first.
//
// The version picker's source, and how a profile pinned to an older tag finds
// the assets to install with. A launcher that only ever installs the newest
// build does not need this; one that lets a player pin does.
//
// Unlike LatestRelease, this includes prereleases: /releases/latest follows the
// repository's stable-release pointer, so the newest entry here is occasionally
// newer than that. Callers show the Prerelease flag rather than hiding them.
func (c *Client) Releases(ctx context.Context, accessToken string) ([]Release, error) {
	var out releaseListResponse
	err := c.do(ctx, http.MethodGet, "/api/v1/releases", accessToken, nil, &out)
	return out.Releases, err
}

// DownloadURL resolves where one asset can be fetched from.
//
// The URL is short-lived and carries its own authorization. Use it at once and
// never store it; if a download fails partway, ask again rather than retrying
// the old one.
func (c *Client) DownloadURL(ctx context.Context, accessToken, assetID string) (string, error) {
	var out downloadResponse
	path := "/api/v1/releases/assets/" + assetID + "/download"
	err := c.do(ctx, http.MethodGet, path, accessToken, nil, &out)
	return out.URL, err
}

// ---------------------------------------------------------------- plumbing

// envelope is the server's response wrapper, tagged on "status".
type envelope struct {
	Status  string          `json:"status"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
	Code    string          `json:"code"`
}

// maxBody caps what will be read from a response.
//
// Sized for the release list, which is the only large body here: thirty
// releases (the server's ListReleases::LIMIT), each carrying its own Markdown
// notes. Everything else is a small JSON object.
const maxBody = 4 << 20

func (c *Client) do(ctx context.Context, method, path, bearer string, in, out any) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// Includes DNS failures, refused connections and timeouts — every case
		// where keeping the stored session is the right call.
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}

	var env envelope
	if jsonErr := json.Unmarshal(raw, &env); jsonErr != nil || env.Status == "" {
		// Not the envelope. Either an axum rejection (plain text) or a proxy
		// answering for the server. Report it as a refusal keyed on the status,
		// since there is no code to read.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return fmt.Errorf("the server sent an unreadable reply: %s", snippet(raw))
		}
		return &Error{
			Status:  resp.StatusCode,
			Code:    "unexpected_response",
			Message: fallbackMessage(resp.StatusCode, raw),
		}
	}

	if env.Status != "ok" {
		return &Error{Status: resp.StatusCode, Code: env.Code, Message: env.Message}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("the server sent an unreadable reply: %w", err)
	}
	return nil
}

// fallbackMessage turns a non-envelope failure into something worth showing.
func fallbackMessage(status int, raw []byte) string {
	switch status {
	case http.StatusNotFound:
		return "this server does not offer that — is the launcher pointed at the right address?"
	case http.StatusRequestTimeout:
		return "the account server took too long to answer"
	case http.StatusNotImplemented:
		return "this server does not offer game downloads"
	}
	if status >= 500 {
		return "the account server is having trouble; try again shortly"
	}
	if text := snippet(raw); text != "" {
		return text
	}
	return fmt.Sprintf("the server answered %d", status)
}

// snippet trims a body down to something loggable and showable.
func snippet(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 200 {
		text = text[:200] + "…"
	}
	return text
}
