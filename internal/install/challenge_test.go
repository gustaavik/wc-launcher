package install

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gustaavik/wc-launcher/internal/catalog"
)

// A CDN bot check can answer with any status. The 200 case is the dangerous
// one: the challenge page is written to disk as the archive, and the only
// thing that catches it is the checksum — which then reports a mismatch, so
// the player is told their download is corrupt when nothing was downloaded.
func TestACDNBotChallengeIsNotMistakenForTheArchive(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusForbidden, http.StatusServiceUnavailable} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("cf-mitigated", "challenge")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`<!DOCTYPE html><title>Just a moment...</title>`))
		}))
		defer server.Close()

		dst := filepath.Join(t.TempDir(), "download.part")
		err := Fetch(context.Background(), server.URL, dst, 10806763, nil)

		if err == nil {
			t.Errorf("HTTP %d: a challenge page was accepted as an archive", code)
			continue
		}
		if !errors.Is(err, catalog.ErrChallenged) {
			t.Errorf("HTTP %d: got %v, want ErrChallenged", code, err)
		}
		if _, statErr := os.Stat(dst); statErr == nil {
			t.Errorf("HTTP %d: the challenge page was left on disk to resume onto", code)
		}
	}
}

// An ordinary refusal must keep reading as one — most downloads that fail have
// nothing to do with a CDN.
func TestAnOrdinaryRefusalIsNotMistakenForAChallenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	err := Fetch(context.Background(), server.URL, filepath.Join(t.TempDir(), "a.part"), 0, nil)
	if errors.Is(err, catalog.ErrChallenged) {
		t.Errorf("a plain 404 was reported as a CDN challenge: %v", err)
	}
	if err == nil {
		t.Fatal("a 404 was accepted")
	}
}
