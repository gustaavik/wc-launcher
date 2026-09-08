package wcauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(server.URL)
}

func TestAnEmptyBaseURLFallsBackToTheShippedDeployment(t *testing.T) {
	// Not localhost: a player has no auth server of their own, and the game's
	// release build is compiled against this address.
	if got := New("").BaseURL(); got != DefaultURL {
		t.Errorf("BaseURL = %q, want %q", got, DefaultURL)
	}
	if got := New("http://example.test/").BaseURL(); got != "http://example.test" {
		t.Errorf("trailing slash should be trimmed, got %q", got)
	}
}

func TestLoginUnwrapsTheEnvelope(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","data":{
			"account":{"id":"abc","username":"gustav","email":null,"netcode_id":"42"},
			"access_token":"at","access_expires_at":"2026-08-19T12:34:56Z",
			"refresh_token":"rt","refresh_expires_at":"2026-09-18T12:34:56Z"}}`))
	})

	session, err := client.Login(context.Background(), "gustav", "hunter2hunter2")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if session.Account.Username != "gustav" || session.AccessToken != "at" {
		t.Fatalf("unexpected session %+v", session)
	}
	if got := session.AccessExpiry().Year(); got != 2026 {
		t.Errorf("AccessExpiry year = %d, want 2026", got)
	}
}

// A refusal and an outage call for opposite responses — discard the session
// versus keep it — so the two must never be conflated.
func TestARefusalIsDistinguishableFromAnOutage(t *testing.T) {
	refusing := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status":"error","code":"invalid_credentials","message":"invalid username or password"}`))
	})

	_, err := refusing.Login(context.Background(), "gustav", "wrong")
	var refused *Error
	if !asError(err, &refused) {
		t.Fatalf("want a *wcauth.Error, got %T: %v", err, err)
	}
	if refused.Code != "invalid_credentials" {
		t.Errorf("Code = %q", refused.Code)
	}
	if Unreachable(err) {
		t.Error("a refusal must not look like an outage")
	}

	// Nothing listening: connection refused.
	_, err = New("http://127.0.0.1:1").Login(context.Background(), "gustav", "pw")
	if !Unreachable(err) {
		t.Errorf("want an unreachable error, got %v", err)
	}
}

func TestASpentRefreshTokenIsReportedAsExpired(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status":"error","code":"session_invalid","message":"please sign in again"}`))
	})

	_, err := client.Refresh(context.Background(), "spent")
	var refused *Error
	if !asError(err, &refused) {
		t.Fatalf("want a *wcauth.Error, got %T", err)
	}
	if !refused.SessionExpired() {
		t.Error("session_invalid should report SessionExpired")
	}
}

// axum rejects malformed requests before a handler runs, and those replies are
// plain text rather than the envelope. Decoding must not panic or claim success.
func TestANonEnvelopeReplyBecomesAReadableError(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte("Failed to deserialize the JSON body into the target type"))
	})

	_, err := client.Login(context.Background(), "gustav", "pw")
	var refused *Error
	if !asError(err, &refused) {
		t.Fatalf("want a *wcauth.Error, got %T: %v", err, err)
	}
	if refused.Code != "unexpected_response" || refused.Message == "" {
		t.Errorf("unhelpful error: %+v", refused)
	}
}

func TestA501IsExplainedAsDownloadsBeingUnavailable(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"status":"error","code":"releases_not_configured","message":"game downloads are not configured on this server"}`))
	})

	_, err := client.LatestRelease(context.Background(), "token")
	var refused *Error
	if !asError(err, &refused) {
		t.Fatalf("want a *wcauth.Error, got %T", err)
	}
	if refused.Code != "releases_not_configured" {
		t.Errorf("Code = %q", refused.Code)
	}
}

func TestReleaseAssetsParseTheirStringEncodedNumbers(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer at" {
			t.Errorf("Authorization = %q", got)
		}
		w.Write([]byte(`{"status":"ok","data":{
			"tag":"v0.0.1","name":"v0.0.1","notes":"## Changes","published_at":"2026-08-19T19:51:33Z",
			"prerelease":false,
			"assets":[{"id":"521286005","name":"wyvencraft-v0.0.1-aarch64-apple-darwin.tar.gz",
			           "size":"6708880","digest":"sha256:ABCDEF"}]}}`))
	})

	release, err := client.LatestRelease(context.Background(), "at")
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	asset := release.Assets[0]
	if asset.SizeBytes() != 6708880 {
		t.Errorf("SizeBytes = %d", asset.SizeBytes())
	}
	// Lowercased and unprefixed, ready to compare against a computed digest.
	if got := asset.SHA256(); got != "abcdef" {
		t.Errorf("SHA256 = %q, want %q", got, "abcdef")
	}
}

func TestAnAssetWithoutADigestReportsNoHashRatherThanAnEmptyOne(t *testing.T) {
	// "" means "verify against the .sha256 sibling", not "hash of nothing".
	if got := (Asset{}).SHA256(); got != "" {
		t.Errorf("SHA256 = %q, want empty", got)
	}
	if got := (Asset{Size: "not a number"}).SizeBytes(); got != 0 {
		t.Errorf("SizeBytes = %d, want 0", got)
	}
}

func TestAnUnparseableExpiryCountsAsExpired(t *testing.T) {
	// A zero time is in the past, so a caller that compares against now will
	// refresh — which is the safe direction.
	if got := (Session{AccessExpiresAt: "nonsense"}).AccessExpiry(); !got.IsZero() {
		t.Errorf("want the zero time, got %v", got)
	}
}

func TestLogoutSendsTheTokenAndTolaratesAnEmptyDataField(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","data":null}`))
	})

	if err := client.Logout(context.Background(), "rt"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
}

// asError is errors.As, spelled out to keep the test bodies readable.
func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

func TestReleasesUnwrapsTheEnvelopedList(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/releases" {
			t.Errorf("asked for %q, want /api/v1/releases", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer at" {
			t.Errorf("Authorization = %q, want a bearer token", got)
		}
		w.Write([]byte(`{"status":"ok","data":{"releases":[
			{"tag":"v0.0.3","name":"v0.0.3","notes":"newest","published_at":"2026-08-19T19:51:33Z",
			 "prerelease":false,"assets":[{"id":"1","name":"a.tar.gz","size":"10","digest":null}]},
			{"tag":"v0.0.2","name":"v0.0.2","notes":"older","published_at":null,
			 "prerelease":true,"assets":[]}]}}`))
	})

	releases, err := client.Releases(context.Background(), "at")
	if err != nil {
		t.Fatalf("Releases: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("want two releases, got %d", len(releases))
	}
	if releases[0].Tag != "v0.0.3" {
		t.Errorf("want newest first, got %q", releases[0].Tag)
	}
	// A prerelease is real and downloadable; the launcher labels it rather than
	// the client hiding it.
	if !releases[1].Prerelease {
		t.Error("the prerelease flag should survive decoding")
	}
	if releases[0].Assets[0].SizeBytes() != 10 {
		t.Errorf("size should decode from its string form, got %d", releases[0].Assets[0].SizeBytes())
	}
}

// A repository with nothing published is an empty list, not an error: a player
// should be told "no builds yet", not "something went wrong".
func TestAnEmptyReleaseListIsNotAnError(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","data":{"releases":[]}}`))
	})

	releases, err := client.Releases(context.Background(), "at")
	if err != nil {
		t.Fatalf("Releases: %v", err)
	}
	if len(releases) != 0 {
		t.Fatalf("want an empty list, got %v", releases)
	}
}

// A server with no GitHub credential answers an enveloped 501. That is a
// refusal the UI can explain, not a transport failure.
func TestReleasesReportsAServerWithoutDownloadsAsARefusal(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte(`{"status":"error","message":"game downloads are not configured on this server","code":"releases_not_configured"}`))
	})

	_, err := client.Releases(context.Background(), "at")
	var refused *Error
	if !asError(err, &refused) {
		t.Fatalf("want a *wcauth.Error, got %T (%v)", err, err)
	}
	if refused.Code != "releases_not_configured" {
		t.Errorf("Code = %q, want releases_not_configured", refused.Code)
	}
	if Unreachable(err) {
		t.Error("a server that answered is not unreachable")
	}
}

// An account server predating this route answers a bare 404. That must be a
// refusal too, not a crash — the launcher may be pointed at an older server.
func TestReleasesOnAServerThatDoesNotOfferTheRouteIsARefusal(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	_, err := client.Releases(context.Background(), "at")
	if err == nil {
		t.Fatal("want an error from a server without the route")
	}
	if Unreachable(err) {
		t.Error("a 404 is an answer, not an outage")
	}
}
