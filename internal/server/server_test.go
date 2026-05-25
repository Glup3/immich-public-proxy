package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glup3/immich-public-proxy/internal/config"
	"github.com/glup3/immich-public-proxy/internal/immich"
	"github.com/glup3/immich-public-proxy/internal/session"
)

const testAssetID = "123e4567-e89b-12d3-a456-426614174000"

func TestSlugDisabledReturnsNotFound(t *testing.T) {
	app := newTestApp(t, config.Default(), nil)
	app.config.IPP.AllowSlugLinks = false

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/s/custom-slug", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPasswordRequiredRendersPasswordPage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"password required"}`))
	}))
	defer upstream.Close()

	app := newTestApp(t, config.Default(), upstream)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/share/share-key", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Password required") {
		t.Fatal("expected password template")
	}
}

func TestGalleryRendersMetadataAndDownloadLink(t *testing.T) {
	cfg := config.Default()
	cfg.IPP.AllowDownloadAll = config.DownloadAllAlways
	cfg.IPP.ShowGalleryTitle = true
	upstream := sharedLinkServer(t, 2)
	defer upstream.Close()

	app := newTestApp(t, cfg, upstream)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/share/share-key", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, expected := range []string{"Gallery title", "/assets/style.css", "/share/share-key/download", "og:image"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected body to contain %q", expected)
		}
	}
}

func TestAssetProxyForwardsWhitelistedHeaders(t *testing.T) {
	upstream := sharedLinkServer(t, 1)
	defer upstream.Close()

	app := newTestApp(t, config.Default(), upstream)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/share/photo/share-key/"+testAssetID+"/thumbnail", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("expected content-type forwarded, got %q", got)
	}
	if rec.Body.String() != "asset-bytes" {
		t.Fatalf("expected proxied asset body, got %q", rec.Body.String())
	}
}

func TestVideoProxyPreservesUpstreamPartialResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/shared-links/me":
			_ = json.NewEncoder(w).Encode(immich.SharedLink{
				Key:  "share-key",
				Type: immich.AlbumTypeIndividual,
				Assets: []immich.Asset{{
					ID:               testAssetID,
					Type:             immich.AssetTypeVideo,
					OriginalFileName: "video.mp4",
				}},
			})
		case strings.HasSuffix(r.URL.Path, "/video/playback"):
			if got := r.Header.Get("Range"); got != "bytes=10-2500009" {
				t.Fatalf("expected chunked range, got %q", got)
			}
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Range", "bytes 10-2500009/9000000")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("video-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	app := newTestApp(t, config.Default(), upstream)
	req := httptest.NewRequest(http.MethodGet, "/share/video/share-key/"+testAssetID, nil)
	req.Header.Set("Range", "bytes=10-")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 10-2500009/9000000" {
		t.Fatalf("expected content-range forwarded, got %q", got)
	}
}

func TestDownloadAllReturnsZip(t *testing.T) {
	cfg := config.Default()
	cfg.IPP.AllowDownloadAll = config.DownloadAllAlways
	upstream := sharedLinkServer(t, 1)
	defer upstream.Close()

	app := newTestApp(t, cfg, upstream)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/share/share-key/download", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	reader, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != "photo.jpg" {
		t.Fatalf("unexpected zip files: %#v", reader.File)
	}
	file, _ := reader.File[0].Open()
	defer file.Close()
	data, _ := io.ReadAll(file)
	if string(data) != "original-bytes" {
		t.Fatalf("unexpected zip content: %q", string(data))
	}
}

func TestUnlockRejectsInvalidJSON(t *testing.T) {
	app := newTestApp(t, config.Default(), sharedLinkServer(t, 1))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/share/unlock", strings.NewReader(`{"key":`))
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestInvalidResponseUsesConfiguredStatus(t *testing.T) {
	cfg := config.Default()
	cfg.IPP.CustomInvalidResponse = config.InvalidResponseMode{StatusCode: http.StatusGone}

	app := newTestApp(t, cfg, nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/share/invalid/key", nil))
	if rec.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", rec.Code)
	}
}

func TestInvalidResponseUsesConfiguredRedirect(t *testing.T) {
	cfg := config.Default()
	cfg.IPP.CustomInvalidResponse = config.InvalidResponseMode{RedirectURL: "https://example.com"}

	app := newTestApp(t, cfg, nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/share/invalid/key", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	if location := rec.Header().Get("Location"); location != "https://example.com" {
		t.Fatalf("expected redirect location, got %q", location)
	}
}

func newTestApp(t *testing.T, cfg config.Config, upstream *httptest.Server) *Server {
	t.Helper()
	baseURL := "http://127.0.0.1:1"
	httpClient := http.DefaultClient
	if upstream != nil {
		baseURL = upstream.URL
		httpClient = upstream.Client()
	}
	sessions := session.NewForTest([]byte("test-secret"), func() time.Time {
		return time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	}, session.DefaultCookieOptions(), nil)
	app, err := New(Options{
		Config:   cfg,
		Client:   immich.NewClient(baseURL, httpClient, func() time.Time { return time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC) }, nil),
		Sessions: sessions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func sharedLinkServer(t *testing.T, assetCount int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/shared-links/me":
			w.Header().Set("Content-Type", "application/json")
			assets := []immich.Asset{{
				ID:               testAssetID,
				Type:             immich.AssetTypeImage,
				OriginalFileName: "photo.jpg",
				OriginalMimeType: "image/jpeg",
			}}
			if assetCount > 1 {
				assets = append(assets, immich.Asset{
					ID:               "123e4567-e89b-12d3-a456-426614174001",
					Type:             immich.AssetTypeImage,
					OriginalFileName: "photo2.jpg",
					OriginalMimeType: "image/jpeg",
				})
			}
			_ = json.NewEncoder(w).Encode(immich.SharedLink{
				Key:           "share-key",
				Type:          immich.AlbumTypeIndividual,
				Description:   "Gallery title",
				AllowDownload: true,
				Assets:        assets,
			})
		case strings.HasSuffix(r.URL.Path, "/thumbnail"):
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Length", "11")
			_, _ = w.Write([]byte("asset-bytes"))
		case strings.HasSuffix(r.URL.Path, "/original"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("original-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestRunStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, "127.0.0.1:0", http.NewServeMux(), nil); err != nil {
		t.Fatal(err)
	}
}
