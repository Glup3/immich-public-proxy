package session

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionPasswordRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	store := NewForTest([]byte("test-secret"), func() time.Time { return now }, DefaultCookieOptions(), nil)

	req := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	rec := httptest.NewRecorder()
	if err := store.RememberPassword(rec, req, "key", "secret"); err != nil {
		t.Fatal(err)
	}

	next := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	next.AddCookie(rec.Result().Cookies()[0])
	if got := store.PasswordForShare(next, "key"); got != "secret" {
		t.Fatalf("expected password restored, got %q", got)
	}
}

func TestSessionDoesNotExposePlaintextStateInCookie(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	store := NewForTest([]byte("test-secret"), func() time.Time { return now }, DefaultCookieOptions(), nil)

	req := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	rec := httptest.NewRecorder()
	if err := store.RememberPassword(rec, req, "share-key", "secret-password"); err != nil {
		t.Fatal(err)
	}

	cookie := rec.Result().Cookies()[0]
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"share-key", "secret-password"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("cookie payload should not expose %q", forbidden)
		}
	}
}

func TestSessionExpiredPasswordIgnored(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	store := NewForTest([]byte("test-secret"), func() time.Time { return now }, DefaultCookieOptions(), nil)

	req := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	rec := httptest.NewRecorder()
	if err := store.RememberPassword(rec, req, "key", "secret"); err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Hour)
	next := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	next.AddCookie(rec.Result().Cookies()[0])
	if got := store.PasswordForShare(next, "key"); got != "" {
		t.Fatalf("expected expired password ignored, got %q", got)
	}
}

func TestSessionTamperedCookieIgnored(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	store := NewForTest([]byte("test-secret"), func() time.Time { return now }, DefaultCookieOptions(), nil)

	req := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	rec := httptest.NewRecorder()
	if err := store.RememberPassword(rec, req, "key", "secret"); err != nil {
		t.Fatal(err)
	}

	cookie := rec.Result().Cookies()[0]
	cookie.Value += "tamper"
	next := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	next.AddCookie(cookie)
	if got := store.PasswordForShare(next, "key"); got != "" {
		t.Fatalf("expected tampered cookie ignored, got %q", got)
	}
}

func TestSessionClearKeyRemovesEntry(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	store := NewForTest([]byte("test-secret"), func() time.Time { return now }, DefaultCookieOptions(), nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	if err := store.RememberPassword(rec, req, "a", "one"); err != nil {
		t.Fatal(err)
	}

	next := httptest.NewRequest(http.MethodGet, "/", nil)
	next.AddCookie(rec.Result().Cookies()[0])
	rec2 := httptest.NewRecorder()
	if err := store.ForgetShare(rec2, next, "a"); err != nil {
		t.Fatal(err)
	}

	clearedCookie := rec2.Result().Cookies()[0]
	if clearedCookie.MaxAge != -1 {
		t.Fatalf("expected cookie deletion, got MaxAge=%d", clearedCookie.MaxAge)
	}
}

func TestSessionStoresMultipleKeys(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	store := NewForTest([]byte("test-secret"), func() time.Time { return now }, DefaultCookieOptions(), nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	if err := store.RememberPassword(rec, req, "a", "one"); err != nil {
		t.Fatal(err)
	}

	next := httptest.NewRequest(http.MethodGet, "/", nil)
	next.AddCookie(rec.Result().Cookies()[0])
	rec2 := httptest.NewRecorder()
	if err := store.RememberPassword(rec2, next, "b", "two"); err != nil {
		t.Fatal(err)
	}

	finalReq := httptest.NewRequest(http.MethodGet, "/", nil)
	finalReq.AddCookie(rec2.Result().Cookies()[0])
	if got := store.PasswordForShare(finalReq, "a"); got != "one" {
		t.Fatalf("expected key a password, got %q", got)
	}
	if got := store.PasswordForShare(finalReq, "b"); got != "two" {
		t.Fatalf("expected key b password, got %q", got)
	}
}

func TestSessionPrunesExpiredEntriesOnWrite(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	store := NewForTest([]byte("test-secret"), func() time.Time { return now }, DefaultCookieOptions(), nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	if err := store.RememberPassword(rec, req, "a", "one"); err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Hour)
	next := httptest.NewRequest(http.MethodGet, "/", nil)
	next.AddCookie(rec.Result().Cookies()[0])
	rec2 := httptest.NewRecorder()
	if err := store.RememberPassword(rec2, next, "b", "two"); err != nil {
		t.Fatal(err)
	}

	finalReq := httptest.NewRequest(http.MethodGet, "/", nil)
	finalReq.AddCookie(rec2.Result().Cookies()[0])
	state, err := store.read(finalReq)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Shares["a"]; ok {
		t.Fatal("expected expired key to be pruned")
	}
	if got := store.PasswordForShare(finalReq, "b"); got != "two" {
		t.Fatalf("expected key b password, got %q", got)
	}
}
