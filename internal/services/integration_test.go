package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gustaavik/wc-launcher/internal/config"
	"github.com/gustaavik/wc-launcher/internal/gamesvc"
	"github.com/gustaavik/wc-launcher/internal/install"
	"github.com/gustaavik/wc-launcher/internal/paths"
	"github.com/gustaavik/wc-launcher/internal/profile"
	"github.com/gustaavik/wc-launcher/internal/profiles"
	"github.com/gustaavik/wc-launcher/internal/wcauth"
)

// The end-to-end path — sign in, check, download, verify, unpack — against a
// real account server. Skipped unless one is pointed at, so `go test ./...`
// stays hermetic:
//
//	WC_IT_AUTH_URL=http://localhost:8080 \
//	WC_IT_USERNAME=launchertest WC_IT_PASSWORD=... \
//	go test ./internal/services/ -run TestEndToEnd -v
//
// This is what proves the three repos agree: the launcher's client against the
// server's new release routes against GitHub's real assets.
func integrationEnv(t *testing.T) (url, username, password string) {
	t.Helper()
	url = os.Getenv("WC_IT_AUTH_URL")
	username = os.Getenv("WC_IT_USERNAME")
	password = os.Getenv("WC_IT_PASSWORD")
	if url == "" || username == "" || password == "" {
		t.Skip("set WC_IT_AUTH_URL, WC_IT_USERNAME and WC_IT_PASSWORD to run the integration test")
	}
	return url, username, password
}

// testCore builds a Core rooted in a temp directory, so a run never touches the
// real Wyvencraft install.
func testCore(t *testing.T, authURL string) *Core {
	t.Helper()
	root := t.TempDir()
	layout := paths.Layout{
		Root:     root,
		Versions: filepath.Join(root, "versions"),
		Data:     filepath.Join(root, "data"),
		Logs:     filepath.Join(root, "logs"),
	}
	for _, dir := range []string{layout.Versions, layout.Data, layout.Logs} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	client := wcauth.New(authURL)
	runner := gamesvc.NewRunner()
	core := &Core{
		Layout:   layout,
		Runner:   runner,
		Client:   client,
		Settings: config.Settings{AuthURL: authURL},
	}
	core.Session = NewSession(layout, client, runner.Running)
	core.Install = install.New(layout, client)
	core.Profiles = profiles.Open(layout.ProfilesFile())
	return core
}

func TestEndToEndSignInAndInstall(t *testing.T) {
	authURL, username, password := integrationEnv(t)
	core := testCore(t, authURL)
	auth := NewAuthService(core)
	updates := NewUpdateService(core)

	t.Run("the server advertises downloads", func(t *testing.T) {
		info := updates.Server()
		if !info.Reachable {
			t.Fatalf("server unreachable: %s", info.Message)
		}
		if !info.UpdatesEnabled {
			t.Fatal("updates_enabled is false; set GITHUB_RELEASES_TOKEN on the server")
		}
	})

	t.Run("signing in writes a profile the game can read", func(t *testing.T) {
		result := auth.Login(username, password)
		if result.Error != "" {
			t.Fatalf("login: %s", result.Error)
		}
		if result.Account == nil || result.Account.Username == "" {
			t.Fatal("no account returned")
		}

		// The whole point of the handoff: the game reads this file at boot.
		stored, err := profile.Read(core.Layout.ProfileFile())
		if err != nil {
			t.Fatalf("read profile.toml: %v", err)
		}
		if stored.Account == nil {
			t.Fatal("profile.toml has no [account] table")
		}
		if stored.Account.RefreshToken == "" {
			t.Error("no refresh token persisted")
		}
		if stored.Account.AccountID != result.Account.ID {
			t.Errorf("account_id = %q, want %q", stored.Account.AccountID, result.Account.ID)
		}
	})

	t.Run("the keys a host needs are cached", func(t *testing.T) {
		if err := core.Session.CacheKeys(t.Context()); err != nil {
			t.Fatalf("CacheKeys: %v", err)
		}
		raw, err := os.ReadFile(core.Layout.KeysFile())
		if err != nil {
			t.Fatalf("read authkeys.toml: %v", err)
		}
		if !strings.Contains(string(raw), "[[keys]]") {
			t.Errorf("authkeys.toml looks wrong:\n%s", raw)
		}
	})

	t.Run("a check finds the latest release and renders its notes", func(t *testing.T) {
		status := updates.Check()
		if status.Latest == nil {
			t.Fatalf("no release found: %s", status.Message)
		}
		if status.Latest.Tag == "" {
			t.Error("release has no tag")
		}
		// Rendered Go-side; the page never parses Markdown itself.
		if status.Latest.NotesHTML == "" {
			t.Error("release notes did not render")
		}
		if status.InstalledTag != "" {
			t.Errorf("a fresh root should have nothing installed, got %q", status.InstalledTag)
		}
		if !status.Supported {
			t.Skipf("no build published for this platform: %s", status.Message)
		}
		if !status.UpdateAvailable {
			t.Error("an empty install should report an update available")
		}
	})

	t.Run("installing downloads, verifies and unpacks a runnable build", func(t *testing.T) {
		if testing.Short() {
			t.Skip("downloads a real release")
		}
		if status := updates.Check(); !status.Supported {
			t.Skip("no build for this platform")
		}

		if msg := updates.Install(); msg != "" {
			t.Fatalf("install: %s", msg)
		}

		builds := core.Install.List()
		if len(builds) == 0 {
			t.Fatal("nothing installed")
		}
		dir := core.Layout.VersionDir(builds[0].Tag)

		// The binary and assets/ must sit directly in the version directory:
		// it becomes the game's working directory, and assets are resolved
		// against it.
		binary := filepath.Join(dir, install.GameBinary())
		info, err := os.Stat(binary)
		if err != nil {
			t.Fatalf("no game binary at %s: %v", binary, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("%s is not executable (mode %o)", binary, info.Mode().Perm())
		}
		if _, err := os.Stat(filepath.Join(dir, "assets", "blocks.toml")); err != nil {
			t.Errorf("assets/blocks.toml missing: %v", err)
		}

		// And a second check now reports it as playable and up to date.
		status := updates.Check()
		if !status.Playable {
			t.Error("should be playable after installing")
		}
		if status.UpdateAvailable {
			t.Error("should be up to date immediately after installing")
		}
	})

	t.Run("signing out clears the session", func(t *testing.T) {
		if result := auth.Logout(); result.Error != "" {
			t.Fatalf("logout: %s", result.Error)
		}
		stored, _ := profile.Read(core.Layout.ProfileFile())
		if stored.Account != nil {
			t.Error("profile.toml still holds an account")
		}
	})
}
