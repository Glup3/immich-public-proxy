package invalid

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alangrainger/immich-public-proxy/internal/config"
)

func TestRespondDefaultStatus(t *testing.T) {
	handler := New(config.Default())
	rec := httptest.NewRecorder()
	handler.Respond(rec, http.StatusNotFound, "test")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestRespondCustomStatus(t *testing.T) {
	cfg := config.Default()
	cfg.IPP.CustomInvalidResponse = 410
	handler := New(cfg)
	rec := httptest.NewRecorder()
	handler.Respond(rec, http.StatusNotFound, "test")
	if rec.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", rec.Code)
	}
}

func TestRespondRedirect(t *testing.T) {
	cfg := config.Default()
	cfg.IPP.CustomInvalidResponse = "https://example.com"
	handler := New(cfg)
	rec := httptest.NewRecorder()
	handler.Respond(rec, http.StatusNotFound, "test")
	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	if rec.Header().Get("Location") != "https://example.com" {
		t.Fatalf("unexpected location %q", rec.Header().Get("Location"))
	}
}
