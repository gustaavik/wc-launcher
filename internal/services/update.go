package services

import (
	"context"
	"errors"

	"github.com/gustaavik/wc-launcher/internal/install"
)

// UpdateService checks for and installs game builds.
type UpdateService struct{ core *Core }

func NewUpdateService(core *Core) *UpdateService { return &UpdateService{core: core} }

// Check reports what is installed and what is available.
//
// Never fails outright: a check that cannot reach the server still reports what
// is installed, because an offline player with a build should still be able to
// press Play.
func (u *UpdateService) Check() UpdateStatus {
	state := u.core.Install.State()
	status := UpdateStatus{
		InstalledTag: state.Tag,
		Playable:     u.core.Install.Installed(state.Tag),
		Supported:    true,
	}

	token, err := u.core.Session.AccessToken(context.Background())
	if err != nil {
		if errors.Is(err, ErrGameRunning) {
			status.Message = "Wyvencraft is running."
		} else if errors.Is(err, ErrSignedOut) {
			status.Message = "Sign in to check for updates."
		} else {
			status.Message = userMessage(err)
		}
		return status
	}

	release, err := u.core.Client.LatestRelease(context.Background(), token)
	if err != nil {
		status.Message = userMessage(err)
		return status
	}

	view := toReleaseView(release)
	status.Latest = &view
	status.UpdateAvailable = release.Tag != state.Tag

	// A release with nothing for this platform is not an update the player can
	// take, and saying "update available" would offer a button that only fails.
	if _, err := install.SelectAsset(release); err != nil {
		status.Supported = false
		status.UpdateAvailable = false
		status.Message = userMessage(err)
	}
	return status
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
		return userMessage(err)
	}

	release, err := u.core.Client.LatestRelease(ctx, token)
	if err != nil {
		return userMessage(err)
	}

	err = u.core.Install.Install(ctx, token, release, func(p install.Progress) {
		u.core.emit("update:progress", p)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			u.core.emit("update:progress", install.Progress{Phase: "cancelled", Percent: -1})
			return "Install cancelled."
		}
		u.core.emit("update:progress", install.Progress{Phase: "failed", Percent: -1})
		return userMessage(err)
	}
	return ""
}

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
