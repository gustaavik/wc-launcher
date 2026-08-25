package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gustaavik/wc-launcher/internal/install"
	"github.com/gustaavik/wc-launcher/internal/wcauth"
)

// hermeticCore is a Core that never reaches the network. The address is one
// nothing listens on, so any accidental call fails fast rather than hanging.
func hermeticCore(t *testing.T) *Core {
	t.Helper()
	return testCore(t, "http://127.0.0.1:1")
}

// installBuild fakes an unpacked build under versions/<tag>.
func installBuild(t *testing.T, core *Core, tag string) {
	t.Helper()
	dir := core.Layout.VersionDir(tag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	binary := filepath.Join(dir, install.GameBinary())
	if err := os.WriteFile(binary, []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", binary, err)
	}
}

// published mirrors the release workflow's asset naming for every target
// the installer knows, so the same fixture works on any developer's machine.
func published(tag string) wcauth.Release {
	names := []string{
		"aarch64-apple-darwin.tar.gz",
		"x86_64-apple-darwin.tar.gz",
		"x86_64-unknown-linux-gnu.tar.gz",
		"aarch64-unknown-linux-gnu.tar.gz",
		"x86_64-pc-windows-msvc.zip",
	}
	assets := make([]wcauth.Asset, 0, len(names))
	for i, suffix := range names {
		assets = append(assets, wcauth.Asset{
			ID:   string(rune('1' + i)),
			Name: "wyvencraft-" + tag + "-" + suffix,
			Size: "1024",
		})
	}
	return wcauth.Release{Tag: tag, Name: tag, Assets: assets}
}

// The force, enforced behind the binding rather than by a disabled button.
func TestLatestProfileWillNotLaunchAnOutdatedBuild(t *testing.T) {
	core := hermeticCore(t)
	installBuild(t, core, "v0.0.1")
	latest := published("v0.0.2")
	core.setKnownLatest(&latest)

	_, err := NewGameService(core).launchPlan()
	if err == nil {
		t.Fatal("the Latest profile must refuse to run a build it knows is stale")
	}
	if !strings.Contains(err.Error(), "v0.0.2") {
		t.Errorf("the refusal should name the release to update to, got %q", err)
	}
}

// The carve-out that keeps a network blip from becoming a locked door. Nothing
// has ever told this launcher what the newest release is, so nothing is forced.
func TestLatestProfileStillLaunchesWhenTheNewestReleaseIsUnknown(t *testing.T) {
	core := hermeticCore(t)
	installBuild(t, core, "v0.0.1")

	dir, err := NewGameService(core).launchPlan()
	if err != nil {
		t.Fatalf("an offline launcher must still play the build it has, got %v", err)
	}
	if dir != core.Layout.VersionDir("v0.0.1") {
		t.Errorf("want the installed build, got %q", dir)
	}
}

// A release nobody here could install is not an update being refused.
func TestLatestProfileStillLaunchesWhenTheNewReleaseHasNoBuildForThisPlatform(t *testing.T) {
	core := hermeticCore(t)
	installBuild(t, core, "v0.0.1")
	// No assets at all, so SelectAsset finds nothing for any platform.
	latest := wcauth.Release{Tag: "v0.0.2", Name: "v0.0.2"}
	core.setKnownLatest(&latest)

	if _, err := NewGameService(core).launchPlan(); err != nil {
		t.Fatalf("an uninstallable release must not block play, got %v", err)
	}
}

func TestLatestProfileLaunchesTheNewestBuildOnceItIsInstalled(t *testing.T) {
	core := hermeticCore(t)
	installBuild(t, core, "v0.0.1")
	installBuild(t, core, "v0.0.2")
	latest := published("v0.0.2")
	core.setKnownLatest(&latest)

	dir, err := NewGameService(core).launchPlan()
	if err != nil {
		t.Fatalf("launchPlan: %v", err)
	}
	if dir != core.Layout.VersionDir("v0.0.2") {
		t.Errorf("want the newest build, got %q", dir)
	}
}

// A pin means "this build, and no other" — a newer release is not its business.
func TestAPinnedProfileLaunchesItsOwnBuildEvenWhenNewerOnesExist(t *testing.T) {
	core := hermeticCore(t)
	installBuild(t, core, "v0.0.1")
	installBuild(t, core, "v0.0.2")
	latest := published("v0.0.2")
	core.setKnownLatest(&latest)

	pinned, err := core.Profiles.Create("Speedrun", "v0.0.1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := core.Profiles.Select(pinned.ID); err != nil {
		t.Fatalf("select: %v", err)
	}

	dir, err := NewGameService(core).launchPlan()
	if err != nil {
		t.Fatalf("a pinned profile must not be forced to update, got %v", err)
	}
	if dir != core.Layout.VersionDir("v0.0.1") {
		t.Errorf("want the pinned build, got %q", dir)
	}
}

func TestLaunchRefusesAPinnedProfileWhoseBuildIsNotInstalled(t *testing.T) {
	core := hermeticCore(t)
	pinned, err := core.Profiles.Create("Speedrun", "v0.0.1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := core.Profiles.Select(pinned.ID); err != nil {
		t.Fatalf("select: %v", err)
	}

	_, err = NewGameService(core).launchPlan()
	if err == nil {
		t.Fatal("want a refusal for a pin with no build")
	}
	if !strings.Contains(err.Error(), "v0.0.1") {
		t.Errorf("the refusal should name the missing version, got %q", err)
	}
}

// Deleting a profile is cheap to regret; a few hundred megabytes of re-download
// is not. The build stays and only the pin goes.
func TestDeletingAProfileLeavesItsBuildInstalled(t *testing.T) {
	core := hermeticCore(t)
	installBuild(t, core, "v0.0.1")
	pinned, err := core.Profiles.Create("Speedrun", "v0.0.1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if result := NewProfileService(core).Delete(pinned.ID); result.Error != "" {
		t.Fatalf("delete: %s", result.Error)
	}

	if !core.Install.Installed("v0.0.1") {
		t.Error("the build should survive the profile that pinned it")
	}
	if tags := core.Profiles.PinnedTags(); len(tags) != 0 {
		t.Errorf("the pin should be gone, got %v", tags)
	}
}

// A prune must not reclaim a pinned build even when it is the oldest thing on
// disk — the pairing of these two packages is where the invariant actually has
// to hold.
func TestAPinnedBuildSurvivesThePruneAfterAnInstall(t *testing.T) {
	core := hermeticCore(t)
	for _, tag := range []string{"v0.0.1", "v0.0.2", "v0.0.3", "v0.0.4"} {
		installBuild(t, core, tag)
	}
	if _, err := core.Profiles.Create("Speedrun", "v0.0.1"); err != nil {
		t.Fatalf("create: %v", err)
	}

	core.Install.Prune(append(core.Profiles.PinnedTags(), "v0.0.4"), keptSpareBuilds)

	if !core.Install.Installed("v0.0.1") {
		t.Error("a pinned build must survive a prune")
	}
	if !core.Install.Installed("v0.0.4") {
		t.Error("the build just installed must survive a prune")
	}
}

// The first run for someone who already had a build: they land on Latest with
// it recognised, which is exactly the behaviour they had before profiles.
func TestFirstRunWithAnInstalledBuildAndNoProfilesFileSelectsLatest(t *testing.T) {
	core := hermeticCore(t)
	installBuild(t, core, "v0.0.3")

	selected := core.Profiles.Selected()
	if !selected.IsLatest() {
		t.Fatalf("want Latest selected on a first run, got %+v", selected)
	}

	list := NewProfileService(core).List()
	if len(list.Profiles) != 1 || !list.Profiles[0].Latest {
		t.Fatalf("want just Latest, got %+v", list.Profiles)
	}
	if !list.Profiles[0].Installed {
		t.Error("Latest should report the existing build as installed")
	}
}

// Tokens and release lists both belong to one server. A cached "newest release"
// from the old one would force an update to a tag the new one never published.
func TestChangingTheAccountServerForgetsTheCachedLatestRelease(t *testing.T) {
	core := hermeticCore(t)
	latest := published("v0.0.2")
	core.setKnownLatest(&latest)

	NewAuthService(core).SaveSettings("http://127.0.0.1:2", "")

	if _, ok := core.knownLatest(); ok {
		t.Error("the cached release should have been forgotten with the server")
	}
}

func TestMigrateRemovesTheOldInstallRecord(t *testing.T) {
	core := hermeticCore(t)
	if err := os.WriteFile(core.Layout.StateFile(), []byte(`{"tag":"v0.0.1"}`), 0o644); err != nil {
		t.Fatalf("seed installed.json: %v", err)
	}

	migrate(core.Layout)

	if _, err := os.Stat(core.Layout.StateFile()); !os.IsNotExist(err) {
		t.Errorf("installed.json should be gone, stat gave %v", err)
	}
	// And again, on a launcher that has already migrated.
	migrate(core.Layout)
}
