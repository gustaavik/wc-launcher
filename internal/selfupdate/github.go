// Package selfupdate keeps the launcher itself current.
//
// The game is updated through wcauthserver's release broker, because the game
// repository is private and the launcher must never carry a GitHub credential.
// The launcher's own repository is public, so this package talks to GitHub
// directly: no token, no account server, and therefore a launcher that can
// still update itself while signed out or while the account server is down.
//
// wcauth.Client is deliberately not reused. It speaks the account server's
// {"status":"ok","data":...} envelope, which api.github.com does not.
package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Repo is the public repository the launcher publishes its own builds to.
const Repo = "gustaavik/wc-launcher"

const (
	defaultAPI     = "https://api.github.com"
	requestTimeout = 30 * time.Second
	// A release manifest is a few kilobytes. A megabyte is room to spare.
	maxBody = 1 << 20
)

// ErrUnreachable reports that GitHub could not be contacted at all.
//
// The same distinction wcauth draws, for the same reason: a refusal is
// something to report and stop on, an outage is something to try again later.
var ErrUnreachable = errors.New("could not reach GitHub")

// Unreachable reports whether err is an outage rather than a refusal.
func Unreachable(err error) bool { return errors.Is(err, ErrUnreachable) }

// Error is a refusal from GitHub, already phrased for a player.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

// Release is a published launcher build.
type Release struct {
	Tag         string
	Name        string
	Notes       string
	PublishedAt string
	Prerelease  bool
	Assets      []Asset
}

// Asset is one file attached to a release.
type Asset struct {
	Name string
	URL  string
	Size int64
	// Digest is GitHub's "sha256:<hex>", or "" on assets uploaded before it
	// published one. Empty means fall back to the .sha256 sibling.
	Digest string
}

// SHA256 is the bare hex digest, or "" when GitHub published none.
func (a Asset) SHA256() string {
	if rest, ok := strings.CutPrefix(a.Digest, "sha256:"); ok {
		return strings.ToLower(strings.TrimSpace(rest))
	}
	return ""
}

// wire mirrors the subset of GitHub's release JSON this needs.
type wireRelease struct {
	TagName     string      `json:"tag_name"`
	Name        string      `json:"name"`
	Body        string      `json:"body"`
	PublishedAt string      `json:"published_at"`
	Prerelease  bool        `json:"prerelease"`
	Assets      []wireAsset `json:"assets"`
}

type wireAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

// Client fetches launcher releases from GitHub.
type Client struct {
	base string
	http *http.Client
}

// NewClient builds a client. An empty base uses api.github.com; tests pass an
// httptest server.
func NewClient(base string) *Client {
	if base == "" {
		base = defaultAPI
	}
	return &Client{
		base: strings.TrimSuffix(base, "/"),
		http: &http.Client{Timeout: requestTimeout},
	}
}

// Latest is the newest published, non-prerelease launcher build.
//
// GitHub's /releases/latest already excludes prereleases and drafts, so a
// release cut for testing does not reach players until it is promoted.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.base, Repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, fmt.Errorf("build the release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Release{}, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}

	if resp.StatusCode != http.StatusOK {
		return Release{}, statusError(resp.StatusCode)
	}

	var wire wireRelease
	if err := json.Unmarshal(body, &wire); err != nil {
		return Release{}, &Error{Status: resp.StatusCode, Message: "GitHub sent an unreadable reply."}
	}
	if wire.TagName == "" {
		return Release{}, &Error{Status: resp.StatusCode, Message: "GitHub sent a release with no tag."}
	}

	release := Release{
		Tag:         wire.TagName,
		Name:        wire.Name,
		Notes:       wire.Body,
		PublishedAt: wire.PublishedAt,
		Prerelease:  wire.Prerelease,
	}
	for _, asset := range wire.Assets {
		release.Assets = append(release.Assets, Asset{
			Name:   asset.Name,
			URL:    asset.URL,
			Size:   asset.Size,
			Digest: asset.Digest,
		})
	}
	return release, nil
}

// statusError phrases a refusal in terms a player can act on.
func statusError(status int) error {
	switch status {
	case http.StatusNotFound:
		return &Error{Status: status, Message: "No launcher release has been published yet."}
	case http.StatusForbidden, http.StatusTooManyRequests:
		// Unauthenticated GitHub allows 60 requests an hour per address. A
		// launcher checking at startup never approaches that alone, but a
		// shared address can.
		return &Error{Status: status, Message: "GitHub is rate limiting update checks. Try again in a little while."}
	default:
		return &Error{Status: status, Message: fmt.Sprintf("GitHub answered %d when asked for the latest launcher.", status)}
	}
}
