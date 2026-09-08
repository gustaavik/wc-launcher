package catalog

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultURL is the object store the release workflow publishes to.
const DefaultURL = "https://s3.wyvencraft.com"

// DefaultPrefix is the bucket and key prefix the game's builds live under:
// bucket "releases", prefix "game/". Everything below it is public to read and
// private to list.
const DefaultPrefix = "releases/game"

// requestTimeout bounds a catalogue fetch. The index is a few tens of
// kilobytes; the archives themselves go through install.Fetch, which has its
// own, far longer, budget.
const requestTimeout = 30 * time.Second

// maxBody caps the index. Thirty releases with their notes inline is well
// under a megabyte, and a proxy's error page is smaller still.
const maxBody = 4 << 20

// ErrUnreachable reports that the object store could not be contacted at all.
//
// The same distinction wcauth and selfupdate both draw, for the same reason: a
// refusal is something to report and stop on, an outage is something to try
// again later — and an outage must never sign anyone out or block Play.
var ErrUnreachable = errors.New("could not reach the download server")

// Unreachable reports whether err is an outage rather than a refusal.
func Unreachable(err error) bool { return errors.Is(err, ErrUnreachable) }

// Error is a refusal: the object store answered, and the answer was no.
//
// Code is S3's error code — "NoSuchBucket" before the bucket exists,
// "AccessDenied" when anonymous read has not been granted. Both are
// deployment mistakes rather than anything a player can act on, so they are
// reported as such rather than folded into "no updates".
type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string {
	switch e.Code {
	case "NoSuchBucket", "NoSuchKey":
		return "no game builds are published yet"
	case "AccessDenied":
		return "the download server refused the request"
	}
	return fmt.Sprintf("the download server answered %d", e.Status)
}

// Client reads the catalogue. Safe for concurrent use.
type Client struct {
	baseURL string
	prefix  string
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
		prefix:  DefaultPrefix,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// BaseURL is the object store this client reads from.
func (c *Client) BaseURL() string { return c.baseURL }

// AssetURL is where one asset can be downloaded from.
//
// A pure function of the base and the asset's bucket-relative path — there is
// no link to broker and nothing to expire, which is why install.Installer no
// longer needs a client at all.
func (c *Client) AssetURL(asset Asset) string {
	return c.baseURL + "/" + asset.Path
}

// Index fetches the catalogue.
//
// An empty catalogue is an error rather than an empty result: a bucket that
// exists but publishes nothing is a deployment mistake, and reporting it as
// "no updates available" would make a broken launcher look up to date.
func (c *Client) Index(ctx context.Context) (Index, error) {
	url := c.baseURL + "/" + c.prefix + "/index.json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Index{}, fmt.Errorf("build the catalogue request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return Index{}, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Index{}, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Index{}, &Error{Status: resp.StatusCode, Code: errorCode(body)}
	}

	var index Index
	if err := json.Unmarshal(body, &index); err != nil {
		return Index{}, fmt.Errorf("the catalogue is not readable: %w", err)
	}
	if err := index.validate(); err != nil {
		return Index{}, err
	}
	return index, nil
}

// errorCode reads S3's <Error><Code> out of a refusal body. Empty when the
// body is not one — a proxy's HTML error page, say.
func errorCode(body []byte) string {
	var payload struct {
		Code string `xml:"Code"`
	}
	if err := xml.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return payload.Code
}

// validate rejects a catalogue this launcher must not act on.
//
// Every check here is one that would otherwise surface as something worse
// later: an install from nowhere, an unverified binary, a download aimed at
// somebody else's host.
func (i Index) validate() error {
	if i.SchemaVersion != SchemaVersion {
		return fmt.Errorf("this launcher does not understand catalogue version %d; update the launcher",
			i.SchemaVersion)
	}
	if len(i.Releases) == 0 {
		return errors.New("no game builds are published yet")
	}
	for _, release := range i.Releases {
		if release.Tag == "" {
			return errors.New("the catalogue lists a release with no tag")
		}
		for _, asset := range release.Assets {
			if err := asset.validate(release.Tag); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a Asset) validate(tag string) error {
	switch {
	case a.Name == "":
		return fmt.Errorf("%s lists an asset with no name", tag)
	case a.SHA256 == "":
		// install.Verify refuses an empty hash anyway; catching it here names
		// the catalogue as the culprit rather than the download.
		return fmt.Errorf("%s publishes no checksum for %s; refusing to install it unverified", tag, a.Name)
	case a.Size < 0:
		return fmt.Errorf("%s lists a negative size for %s", tag, a.Name)
	case a.Path == "":
		return fmt.Errorf("%s lists no path for %s", tag, a.Name)
	case strings.Contains(a.Path, "://"), strings.HasPrefix(a.Path, "/"):
		// The index names paths, never hosts. An absolute URL would let a
		// tampered catalogue point the downloader anywhere it liked.
		return fmt.Errorf("%s lists %s at an absolute location", tag, a.Name)
	case a.Path == "..", strings.HasPrefix(a.Path, "../"), strings.Contains(a.Path, "/../"):
		return fmt.Errorf("%s lists %s outside the catalogue", tag, a.Name)
	}
	return nil
}
