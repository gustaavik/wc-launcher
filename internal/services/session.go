// Package services holds the three objects the frontend can call.
//
// They are the only layer that knows about Wails. Everything below is ordinary
// Go, testable without a window.
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gustaavik/wc-launcher/internal/paths"
	"github.com/gustaavik/wc-launcher/internal/profile"
	"github.com/gustaavik/wc-launcher/internal/wcauth"
)

// refreshMargin is how far ahead of expiry a token is renewed. Matches the
// game's own REFRESH_MARGIN_SECS, so the two agree on when a token is stale.
const refreshMargin = 60 * time.Second

// ErrSignedOut means there is no session to work with.
var ErrSignedOut = errors.New("not signed in")

// ErrGameRunning means an operation was refused because the game is running.
//
// This is the launcher's half of a rule that has to hold across two processes:
// refresh-token rotation is single-use, and the game refreshes the same family.
// Two refreshes racing look like reuse to the server, which revokes every
// session for the account — signing the player out everywhere. So while the
// game is up, the launcher touches no token at all.
var ErrGameRunning = errors.New("Wyvencraft is running; the game manages the session while it plays")

// Session owns the launcher's copy of the signed-in state.
//
// The refresh token is not stored here or anywhere else the launcher owns: it
// lives in the game's profile.toml, which is the single copy both processes
// read and write. That is what makes it possible for the game to rotate the
// token while it runs and for the launcher to pick up the result afterwards.
type Session struct {
	mu      sync.Mutex
	layout  paths.Layout
	client  *wcauth.Client
	current *wcauth.Session
	// gameRunning is supplied by the runner; see ErrGameRunning.
	gameRunning func() bool
}

func NewSession(layout paths.Layout, client *wcauth.Client, gameRunning func() bool) *Session {
	return &Session{layout: layout, client: client, gameRunning: gameRunning}
}

// SetClient swaps the account server, for when the URL setting changes.
func (s *Session) SetClient(client *wcauth.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = client
	s.current = nil
}

// Account returns who is signed in, or nil.
func (s *Session) Account() *wcauth.Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil
	}
	account := s.current.Account
	return &account
}

// StoredUsername is the name in profile.toml, if any.
//
// Used to prefill the sign-in form after a session expires, so the player types
// one field instead of two.
func (s *Session) StoredUsername() string {
	stored, err := profile.Read(s.layout.ProfileFile())
	if err != nil || stored.Account == nil {
		return ""
	}
	return stored.Account.Username
}

// Restore signs in from the token in profile.toml.
//
// Returns ErrSignedOut when there is nothing stored. A refusal clears the token
// (it is dead); an outage keeps it (a network blip must not sign anyone out) —
// the same asymmetry the game applies at boot.
func (s *Session) Restore(ctx context.Context) (*wcauth.Account, error) {
	stored, err := profile.Read(s.layout.ProfileFile())
	if err != nil {
		return nil, err
	}
	if stored.Account == nil {
		return nil, ErrSignedOut
	}
	return s.refreshWith(ctx, stored.Account.RefreshToken)
}

// Login exchanges credentials for a session and persists it.
func (s *Session) Login(ctx context.Context, username, password string) (*wcauth.Account, error) {
	if s.gameRunning != nil && s.gameRunning() {
		return nil, ErrGameRunning
	}

	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	session, err := client.Login(ctx, username, password)
	if err != nil {
		return nil, err
	}
	if err := s.adopt(session); err != nil {
		return nil, err
	}
	return s.Account(), nil
}

// Logout revokes the session and forgets it.
func (s *Session) Logout(ctx context.Context) error {
	if s.gameRunning != nil && s.gameRunning() {
		return ErrGameRunning
	}

	stored, _ := profile.Read(s.layout.ProfileFile())

	s.mu.Lock()
	client := s.client
	s.current = nil
	s.mu.Unlock()

	// Cleared first. If revocation fails — the server is down, say — the player
	// still expects to be signed out locally, and a token nobody holds is
	// harmless.
	if err := profile.StoreAccount(s.layout.ProfileFile(), nil); err != nil {
		return err
	}
	if stored.Account != nil {
		if err := client.Logout(ctx, stored.Account.RefreshToken); err != nil {
			// Worth logging, not worth failing: the local state is already gone.
			slog.Warn("could not revoke the session upstream", "error", err)
		}
	}
	return nil
}

// AccessToken returns a token good for the next few minutes, refreshing first
// if the current one is close to expiring.
//
// Every caller that needs to talk to an authenticated endpoint goes through
// here, which is what keeps refresh in one place.
func (s *Session) AccessToken(ctx context.Context) (string, error) {
	if s.gameRunning != nil && s.gameRunning() {
		return "", ErrGameRunning
	}

	s.mu.Lock()
	current := s.current
	s.mu.Unlock()

	if current == nil {
		return "", ErrSignedOut
	}
	if time.Until(current.AccessExpiry()) > refreshMargin {
		return current.AccessToken, nil
	}

	if _, err := s.refreshWith(ctx, current.RefreshToken); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current.AccessToken, nil
}

// Reload re-reads profile.toml and adopts whatever is there.
//
// Called after the game exits: it will have rotated the refresh token, so the
// launcher's in-memory copy is stale and the one on disk is authoritative.
func (s *Session) Reload(ctx context.Context) {
	s.mu.Lock()
	s.current = nil
	s.mu.Unlock()

	if _, err := s.Restore(ctx); err != nil && !errors.Is(err, ErrSignedOut) {
		slog.Warn("could not restore the session after the game exited", "error", err)
	}
}

// refreshWith rotates a refresh token and adopts the result.
func (s *Session) refreshWith(ctx context.Context, refreshToken string) (*wcauth.Account, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	session, err := client.Refresh(ctx, refreshToken)
	if err != nil {
		var refused *wcauth.Error
		if errors.As(err, &refused) && refused.SessionExpired() {
			// Dead for good. Keeping it would mean a doomed refresh on every
			// start, and the player has to sign in again either way.
			s.mu.Lock()
			s.current = nil
			s.mu.Unlock()
			if clearErr := profile.StoreAccount(s.layout.ProfileFile(), nil); clearErr != nil {
				slog.Warn("could not clear the expired session", "error", clearErr)
			}
		}
		return nil, err
	}
	if err := s.adopt(session); err != nil {
		return nil, err
	}
	return s.Account(), nil
}

// adopt persists a session, then holds it in memory.
//
// The order is the whole point. Rotation is destructive: by the time this is
// called the previous refresh token is already spent server-side. If the write
// were deferred and the process died first, the player would be signed out
// everywhere with nothing on disk to recover from.
func (s *Session) adopt(session wcauth.Session) error {
	err := profile.StoreAccount(s.layout.ProfileFile(), &profile.Account{
		AccountID:    session.Account.ID,
		Username:     session.Account.Username,
		RefreshToken: session.RefreshToken,
	})
	if err != nil {
		return fmt.Errorf("could not save the session: %w", err)
	}

	s.mu.Lock()
	s.current = &session
	s.mu.Unlock()
	return nil
}

// CacheKeys fetches the ticket-verification keys and writes authkeys.toml.
//
// Best-effort, and only relevant to hosting: a host without the file refuses
// every join. The game caches keys itself when it establishes a session, but a
// launcher-managed data directory may not have gone through that yet.
func (s *Session) CacheKeys(ctx context.Context) error {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()

	keys, err := client.Keys(ctx)
	if err != nil {
		return err
	}

	converted := make([]profile.Key, 0, len(keys))
	for _, key := range keys {
		converted = append(converted, profile.Key{ID: key.ID, PublicKey: key.PublicKey})
	}
	return profile.WriteKeys(s.layout.KeysFile(), converted)
}
