package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serve points a client at a test server, mirroring wcauth's client_test.
func serve(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(server.URL)
}

// body is a well-formed catalogue with one release.
func body(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(index(release("v0.5.0", false)))
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return string(raw)
}

func TestIndexIsFetchedFromTheCataloguePath(t *testing.T) {
	var path string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(body(t)))
	})

	got, err := client.Index(context.Background())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if want := "/" + DefaultPrefix + "/index.json"; path != want {
		t.Errorf("requested %q, want %q", path, want)
	}
	if len(got.Releases) != 1 || got.Releases[0].Tag != "v0.5.0" {
		t.Errorf("Index = %+v, want one v0.5.0 release", got.Releases)
	}
}

// The same distinction wcauth and selfupdate both draw, for the same reason: a
// refusal is something to report and stop on, an outage is something to retry.
func TestARefusalIsDistinguishableFromAnOutage(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<Error><Code>NoSuchBucket</Code></Error>`))
	})

	_, err := client.Index(context.Background())
	if err == nil {
		t.Fatal("a 404 was accepted")
	}
	if Unreachable(err) {
		t.Error("a refusal was reported as an outage")
	}
	var refusal *Error
	if !errors.As(err, &refusal) {
		t.Fatalf("error is %T, want *catalog.Error", err)
	}
	if refusal.Status != http.StatusNotFound {
		t.Errorf("Status = %d, want 404", refusal.Status)
	}
	if refusal.Code != "NoSuchBucket" {
		t.Errorf("Code = %q, want NoSuchBucket", refusal.Code)
	}
}

func TestAnUnreachableHostIsAnOutage(t *testing.T) {
	// Nothing listens here, so this fails in the transport rather than at the
	// far end.
	_, err := New("http://127.0.0.1:1").Index(context.Background())
	if err == nil {
		t.Fatal("an unreachable host was accepted")
	}
	if !Unreachable(err) {
		t.Errorf("error is %v, want an outage", err)
	}
}

// A misconfigured bucket must not look like a launcher that is already up to
// date. This is the failure most likely to pass unnoticed.
func TestAnEmptyIndexIsARefusalNotAnUpToDateLauncher(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"schemaVersion":1,"releases":[]}`))
	})

	if _, err := client.Index(context.Background()); err == nil {
		t.Fatal("an empty catalogue was accepted as valid")
	}
}

func TestAMalformedIndexIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"not json", `<html>a proxy error page</html>`},
		{"no releases key", `{"schemaVersion":1}`},
		{"release with no tag", `{"schemaVersion":1,"releases":[{"name":"x"}]}`},
		{"asset with no checksum", `{"schemaVersion":1,"releases":[{"tag":"v1","assets":[{"name":"a","path":"game/v1/a","size":1}]}]}`},
		{"asset with no path", `{"schemaVersion":1,"releases":[{"tag":"v1","assets":[{"name":"a","size":1,"sha256":"ab"}]}]}`},
		{"negative size", `{"schemaVersion":1,"releases":[{"tag":"v1","assets":[{"name":"a","path":"game/v1/a","size":-1,"sha256":"ab"}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := serve(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			})
			if _, err := client.Index(context.Background()); err == nil {
				t.Errorf("%s was accepted", tc.name)
			}
		})
	}
}

// A newer publisher must not be silently misread by an older launcher.
func TestAnUnknownSchemaVersionIsRefused(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"schemaVersion":99,"releases":[{"tag":"v1","assets":[]}]}`))
	})
	if _, err := client.Index(context.Background()); err == nil {
		t.Fatal("an unknown schema version was accepted")
	}
}

func TestAssetURLJoinsThePathOntoTheConfiguredBase(t *testing.T) {
	client := New("https://mirror.example.com/")
	asset := Asset{Path: "game/v0.5.0/wyvencraft-v0.5.0-aarch64-apple-darwin.tar.gz"}

	want := "https://mirror.example.com/game/v0.5.0/wyvencraft-v0.5.0-aarch64-apple-darwin.tar.gz"
	if got := client.AssetURL(asset); got != want {
		t.Errorf("AssetURL = %q, want %q", got, want)
	}
}

// The index names paths, never hosts. An absolute URL in the feed would let a
// tampered catalogue point the downloader anywhere.
func TestAnAbsolutePathInTheIndexIsRefused(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"schemaVersion":1,"releases":[{"tag":"v1","assets":[` +
			`{"name":"a","path":"https://evil.example.com/a","size":1,"sha256":"ab"}]}]}`))
	})
	if _, err := client.Index(context.Background()); err == nil {
		t.Fatal("an absolute asset path was accepted")
	}
}

func TestATraversingPathInTheIndexIsRefused(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"schemaVersion":1,"releases":[{"tag":"v1","assets":[` +
			`{"name":"a","path":"game/../../etc/passwd","size":1,"sha256":"ab"}]}]}`))
	})
	if _, err := client.Index(context.Background()); err == nil {
		t.Fatal("a traversing asset path was accepted")
	}
}

func TestNewFallsBackToTheShippedDefault(t *testing.T) {
	if got := New("").BaseURL(); got != DefaultURL {
		t.Errorf("BaseURL = %q, want %q", got, DefaultURL)
	}
	if got := New("  https://mirror.example.com/  ").BaseURL(); got != "https://mirror.example.com" {
		t.Errorf("BaseURL = %q, want the trimmed form", got)
	}
}

// These strings reach a player, so they are asserted rather than assumed.
func TestARefusalIsPhrasedForAPlayer(t *testing.T) {
	for _, tc := range []struct {
		err  *Error
		want string
	}{
		{&Error{Status: 404, Code: "NoSuchBucket"}, "no game builds are published yet"},
		{&Error{Status: 404, Code: "NoSuchKey"}, "no game builds are published yet"},
		{&Error{Status: 403, Code: "AccessDenied"}, "the download server refused the request"},
		{&Error{Status: 502, Code: ""}, "the download server answered 502"},
	} {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("Error() for %s = %q, want %q", tc.err.Code, got, tc.want)
		}
	}
}

// The Latest profile carries no tag, and must not match a release by accident.
func TestFindNeverMatchesTheEmptyTag(t *testing.T) {
	if _, ok := index(release("v0.5.0", false)).Find(""); ok {
		t.Error("Find matched on an empty tag")
	}
}

// The catalogue is written by a jq pipeline in another repository, so nothing
// but this test would notice the day the two shapes drift apart. The fixture is
// real workflow output — see testdata/README.md.
func TestTheWorkflowsOwnOutputParses(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(raw)
	})

	got, err := client.Index(context.Background())
	if err != nil {
		t.Fatalf("the release workflow's index.json does not parse: %v", err)
	}

	// Newest first, prereleases included in the list but never chosen as latest.
	if got.Releases[0].Tag != "v0.6.0-rc.1" {
		t.Errorf("Releases[0] = %q, want the newest entry first", got.Releases[0].Tag)
	}
	latest, ok := got.Latest()
	if !ok {
		t.Fatal("Latest found nothing")
	}
	if latest.Tag != "v0.5.0" {
		t.Errorf("Latest = %q, want v0.5.0 — the prerelease must not be promoted", latest.Tag)
	}

	// Notes survive the round trip: the changelog pane renders these.
	if !strings.Contains(latest.Notes, "What's Changed") {
		t.Errorf("notes did not survive: %q", latest.Notes)
	}

	// And the asset an installer would actually reach for.
	asset := latest.Assets[0]
	if asset.Size == 0 || len(asset.SHA256) != 64 {
		t.Errorf("asset is not installable: %+v", asset)
	}
	if want := "https://s3.wyvencraft.com/" + asset.Path; New("").AssetURL(asset) != want {
		t.Errorf("AssetURL = %q, want %q", New("").AssetURL(asset), want)
	}
}
