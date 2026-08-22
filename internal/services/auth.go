package services

import (
	"context"
	"errors"
	"strings"

	"github.com/gustaavik/wc-launcher/internal/config"
	"github.com/gustaavik/wc-launcher/internal/wcauth"
)

// AuthService is the frontend's view of signing in.
//
// There is no Register: accounts are created elsewhere, deliberately.
type AuthService struct{ core *Core }

func NewAuthService(core *Core) *AuthService { return &AuthService{core: core} }

// LoginResult is what the sign-in form gets back.
type LoginResult struct {
	Account *AccountView `json:"account"`
	// Error is empty on success. A message, not a code — the UI shows it.
	Error string `json:"error"`
}

// Restore signs in from the stored session, if there is one.
//
// Called once at startup. A result with no account and no error means "nobody
// was signed in", which is the normal first-run case rather than a failure.
func (a *AuthService) Restore() LoginResult {
	account, err := a.core.Session.Restore(context.Background())
	if err != nil {
		if errors.Is(err, ErrSignedOut) {
			return LoginResult{}
		}
		return LoginResult{Error: userMessage(err)}
	}

	view := toAccountView(account)
	a.core.emit("auth:changed", view)
	// Best-effort: only hosting needs it, and it must not delay sign-in.
	go logIfErr("could not cache the auth keys", a.core.Session.CacheKeys(context.Background()))
	return LoginResult{Account: view}
}

// Login signs in with a username and password.
func (a *AuthService) Login(username, password string) LoginResult {
	username = strings.TrimSpace(username)
	// Mirrors the server's rules so an obvious typo does not cost a round trip.
	// Deliberately not stricter: the server is the authority.
	if msg := validateCredentials(username, password); msg != "" {
		return LoginResult{Error: msg}
	}

	account, err := a.core.Session.Login(context.Background(), username, password)
	if err != nil {
		return LoginResult{Error: userMessage(err)}
	}

	view := toAccountView(account)
	a.core.emit("auth:changed", view)
	go logIfErr("could not cache the auth keys", a.core.Session.CacheKeys(context.Background()))
	return LoginResult{Account: view}
}

// Logout revokes the session.
func (a *AuthService) Logout() LoginResult {
	if err := a.core.Session.Logout(context.Background()); err != nil {
		return LoginResult{Error: userMessage(err)}
	}
	a.core.emit("auth:changed", nil)
	return LoginResult{}
}

// Account returns who is signed in, or null.
func (a *AuthService) Account() *AccountView {
	return toAccountView(a.core.Session.Account())
}

// StoredUsername prefills the form after a session expires.
func (a *AuthService) StoredUsername() string {
	return a.core.Session.StoredUsername()
}

// SettingsView is the launcher's own configuration, for the settings screen.
type SettingsView struct {
	AuthURL string `json:"authUrl"`
	// DefaultAuthURL lets the UI show what "empty" means.
	DefaultAuthURL string `json:"defaultAuthUrl"`
	LogFilter      string `json:"logFilter"`
	// DataDir and VersionsDir are shown read-only, so a player can find their
	// saves without being told where to look.
	DataDir     string `json:"dataDir"`
	VersionsDir string `json:"versionsDir"`
}

// Settings returns the current configuration.
func (a *AuthService) Settings() SettingsView {
	return SettingsView{
		AuthURL:        a.core.Settings.AuthURL,
		DefaultAuthURL: wcauth.DefaultURL,
		LogFilter:      a.core.Settings.LogFilter,
		DataDir:        a.core.Layout.Data,
		VersionsDir:    a.core.Layout.Versions,
	}
}

// SaveSettings persists the configuration and rebuilds the client.
//
// Changing the auth server invalidates the session: tokens are issued by one
// server and meaningless to another. So this signs the player out rather than
// leaving them apparently signed in to somewhere they are not.
func (a *AuthService) SaveSettings(authURL, logFilter string) LoginResult {
	if a.core.Runner.Running() {
		return LoginResult{Error: ErrGameRunning.Error()}
	}

	settings := config.Settings{
		AuthURL:   strings.TrimSpace(authURL),
		LogFilter: strings.TrimSpace(logFilter),
	}
	changed := settings.ResolvedAuthURL() != a.core.Settings.ResolvedAuthURL()

	if err := config.Save(a.core.Layout.SettingsFile(), settings); err != nil {
		return LoginResult{Error: err.Error()}
	}
	a.core.Settings = settings

	if !changed {
		return LoginResult{Account: a.Account()}
	}

	client := wcauth.New(settings.ResolvedAuthURL())
	a.core.Client = client
	a.core.Session.SetClient(client)
	a.core.Install = newInstaller(a.core)
	a.core.emit("auth:changed", nil)

	// The stored token belongs to the old server.
	return a.Restore()
}

// validateCredentials mirrors the server's own rules (wcauth-domain::username,
// wcauth-domain::password). Fails fast on input the server would reject anyway.
func validateCredentials(username, password string) string {
	switch {
	case username == "":
		return "Enter your username."
	case len(username) < 3 || len(username) > 16:
		return "A username is 3 to 16 characters."
	case username[0] == '_':
		return "A username cannot start with an underscore."
	}
	for _, r := range username {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isDigit := r >= '0' && r <= '9'
		if !isLetter && !isDigit && r != '_' {
			return "A username may only contain letters, digits and underscores."
		}
	}
	// Byte length, and not trimmed — matching the server exactly.
	switch {
	case password == "":
		return "Enter your password."
	case len(password) < 10:
		return "A password is at least 10 characters."
	case len(password) > 256:
		return "That password is too long."
	}
	return ""
}
