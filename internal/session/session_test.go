package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionPasswordRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	manager := NewForTest([]byte("test-secret"), func() time.Time { return now }, DefaultCookieOptions(), nil)

	req := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	rec := httptest.NewRecorder()
	if err := manager.SetPassword(rec, req, "key", "secret"); err != nil {
		t.Fatal(err)
	}

	next := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	next.AddCookie(rec.Result().Cookies()[0])
	if got := manager.Password(next, "key"); got != "secret" {
		t.Fatalf("expected password restored, got %q", got)
	}
}

func TestSessionExpiredPasswordIgnored(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	manager := NewForTest([]byte("test-secret"), func() time.Time { return now }, DefaultCookieOptions(), nil)

	req := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	rec := httptest.NewRecorder()
	if err := manager.SetPassword(rec, req, "key", "secret"); err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Hour)
	next := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	next.AddCookie(rec.Result().Cookies()[0])
	if got := manager.Password(next, "key"); got != "" {
		t.Fatalf("expected expired password ignored, got %q", got)
	}
}

func TestSessionTamperedCookieIgnored(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	manager := NewForTest([]byte("test-secret"), func() time.Time { return now }, DefaultCookieOptions(), nil)

	req := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	rec := httptest.NewRecorder()
	if err := manager.SetPassword(rec, req, "key", "secret"); err != nil {
		t.Fatal(err)
	}

	cookie := rec.Result().Cookies()[0]
	cookie.Value += "tamper"
	next := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	next.AddCookie(cookie)
	if got := manager.Password(next, "key"); got != "" {
		t.Fatalf("expected tampered cookie ignored, got %q", got)
	}
}
