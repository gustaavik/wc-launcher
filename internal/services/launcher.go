package services

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/gustaavik/wc-launcher/internal/install"
	"github.com/gustaavik/wc-launcher/internal/markdown"
	"github.com/gustaavik/wc-launcher/internal/selfupdate"
	"github.com/gustaavik/wc-launcher/internal/version"
)

// devMessage is what a build nobody stamped says when asked to update itself.
const devMessage = "This is a development build, so it does not update itself."

// LauncherStatus is what the UI needs to decide whether to offer an update to
// the launcher itself.
type LauncherStatus struct {
	// Current is the tag this launcher was built from, or "dev".
	Current string `json:"current"`
	// Latest is nil when the check could not be made.
	Latest *ReleaseView `json:"latest"`
	// UpdateAvailable is true when GitHub publishes a different tag.
	UpdateAvailable bool `json:"updateAvailable"`
	// Staged is true when the update is downloaded and only needs a restart.
	Staged bool `json:"staged"`
	// Supported is false when the release has no build for this platform.
	Supported bool `json:"supported"`
	// Writable is false when the launcher is installed somewhere it cannot
	// replace itself. Worth knowing before a download rather than after one.
	Writable bool `json:"writable"`
	// Dev is true for an unstamped build, which never updates itself.
	Dev bool `json:"dev"`
	// Message explains anything the fields above cannot.
	Message string `json:"message"`
}

// CheckLauncher reports whether a newer launcher is published.
//
// Never fails outright, for the same reason Check does not: a launcher that
// cannot reach GitHub is still a working launcher, and saying so is more use
// than an error.
func (u *UpdateService) CheckLauncher() LauncherStatus {
	status := LauncherStatus{
		Current:   version.Current,
		Supported: true,
		Dev:       version.IsDev(),
	}

	target, err := selfupdate.Locate()
	if err != nil {
		status.Supported = false
		status.Message = err.Error()
		return status
	}
	status.Writable = target.Writable

	u.core.mu.Lock()
	status.Staged = u.core.stagedLauncher != ""
	u.core.mu.Unlock()

	if status.Dev {
		status.Message = devMessage
		return status
	}

	release, err := u.core.Launcher.Latest(context.Background())
	if err != nil {
		status.Message = userMessage(err)
		return status
	}

	view := toLauncherReleaseView(release)
	status.Latest = &view
	// The same tag-inequality test the game update uses. A launcher that has
	// somehow ended up ahead of the published tag is still out of step with
	// what everyone else runs, and offering the published build is right.
	status.UpdateAvailable = release.Tag != version.Current

	// A release with nothing for this platform is not an update the player can
	// take, and saying "update available" would offer a button that only fails.
	if _, err := selfupdate.SelectAsset(release); err != nil {
		status.Supported = false
		status.UpdateAvailable = false
		status.Message = userMessage(err)
	}
	return status
}

// InstallLauncher downloads, verifies and unpacks the newest launcher.
//
// Nothing is replaced here: the build is staged, and ApplyLauncherUpdate puts
// it in place. Splitting the two keeps the restart something the player asks
// for rather than something a download does to them.
//
// Progress arrives on the "launcher:progress" event.
func (u *UpdateService) InstallLauncher() string {
	if u.core.Runner.Running() {
		return ErrGameRunning.Error()
	}
	if version.IsDev() {
		return devMessage
	}

	target, err := selfupdate.Locate()
	if err != nil {
		return err.Error()
	}
	if !target.Writable {
		return unwritableMessage(target)
	}

	u.core.mu.Lock()
	if u.core.cancelLauncher != nil {
		u.core.mu.Unlock()
		return "A launcher update is already downloading."
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.core.cancelLauncher = cancel
	u.core.mu.Unlock()

	defer func() {
		u.core.mu.Lock()
		u.core.cancelLauncher = nil
		u.core.mu.Unlock()
		cancel()
	}()

	release, err := u.core.Launcher.Latest(ctx)
	if err != nil {
		return userMessage(err)
	}
	if release.Tag == version.Current {
		return "The launcher is already up to date."
	}

	staged, err := selfupdate.Stage(ctx, u.core.Layout, u.core.Launcher, release, func(p install.Progress) {
		u.core.emit("launcher:progress", p)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			u.core.emit("launcher:progress", install.Progress{Phase: "cancelled", Percent: -1})
			return "Update cancelled."
		}
		u.core.emit("launcher:progress", install.Progress{Phase: "failed", Percent: -1})
		return userMessage(err)
	}

	u.core.mu.Lock()
	u.core.stagedLauncher = staged
	u.core.mu.Unlock()
	return ""
}

// ApplyLauncherUpdate replaces this launcher with the staged one and restarts.
//
// On success the launcher quits: the process being replaced cannot do the
// replacing, so the staged build does it and starts the result.
func (u *UpdateService) ApplyLauncherUpdate() string {
	// The same rule that guards every other path: the game holds the session
	// while it runs, and quitting the launcher underneath it would orphan the
	// child and lose the log it is streaming.
	if u.core.Runner.Running() {
		return ErrGameRunning.Error()
	}

	u.core.mu.Lock()
	staged := u.core.stagedLauncher
	u.core.mu.Unlock()
	if staged == "" {
		return "No launcher update has been downloaded yet."
	}

	target, err := selfupdate.Locate()
	if err != nil {
		return err.Error()
	}
	if !target.Writable {
		return unwritableMessage(target)
	}
	if err := selfupdate.Apply(staged, target); err != nil {
		return err.Error()
	}

	if u.core.Quit != nil {
		u.core.Quit()
	}
	return ""
}

// CancelLauncher stops a launcher download in progress. A no-op when none is.
//
// The partially downloaded file is kept, so resuming later picks up where this
// left off rather than starting again.
func (u *UpdateService) CancelLauncher() {
	u.core.mu.Lock()
	cancel := u.core.cancelLauncher
	u.core.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func unwritableMessage(target selfupdate.Target) string {
	return fmt.Sprintf("%s cannot update itself where it is installed. Move it to your Applications folder and try again.",
		filepath.Base(target.Path))
}

func toLauncherReleaseView(release selfupdate.Release) ReleaseView {
	return ReleaseView{
		Tag:         release.Tag,
		Name:        release.Name,
		PublishedAt: release.PublishedAt,
		Prerelease:  release.Prerelease,
		NotesHTML:   markdown.Render(release.Notes),
	}
}
