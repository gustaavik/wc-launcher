package selfupdate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const latestBody = `{
  "tag_name": "v0.2.0",
  "name": "v0.2.0",
  "body": "## What's Changed\r\n* it updates itself now",
  "published_at": "2026-08-22T10:00:00Z",
  "prerelease": false,
  "assets": [
    {
      "name": "wc-launcher-v0.2.0-macos-universal.zip",
      "browser_download_url": "https://example.invalid/wc-launcher-v0.2.0-macos-universal.zip",
      "size": 12345678,
      "digest": "sha256:AABBCC"
    },
    {
      "name": "wc-launcher-v0.2.0-macos-universal.zip.sha256",
      "browser_download_url": "https://example.invalid/wc-launcher-v0.2.0-macos-universal.zip.sha256",
      "size": 96
    }
  ]
}`

func TestLatestReadsARelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/repos/" + Repo + "/releases/latest"; r.URL.Path != want {
			t.Errorf("asked for %q, want %q", r.URL.Path, want)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(latestBody))
	}))
	defer server.Close()

	release, err := NewClient(server.URL).Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}

	if release.Tag != "v0.2.0" {
		t.Errorf("Tag = %q, want v0.2.0", release.Tag)
	}
	if release.Notes == "" {
		t.Error("Notes is empty; the changelog would render blank")
	}
	if len(release.Assets) != 2 {
		t.Fatalf("got %d assets, want 2", len(release.Assets))
	}
	if got, want := release.Assets[0].URL, "https://example.invalid/wc-launcher-v0.2.0-macos-universal.zip"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
	if got, want := release.Assets[0].Size, int64(12345678); got != want {
		t.Errorf("Size = %d, want %d", got, want)
	}
	// Lowercased, and the "sha256:" prefix gone, so it can be compared against
	// a hex digest without either side having to normalise again.
	if got, want := release.Assets[0].SHA256(), "aabbcc"; got != want {
		t.Errorf("SHA256 = %q, want %q", got, want)
	}
	if got := release.Assets[1].SHA256(); got != "" {
		t.Errorf("an asset with no digest reported %q, want empty", got)
	}
}

func TestLatestRefusalsArePhrasedForAPlayer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"no release yet", http.StatusNotFound},
		{"rate limited", http.StatusForbidden},
		{"github is unwell", http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			}))
			defer server.Close()

			_, err := NewClient(server.URL).Latest(context.Background())
			if err == nil {
				t.Fatalf("status %d returned no error", tc.status)
			}

			var refused *Error
			if !errors.As(err, &refused) {
				t.Fatalf("got %T, want *Error", err)
			}
			if refused.Status != tc.status {
				t.Errorf("Status = %d, want %d", refused.Status, tc.status)
			}
			// A refusal is not an outage: the caller must be able to tell them
			// apart, because only one of them is worth retrying.
			if Unreachable(err) {
				t.Error("a refusal was reported as unreachable")
			}
		})
	}
}

func TestAnOutageIsNotARefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := server.URL
	server.Close() // nothing is listening now

	_, err := NewClient(base).Latest(context.Background())
	if err == nil {
		t.Fatal("a dead server returned no error")
	}
	if !Unreachable(err) {
		t.Errorf("got %v, want an ErrUnreachable", err)
	}
	var refused *Error
	if errors.As(err, &refused) {
		t.Error("an outage was reported as a refusal")
	}
}

func TestAReleaseWithNoTagIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"assets":[]}`))
	}))
	defer server.Close()

	if _, err := NewClient(server.URL).Latest(context.Background()); err == nil {
		t.Fatal("a tagless release was accepted; it would compare equal to nothing")
	}
}
