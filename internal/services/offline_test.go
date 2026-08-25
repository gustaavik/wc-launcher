package services

import (
	"strings"
	"testing"
)

// The whole point of the offline path: a launcher nobody has signed into still
// reports the build on disk as playable, and does not force an update it has no
// way of knowing about.
func TestCheckStaysPlayableWhenSignedOutWithABuildInstalled(t *testing.T) {
	core := hermeticCore(t)
	installBuild(t, core, "v0.0.1")

	status := NewUpdateService(core).Check()

	if !status.Playable {
		t.Error("a signed-out player with a build on disk must still be able to press Play")
	}
	if status.Required {
		t.Error("an update nobody could check for must never be forced")
	}
	if status.InstalledTag != "v0.0.1" {
		t.Errorf("want the installed build reported, got %q", status.InstalledTag)
	}
	if !strings.Contains(status.Message, "offline") {
		t.Errorf("the message should say they are playing offline, got %q", status.Message)
	}
}

// The other half: with nothing installed, being signed out is the only thing in
// the way, and the message has to name the one action that needs an account.
func TestCheckTellsASignedOutPlayerWithNoBuildToSignIn(t *testing.T) {
	core := hermeticCore(t)

	status := NewUpdateService(core).Check()

	if status.Playable {
		t.Error("there is no build to play")
	}
	if status.Message != signInToDownload {
		t.Errorf("want %q, got %q", signInToDownload, status.Message)
	}
}

// Downloading is the one thing offline cannot do, so the refusal has to explain
// itself rather than leaking the bare sentinel.
func TestInstallRefusesSignedOutWithAReadableMessage(t *testing.T) {
	core := hermeticCore(t)

	msg := NewUpdateService(core).Install()

	if msg != signInToDownload {
		t.Errorf("want %q, got %q", signInToDownload, msg)
	}
	if strings.Contains(msg, ErrSignedOut.Error()) {
		t.Errorf("the raw error must not reach the player, got %q", msg)
	}
}
