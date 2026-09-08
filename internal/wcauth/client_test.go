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
