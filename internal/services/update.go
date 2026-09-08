package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/gustaavik/wc-launcher/internal/catalog"
	"github.com/gustaavik/wc-launcher/internal/deps"
	"github.com/gustaavik/wc-launcher/internal/install"
)

// UpdateService checks for and installs game builds.
type UpdateService struct{ core *Core }

func NewUpdateService(core *Core) *UpdateService { return &UpdateService{core: core} }

// Check reports what the selected profile needs and whether it can play.
//
// Never fails outright: a check that cannot reach the catalogue still reports
// what is installed, because an offline player with a build should still be
// able to press Play. That is also why the forced update is gated on a
// *successful* check — see UpdateStatus.Required.
//
// Needs no access token, which is what lets it run while the game is running:
// the one thing the launcher must not do then is touch the refresh token, and
// reading a public catalogue does not.
func (u *UpdateService) Check() UpdateStatus {
	profile := u.core.Profiles.Selected()
	status := UpdateStatus{
		Profile:   toProfileView(profile, u.core.Install),
		Supported: true,
	}

	// What would run right now, before anything is known about what is
	// published. For Latest that is the newest build on disk; for a pin it is
	// that build or nothing.
	if profile.IsLatest() {
		if builds := u.core.Install.List(); len(builds) > 0 {
			status.InstalledTag = builds[0].Tag
		}
	} else if u.core.Install.Installed(profile.Tag) {
		status.InstalledTag = profile.Tag
	}
	status.Playable = status.InstalledTag != ""

	index, err := u.core.Catalog.Index(context.Background())
	if err != nil {
		status.Message = userMessage(err)
		return status
	}

	latest, ok := index.Latest()
	if !ok {
		status.Message = "No Wyvencraft builds are published yet."
		return status
	}
	u.core.setKnownLatest(&latest)

	latestView := toReleaseView(latest)
	status.Latest = &latestView

	target := latest
	if !profile.IsLatest() {
		found, err := releaseTagged(index, profile.Tag)
		if err != nil {
			// The pin still points at a build that may well be installed, so
			// this is a message, not a downgrade to unplayable.
			status.Message = userMessage(err)
			return status
		}
		target = found
	}

	targetView := toReleaseView(target)
	status.Target = &targetView
	status.UpdateAvailable = !u.core.Install.Installed(target.Tag)

	// A release with nothing for this platform is not an update the player can
	// take, and saying "update available" would offer a button that only fails.
	if _, err := install.SelectAsset(target); err != nil {
		status.Supported = false
		status.UpdateAvailable = false
		status.Message = userMessage(err)
		return status
	}

	// The force. Latest is a promise to run the newest build, so an older one
	// is not a fallback the player gets to decline — but only now, having
	// actually learned what the newest build is.
	if profile.IsLatest() && status.UpdateAvailable {
		status.Required = true
		status.Playable = false
	}

	// Downloading needs no account; multiplayer does. Said only when there is
	// nothing more useful to say, so it never displaces a real problem.
	if status.Message == "" && u.core.Session.Account() == nil {
		status.Message = "Playing offline. Sign in for multiplayer."
	}
	return status
}

// releaseTagged finds one published release by tag.
//
// A lookup in the catalogue already fetched rather than a second request: the
// index carries every release, so a pinned profile costs the same one fetch as
// Latest. A tag that is no longer published is reported as such rather than
// silently becoming "whatever is newest".
func releaseTagged(index catalog.Index, tag string) (catalog.Release, error) {
	if release, ok := index.Find(tag); ok {
		return release, nil
	}
	return catalog.Release{}, fmt.Errorf("%s is no longer published; pick another version for this profile", tag)
}

// Install downloads and unpacks the latest release.
//
// Progress arrives on the "update:progress" event rather than as a return
// value, so the UI can show a bar while this runs.
func (u *UpdateService) Install() string {
	if u.core.Runner.Running() {
		return ErrGameRunning.Error()
	}

	u.core.mu.Lock()
	if u.core.cancelInstall != nil {
		u.core.mu.Unlock()
		return "An install is already running."
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.core.cancelInstall = cancel
	u.core.mu.Unlock()

	defer func() {
		u.core.mu.Lock()
		u.core.cancelInstall = nil
		u.core.mu.Unlock()
		cancel()
	}()

	index, err := u.core.Catalog.Index(ctx)
	if err != nil {
		return userMessage(err)
	}

	// What the *selected profile* needs, which is only "latest" when the Latest
	// profile is selected.
	profile := u.core.Profiles.Selected()
	var release catalog.Release
	if profile.IsLatest() {
		found, ok := index.Latest()
		if !ok {
			return "No Wyvencraft builds are published yet."
		}
		release = found
	} else {
		release, err = releaseTagged(index, profile.Tag)
		if err != nil {
			return userMessage(err)
		}
	}

	asset, err := install.SelectAsset(release)
	if err != nil {
		return userMessage(err)
	}

	report := func(p install.Progress) { u.core.emit("update:progress", p) }

	// The Vulkan driver, before the game. Deliberately not fatal: a machine
	// that already has one does not need this to succeed, and a hiccup here
	// must not throw away a game download that would otherwise have worked.
	// GameService.Launch tries again if it turns out the game cannot run.
	if _, err := deps.Ensure(ctx, u.core.Layout, report); err != nil {
		logIfErr("could not install the graphics driver", err)
	}

	err = u.core.Install.Install(ctx, release, u.core.Catalog.AssetURL(asset), report)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			u.core.emit("update:progress", install.Progress{Phase: "cancelled", Percent: -1})
			return "Install cancelled."
		}
		u.core.emit("update:progress", install.Progress{Phase: "failed", Percent: -1})
		return userMessage(err)
	}

	// Reclaim disk now that something new is in place. Every pinned build is in
	// the keep set: a pin is not an old build, it is the build that profile is.
	// Safe here and nowhere else, because this method refuses to run at all
	// while the game holds a version directory open.
	u.core.Install.Prune(append(u.core.Profiles.PinnedTags(), release.Tag), keptSpareBuilds)
	return ""
}

// keptSpareBuilds is how many unpinned builds survive a prune beyond the one
// just installed, so an update that turns out badly can be rolled back by hand.
const keptSpareBuilds = 2

// Cancel stops an install in progress. A no-op when none is running.
//
// The partially downloaded file is kept, so resuming later picks up where this
// left off rather than starting again.
func (u *UpdateService) Cancel() {
	u.core.mu.Lock()
	cancel := u.core.cancelInstall
	u.core.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// ServerInfo reports what the account server supports.
//
// Only about signing in. Whether builds can be downloaded is no longer this
// server's business, so it is deliberately not reported here — the update check
// answers that, against the catalogue.
type ServerInfo struct {
	Reachable bool   `json:"reachable"`
	Version   string `json:"version"`
	Message   string `json:"message"`
}

// Server probes the account server. Unauthenticated, so it works before login.
func (u *UpdateService) Server() ServerInfo {
	health, err := u.core.Client.Health(context.Background())
	if err != nil {
		return ServerInfo{Message: userMessage(err)}
	}
	return ServerInfo{Reachable: true, Version: health.Version}
}
