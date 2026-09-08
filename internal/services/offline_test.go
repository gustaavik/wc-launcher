package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gustaavik/wc-launcher/internal/catalog"
	"github.com/gustaavik/wc-launcher/internal/gamesvc"
	"github.com/gustaavik/wc-launcher/internal/install"
)

// serveCatalog stands up a catalogue with these releases, newest first, and
// returns a Core reading from it with no account server behind it.
//
// The account address stays unreachable on purpose: everything about
// downloading must work without one, and a test that accidentally needed a
// token would hang rather than quietly pass.
func serveCatalog(t *testing.T, releases ...catalog.Release) *Core {
	t.Helper()
	index := catalog.Index{SchemaVersion: catalog.SchemaVersion, Releases: releases}
	raw, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("marshal catalogue: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/index.json") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(raw)
	}))
	t.Cleanup(server.Close)
	return testCore(t, "http://127.0.0.1:1", server.URL)
}

// gameRunning starts a stand-in for the game, so Runner.Running() is genuinely
// true rather than faked through a test-only door in the runner.
func gameRunning(t *testing.T, core *Core) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}

	// probeVulkan honours an already-set VK_ICD_FILENAMES as-is, so pointing it
	// at any existing file gets past the driver check. Nothing renders here.
	icd := filepath.Join(t.TempDir(), "icd.json")
	if err := os.WriteFile(icd, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VK_ICD_FILENAMES", icd)

	dir := core.Layout.VersionDir("running")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, install.GameBinary())
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	opts := gamesvc.Options{VersionDir: dir, DataDir: core.Layout.Data}
	if err := core.Runner.Start(opts, core.Layout.GameLog(), nil, nil); err != nil {
		t.Fatalf("start the stand-in game: %v", err)
	}
	t.Cleanup(func() { _ = core.Runner.Stop() })
}

// The whole point of the offline path: a launcher nobody has signed into still
// reports the build on disk as playable, and does not force an update it has no
// way of knowing about.
func TestCheckStaysPlayableWhenTheCatalogueCannotBeRead(t *testing.T) {
	core := hermeticCore(t)
	installBuild(t, core, "v0.0.1")

	status := NewUpdateService(core).Check()

	if !status.Playable {
		t.Error("a player with a build on disk must still be able to press Play")
	}
	if status.Required {
		t.Error("an update nobody could check for must never be forced")
	}
	if status.InstalledTag != "v0.0.1" {
		t.Errorf("want the installed build reported, got %q", status.InstalledTag)
	}
	if status.Message == "" {
		t.Error("an unreadable catalogue should be explained, not passed over in silence")
	}
}

// The behaviour change: downloading no longer needs an account. A signed-out
// player with nothing installed is offered the install, not a sign-in wall.
func TestASignedOutPlayerIsOfferedTheInstall(t *testing.T) {
	core := serveCatalog(t, published("v0.5.0"))

	status := NewUpdateService(core).Check()

	if !status.UpdateAvailable {
		t.Fatalf("want an install on offer, got %+v", status)
	}
	if !status.Supported {
		t.Error("this platform has a build in the fixture")
	}
	if status.Latest == nil || status.Latest.Tag != "v0.5.0" {
		t.Errorf("want v0.5.0 reported as latest, got %+v", status.Latest)
	}
	// Signing in is still worth mentioning — for multiplayer, not downloads.
	if !strings.Contains(status.Message, "multiplayer") {
		t.Errorf("want the message to be about multiplayer, got %q", status.Message)
	}
	if strings.Contains(strings.ToLower(status.Message), "sign in to download") {
		t.Errorf("downloading no longer needs an account, got %q", status.Message)
	}
}

// Reading the catalogue touches no refresh token, and the one rule that matters
// while the game runs is that the launcher must not touch one.
func TestCheckStillWorksWhileTheGameIsRunning(t *testing.T) {
	core := serveCatalog(t, published("v0.5.0"))
	installBuild(t, core, "v0.5.0")
	gameRunning(t, core)

	status := NewUpdateService(core).Check()

	if status.Latest == nil {
		t.Fatalf("the catalogue should still be readable, got %q", status.Message)
	}
	if !status.Playable {
		t.Error("the installed build is still the one that would run")
	}
}

// The guard that must NOT lift: an install replaces a version directory the
// running game may have open.
func TestInstallStillRefusesWhileTheGameIsRunning(t *testing.T) {
	core := serveCatalog(t, published("v0.5.0"))
	gameRunning(t, core)

	msg := NewUpdateService(core).Install()

	if msg != ErrGameRunning.Error() {
		t.Errorf("want %q, got %q", ErrGameRunning.Error(), msg)
	}
}

// A bucket that answers but publishes nothing is a deployment mistake. Saying
// "no updates" would make it look like an up-to-date launcher.
func TestAnEmptyCatalogueIsExplainedRatherThanLookingUpToDate(t *testing.T) {
	core := serveCatalog(t)

	status := NewUpdateService(core).Check()

	if status.UpdateAvailable {
		t.Error("nothing is published")
	}
	if status.Message == "" {
		t.Error("an empty catalogue must be explained")
	}
}

// The version picker reads the same public catalogue, so it works signed out.
func TestTheVersionPickerWorksSignedOut(t *testing.T) {
	core := serveCatalog(t, published("v0.5.0"), published("v0.4.0"))

	list := NewProfileService(core).Releases()

	if list.Error != "" {
		t.Fatalf("Releases: %s", list.Error)
	}
	if len(list.Releases) != 2 {
		t.Fatalf("want both releases, got %+v", list.Releases)
	}
	if !list.Releases[0].Supported {
		t.Error("the fixture publishes a build for every platform")
	}
}

// A pin that has aged out of the catalogue must say so, not quietly become
// whatever is newest.
func TestAPinToAnUnpublishedTagIsReportedRatherThanSilentlyMoved(t *testing.T) {
	core := serveCatalog(t, published("v0.5.0"))
	pinned, err := core.Profiles.Create("Speedrun", "v0.0.9")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := core.Profiles.Select(pinned.ID); err != nil {
		t.Fatalf("select: %v", err)
	}

	status := NewUpdateService(core).Check()

	if status.Target != nil {
		t.Errorf("want no target for a tag that is gone, got %+v", status.Target)
	}
	if !strings.Contains(status.Message, "v0.0.9") {
		t.Errorf("the message should name the missing tag, got %q", status.Message)
	}
}

// Every other client's failures are phrased for a player; the catalogue's must
// be too, rather than leaking a dial error into the UI.
func TestACatalogueOutageIsPhrasedForAPlayer(t *testing.T) {
	status := NewUpdateService(hermeticCore(t)).Check()

	if !strings.Contains(status.Message, "Check your connection") {
		t.Errorf("want a readable outage message, got %q", status.Message)
	}
	if strings.Contains(status.Message, "dial tcp") {
		t.Errorf("the raw transport error reached the player: %q", status.Message)
	}
}
