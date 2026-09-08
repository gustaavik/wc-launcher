package services

import (
	"context"
	"errors"

	"github.com/gustaavik/wc-launcher/internal/install"
)

// ProfileService is the player's list of profiles: what exists, which is
// selected, and what versions there are to pin one to.
//
// A service of its own rather than more methods on UpdateService, because
// main.go's service list is the frontend's API surface and "profiles" is a
// coherent noun there.
type ProfileService struct{ core *Core }

func NewProfileService(core *Core) *ProfileService { return &ProfileService{core: core} }

// ProfileList is the whole list plus the selection.
//
// Every mutator returns one, so the frontend never has to reconcile a partial
// update against what it already had.
type ProfileList struct {
	Profiles []ProfileView `json:"profiles"`
	Selected string        `json:"selected"`
	// Error is empty on success, matching LoginResult: a Go error crossing the
	// binding would arrive as a thrown exception the UI has to catch.
	Error string `json:"error"`
}

// ReleaseList is what the version picker shows.
type ReleaseList struct {
	Releases []ReleaseOption `json:"releases"`
	Error    string          `json:"error"`
}

// List reports every profile, Latest first.
func (p *ProfileService) List() ProfileList { return p.list("") }

// Select changes which profile Play will launch.
//
// Allowed while the game runs: it changes only the *next* launch, and is no
// more consequential than opening Settings.
func (p *ProfileService) Select(id string) ProfileList {
	return p.list(errorText(p.core.Profiles.Select(id)))
}

// Create adds a profile pinned to tag.
func (p *ProfileService) Create(name, tag string) ProfileList {
	if p.core.Runner.Running() {
		return p.list(ErrGameRunning.Error())
	}
	_, err := p.core.Profiles.Create(name, tag)
	return p.list(errorText(err))
}

// Rename changes a profile's name.
func (p *ProfileService) Rename(id, name string) ProfileList {
	if p.core.Runner.Running() {
		return p.list(ErrGameRunning.Error())
	}
	_, err := p.core.Profiles.Rename(id, name)
	return p.list(errorText(err))
}

// Retag repins a profile to a different version.
func (p *ProfileService) Retag(id, tag string) ProfileList {
	if p.core.Runner.Running() {
		return p.list(ErrGameRunning.Error())
	}
	_, err := p.core.Profiles.Retag(id, tag)
	return p.list(errorText(err))
}

// Delete removes a profile.
//
// The build it pinned stays on disk. Deleting a profile is cheap to regret and
// re-downloading a few hundred megabytes is not; the build simply stops being
// pinned, and the next prune may reclaim it.
func (p *ProfileService) Delete(id string) ProfileList {
	if p.core.Runner.Running() {
		return p.list(ErrGameRunning.Error())
	}
	return p.list(errorText(p.core.Profiles.Delete(id)))
}

// Releases lists the versions a profile can be pinned to.
//
// Needs a bearer token, so it reports the same signed-out and game-running
// conditions the update check does rather than failing opaquely.
func (p *ProfileService) Releases() ReleaseList {
	ctx := context.Background()

	token, err := p.core.Session.AccessToken(ctx)
	if err != nil {
		switch {
		case errors.Is(err, ErrGameRunning):
			return ReleaseList{Error: "Wyvencraft is running."}
		case errors.Is(err, ErrSignedOut):
			return ReleaseList{Error: "Sign in to see the available versions."}
		default:
			return ReleaseList{Error: userMessage(err)}
		}
	}

	releases, err := p.core.Client.Releases(ctx, token)
	if err != nil {
		return ReleaseList{Error: userMessage(err)}
	}

	options := make([]ReleaseOption, 0, len(releases))
	for _, release := range releases {
		_, assetErr := install.SelectAsset(release)
		options = append(options, ReleaseOption{
			Tag:         release.Tag,
			Name:        release.Name,
			PublishedAt: release.PublishedAt,
			Prerelease:  release.Prerelease,
			Supported:   assetErr == nil,
			Installed:   p.core.Install.Installed(release.Tag),
		})
	}
	return ReleaseList{Releases: options}
}

// Builds reports the game builds currently unpacked on disk.
func (p *ProfileService) Builds() []install.Build {
	builds := p.core.Install.List()
	if builds == nil {
		// A nil slice crosses the binding as null; the UI wants a list it can
		// iterate without a guard.
		return []install.Build{}
	}
	return builds
}

// list renders the current state, carrying err through.
func (p *ProfileService) list(errText string) ProfileList {
	stored := p.core.Profiles.List()
	views := make([]ProfileView, 0, len(stored))
	for _, profile := range stored {
		views = append(views, toProfileView(profile, p.core.Install))
	}
	return ProfileList{
		Profiles: views,
		Selected: p.core.Profiles.Selected().ID,
		Error:    errText,
	}
}

// errorText is the empty-string-means-success convention, in one place.
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
