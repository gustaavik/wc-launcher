package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gustaavik/wc-launcher/internal/catalog"
	"github.com/gustaavik/wc-launcher/internal/paths"
)

// keptVersions is how many builds beyond the pinned ones survive a prune: the
// newest and the one before it, so an update that turns out badly can still be
// rolled back by hand.
const keptVersions = 2

// tagMarker records the exact release tag a build came from.
//
// VersionDir sanitises a tag into a directory name, and that is deliberately
// lossy — a tag is a remote string and a directory name has to be safe. So the
// tag cannot be read back out of the path, and is written down beside the build
// instead. Colocated rather than kept in a second index file: a build deleted
// by hand takes its own record with it, and there is nothing left to fall out
// of sync.
const tagMarker = ".wyvencraft-tag"

// Build is one unpacked game build on disk.
type Build struct {
	// Tag is the release this build came from.
	Tag string `json:"tag"`
	// InstalledAt is when it was moved into place.
	InstalledAt time.Time `json:"installedAt"`
}

// Installer downloads and unpacks game builds.
//
// It holds no client. An asset's URL is a pure function of the catalogue's base
// and the asset's own path, so there is no link to broker and nothing to
// expire — the caller passes the URL in. That is the whole of what this package
// used to need the account server for.
type Installer struct {
	layout paths.Layout

	// mu serialises installs. Two at once would race on the same temp paths,
	// and there is no reason to allow it.
	mu sync.Mutex
}

func New(layout paths.Layout) *Installer {
	return &Installer{layout: layout}
}

// List reports every unpacked, playable build, newest first.
//
// The directory *is* the record. There is no index file to go stale, and a
// build removed by hand simply stops being listed.
//
// Never fails: an unreadable versions/ means "nothing is installed", which is
// the same answer a first run gives and one every caller already handles.
func (i *Installer) List() []Build {
	entries, err := os.ReadDir(i.layout.Versions)
	if err != nil {
		return nil
	}

	builds := make([]Build, 0, len(entries))
	for _, entry := range entries {
		// Staging directories and partial downloads both start with a dot.
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dir := filepath.Join(i.layout.Versions, entry.Name())
		if _, err := os.Stat(filepath.Join(dir, GameBinary())); err != nil {
			continue
		}

		build := Build{Tag: readTag(dir, entry.Name())}
		if info, err := entry.Info(); err == nil {
			build.InstalledAt = info.ModTime()
		}
		builds = append(builds, build)
	}

	// Newest first, which is the order every caller wants: the picker shows
	// recent builds first, and Latest runs builds[0].
	sort.Slice(builds, func(a, b int) bool {
		return builds[a].InstalledAt.After(builds[b].InstalledAt)
	})
	return builds
}

// readTag recovers a build's release tag.
//
// Falls back to the directory name for a build installed before the marker
// existed. That is right in practice as well as being the only option: safeTag
// is the identity function on a real tag like v0.0.3.
func readTag(dir, fallback string) string {
	raw, err := os.ReadFile(filepath.Join(dir, tagMarker))
	if err != nil {
		return fallback
	}
	if tag := strings.TrimSpace(string(raw)); tag != "" {
		return tag
	}
	return fallback
}

// Remove deletes one build's files. A no-op for a build that is not installed.
func (i *Installer) Remove(tag string) error {
	if tag == "" {
		return nil
	}
	dir := i.layout.VersionDir(tag)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	return nil
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
func (i *Installer) Install(ctx context.Context, release catalog.Release, assetURL string, report ProgressFunc) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	asset, err := SelectAsset(release)
	if err != nil {
		return err
	}

	// Kept between attempts so an interrupted download can resume.
	partial := filepath.Join(i.layout.Versions, ".download-"+asset.Name+".part")

	if err := Fetch(ctx, assetURL, partial, asset.Size, report); err != nil {
		return err
	}

	// The catalogue guarantees a checksum, so there is no sibling to fetch and
	// no unverified path to fall back to. Verify still refuses an empty hash.
	if err := Verify(partial, asset.SHA256, report); err != nil {
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

	// Written into staging rather than after the rename, so a version directory
	// is never observed without a tag beside it.
	marker := filepath.Join(staging, tagMarker)
	if err := os.WriteFile(marker, []byte(release.Tag+"\n"), 0o644); err != nil {
		return fmt.Errorf("record the release tag: %w", err)
	}

	final := i.layout.VersionDir(release.Tag)
	if err := os.RemoveAll(final); err != nil {
		return fmt.Errorf("replace %s: %w", final, err)
	}
	if err := os.Rename(staging, final); err != nil {
		return fmt.Errorf("move the new build into place: %w", err)
	}

	os.Remove(partial)

	// Deliberately no prune here. Which builds may be reclaimed depends on what
	// the player's profiles are pinned to, and a package that downloads and
	// unpacks archives has no business reading profiles.json. The caller knows
	// the keep set and calls Prune itself.

	if report != nil {
		report(Progress{Phase: "done", Percent: 100})
	}
	return nil
}

// Prune deletes installed builds that nothing needs, and reports what it took.
//
// keep names every build that must survive whatever happens — one per profile,
// plus whatever is about to be launched. Beyond those, the `spare` most
// recently installed builds are kept as well, because a build is tens of
// megabytes and there is no reason to hoard them, but keeping a spare makes a
// bad update recoverable by hand.
//
// The keep set is the whole point of the signature. The version a profile is
// pinned to is not "an old build": it is the build that profile *is*, and
// deleting it would silently turn a working profile into a re-download the
// player never asked for.
//
// Safe to call without checking whether the game is running, because the only
// caller refuses to install at all while it does — see UpdateService.Install.
func (i *Installer) Prune(keep []string, spare int) []string {
	protected := make(map[string]bool, len(keep))
	for _, tag := range keep {
		if tag != "" {
			protected[tag] = true
		}
	}

	var removed []string
	kept := 0
	for _, build := range i.List() { // newest first
		if protected[build.Tag] {
			continue
		}
		if kept < spare {
			kept++
			continue
		}
		if err := i.Remove(build.Tag); err == nil {
			removed = append(removed, build.Tag)
		}
	}
	return removed
}
