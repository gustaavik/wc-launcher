package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gustaavik/wc-launcher/internal/paths"
	"github.com/gustaavik/wc-launcher/internal/wcauth"
)

// keptVersions is how many installs survive a prune: the current one and the
// one before it, so a bad update can be rolled back by hand.
const keptVersions = 2

// State records what is installed. Persisted as installed.json.
type State struct {
	// Tag of the installed build, or "" when nothing is installed.
	Tag string `json:"tag"`
}

// Installer downloads and unpacks game builds.
type Installer struct {
	layout paths.Layout
	client *wcauth.Client

	// mu serialises installs. Two at once would race on the same temp paths,
	// and there is no reason to allow it.
	mu sync.Mutex
}

func New(layout paths.Layout, client *wcauth.Client) *Installer {
	return &Installer{layout: layout, client: client}
}

// State reads what is installed. A missing or unreadable file means nothing is.
func (i *Installer) State() State {
	raw, err := os.ReadFile(i.layout.StateFile())
	if err != nil {
		return State{}
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return State{}
	}
	// Trust the directory over the record: a file saying v1 is installed when
	// the directory is gone would make Play spawn a missing binary.
	if state.Tag != "" && !i.Installed(state.Tag) {
		return State{}
	}
	return state
}

// Installed reports whether tag is unpacked and has a game binary.
func (i *Installer) Installed(tag string) bool {
	if tag == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(i.layout.VersionDir(tag), GameBinary()))
	return err == nil
}

// Install downloads, verifies and unpacks a release.
//
// Nothing is visible under versions/<tag> until the whole thing has succeeded:
// the unpack goes to a temp directory and is renamed into place at the end. A
// failure part-way leaves the previous install untouched and playable.
func (i *Installer) Install(ctx context.Context, accessToken string, release wcauth.Release, report ProgressFunc) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	asset, err := SelectAsset(release)
	if err != nil {
		return err
	}

	wantHash, err := i.expectedHash(ctx, accessToken, release, asset)
	if err != nil {
		return err
	}

	// Kept between attempts so an interrupted download can resume.
	partial := filepath.Join(i.layout.Versions, ".download-"+asset.Name+".part")

	url, err := i.client.DownloadURL(ctx, accessToken, asset.ID)
	if err != nil {
		return fmt.Errorf("could not get a download link: %w", err)
	}
	if err := download(ctx, url, partial, asset.SizeBytes(), report); err != nil {
		return err
	}

	if err := verify(partial, wantHash, report); err != nil {
		// A mismatched file will never verify, so keeping it would make every
		// later attempt resume onto corrupt bytes.
		os.Remove(partial)
		return err
	}

	if report != nil {
		report(Progress{Phase: "extracting", Percent: -1})
	}

	staging := filepath.Join(i.layout.Versions, ".staging-"+filepath.Base(i.layout.VersionDir(release.Tag)))
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("clear %s: %w", staging, err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", staging, err)
	}
	defer os.RemoveAll(staging)

	if err := Extract(partial, staging); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(staging, GameBinary())); err != nil {
		return fmt.Errorf("%s contains no %s", asset.Name, GameBinary())
	}

	final := i.layout.VersionDir(release.Tag)
	if err := os.RemoveAll(final); err != nil {
		return fmt.Errorf("replace %s: %w", final, err)
	}
	if err := os.Rename(staging, final); err != nil {
		return fmt.Errorf("move the new build into place: %w", err)
	}

	os.Remove(partial)

	if err := i.setState(State{Tag: release.Tag}); err != nil {
		return err
	}
	i.prune(release.Tag)

	if report != nil {
		report(Progress{Phase: "done", Percent: 100})
	}
	return nil
}

// expectedHash finds the SHA-256 to check the download against: the digest
// published with the asset, or failing that the `.sha256` sibling.
func (i *Installer) expectedHash(ctx context.Context, accessToken string, release wcauth.Release, asset wcauth.Asset) (string, error) {
	if hash := asset.SHA256(); hash != "" {
		return hash, nil
	}

	sibling, ok := ChecksumAsset(release, asset)
	if !ok {
		return "", fmt.Errorf("%s publishes no checksum for %s; refusing to install it unverified",
			release.Tag, asset.Name)
	}

	url, err := i.client.DownloadURL(ctx, accessToken, sibling.ID)
	if err != nil {
		return "", fmt.Errorf("could not fetch the checksum: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build checksum request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch the checksum: %w", err)
	}
	defer resp.Body.Close()

	// A checksum line is under 128 bytes; anything larger is not one.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("read the checksum: %w", err)
	}
	hash := parseChecksumFile(string(body))
	if hash == "" {
		return "", fmt.Errorf("could not read a checksum for %s", asset.Name)
	}
	return hash, nil
}

func (i *Installer) setState(state State) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode install state: %w", err)
	}
	if err := os.WriteFile(i.layout.StateFile(), raw, 0o644); err != nil {
		return fmt.Errorf("write install state: %w", err)
	}
	return nil
}

// prune deletes old installs, keeping the current one and the most recent
// other. Builds are tens of megabytes and there is no reason to hoard them,
// but keeping one spare makes a bad update recoverable by hand.
func (i *Installer) prune(current string) {
	entries, err := os.ReadDir(i.layout.Versions)
	if err != nil {
		return
	}

	var others []os.DirEntry
	currentDir := filepath.Base(i.layout.VersionDir(current))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == currentDir || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		others = append(others, entry)
	}

	// Newest first, so the oldest are the ones dropped.
	sort.Slice(others, func(a, b int) bool {
		ia, errA := others[a].Info()
		ib, errB := others[b].Info()
		if errA != nil || errB != nil {
			return others[a].Name() > others[b].Name()
		}
		return ia.ModTime().After(ib.ModTime())
	})

	for idx, entry := range others {
		if idx < keptVersions-1 {
			continue
		}
		_ = os.RemoveAll(filepath.Join(i.layout.Versions, entry.Name()))
	}
}
