package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionPasswordRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	manager := NewForTest(bytesOf(1, 32), bytesOf(2, 32), func() time.Time { return now })

	req := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	rec := httptest.NewRecorder()
	manager.SetPassword(rec, req, "key", "secret")

	next := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	next.AddCookie(rec.Result().Cookies()[0])
	if got := manager.Password(next, "key"); got != "secret" {
		t.Fatalf("expected password restored, got %q", got)
	}
}

func TestSessionExpiredPasswordIgnored(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	manager := NewForTest(bytesOf(1, 32), bytesOf(2, 32), func() time.Time { return now })

	req := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	rec := httptest.NewRecorder()
	manager.SetPassword(rec, req, "key", "secret")

	now = now.Add(2 * time.Hour)
	next := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	next.AddCookie(rec.Result().Cookies()[0])
	if got := manager.Password(next, "key"); got != "" {
		t.Fatalf("expected expired password ignored, got %q", got)
	}
}

func TestSessionTamperedCookieIgnored(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	manager := NewForTest(bytesOf(1, 32), bytesOf(2, 32), func() time.Time { return now })

	req := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	rec := httptest.NewRecorder()
	manager.SetPassword(rec, req, "key", "secret")

	cookie := rec.Result().Cookies()[0]
	cookie.Value += "tamper"
	next := httptest.NewRequest(http.MethodGet, "/share/key", nil)
	next.AddCookie(cookie)
	if got := manager.Password(next, "key"); got != "" {
		t.Fatalf("expected tampered cookie ignored, got %q", got)
	}
}

func bytesOf(value byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = value
	}
	return out
}
