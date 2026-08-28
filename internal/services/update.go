package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/gustaavik/wc-launcher/internal/deps"
	"github.com/gustaavik/wc-launcher/internal/install"
	"github.com/gustaavik/wc-launcher/internal/wcauth"
)

// signInToDownload is the refusal a signed-out player gets from anything that
// reaches the release broker. The game repository is private, so the account
// server brokers every download against the player's own token — this is the
// one thing playing offline cannot do.
const signInToDownload = "Sign in to download Wyvencraft."

// UpdateService checks for and installs game builds.
type UpdateService struct{ core *Core }

func NewUpdateService(core *Core) *UpdateService { return &UpdateService{core: core} }

// Check reports what the selected profile needs and whether it can play.
//
// Never fails outright: a check that cannot reach the server still reports what
// is installed, because an offline player with a build should still be able to
// press Play. That is also why the forced update is gated on a *successful*
// check — see UpdateStatus.Required.
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

	token, err := u.core.Session.AccessToken(context.Background())
	if err != nil {
		if errors.Is(err, ErrGameRunning) {
			status.Message = "Wyvencraft is running."
		} else if errors.Is(err, ErrSignedOut) {
			// Two different situations, and telling them apart is the whole
			// point: with a build on disk, being signed out costs updates and
			// multiplayer. With none, it is the only thing in the way.
			if status.Playable {
				status.Message = "Playing offline. Sign in to check for updates."
			} else {
				status.Message = signInToDownload
			}
		} else {
			status.Message = userMessage(err)
		}
		return status
	}

	latest, err := u.core.Client.LatestRelease(context.Background(), token)
	if err != nil {
		status.Message = userMessage(err)
		return status
	}
	u.core.setKnownLatest(&latest)

	latestView := toReleaseView(latest)
	status.Latest = &latestView

	target := latest
	if !profile.IsLatest() {
		found, err := u.releaseTagged(context.Background(), token, profile.Tag)
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
	return status
}

// releaseTagged finds one published release by tag.
//
// List-then-find rather than a route of its own: the picker fetches the list
// anyway, and the server caches it. A tag that has aged out of the window is
// reported as such rather than silently treated as missing.
func (u *UpdateService) releaseTagged(ctx context.Context, token, tag string) (wcauth.Release, error) {
	releases, err := u.core.Client.Releases(ctx, token)
	if err != nil {
		return wcauth.Release{}, err
	}
	for _, release := range releases {
		if release.Tag == tag {
			return release, nil
		}
	}
	return wcauth.Release{}, fmt.Errorf("%s is no longer published; pick another version for this profile", tag)
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

	token, err := u.core.Session.AccessToken(ctx)
	if err != nil {
		if errors.Is(err, ErrSignedOut) {
			// userMessage would surface the bare "not signed in", which says
			// nothing about what to do or why this one action needs it.
			return signInToDownload
		}
		return userMessage(err)
	}

	// What the *selected profile* needs, which is only "latest" when the Latest
	// profile is selected.
	profile := u.core.Profiles.Selected()
	var release wcauth.Release
	if profile.IsLatest() {
		release, err = u.core.Client.LatestRelease(ctx, token)
	} else {
		release, err = u.releaseTagged(ctx, token, profile.Tag)
	}
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

	err = u.core.Install.Install(ctx, token, release, report)
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
type ServerInfo struct {
	Reachable bool   `json:"reachable"`
	Version   string `json:"version"`
	// UpdatesEnabled is false when the server brokers no downloads, so the UI
	// can say so rather than offering a button that answers 501.
	UpdatesEnabled bool   `json:"updatesEnabled"`
	Message        string `json:"message"`
}

// Server probes the account server. Unauthenticated, so it works before login.
func (u *UpdateService) Server() ServerInfo {
	health, err := u.core.Client.Health(context.Background())
	if err != nil {
		return ServerInfo{Message: userMessage(err)}
	}
	info := ServerInfo{
		Reachable:      true,
		Version:        health.Version,
		UpdatesEnabled: health.UpdatesEnabled,
	}
	if !health.UpdatesEnabled {
		info.Message = "This server does not offer game downloads."
	}
	return info
}
