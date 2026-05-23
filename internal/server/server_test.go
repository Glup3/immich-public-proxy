package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alangrainger/immich-public-proxy/internal/config"
	"github.com/alangrainger/immich-public-proxy/internal/immich"
	"github.com/alangrainger/immich-public-proxy/internal/session"
	"github.com/alangrainger/immich-public-proxy/internal/types"
)

const testAssetID = "123e4567-e89b-12d3-a456-426614174000"

func TestSlugDisabledReturnsNotFound(t *testing.T) {
	app := newTestApp(t, config.Default(), nil)
	app.Config.IPP.AllowSlugLinks = false

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
	cfg.IPP.AllowDownloadAll = types.DownloadAllAlways
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
	for _, expected := range []string{"Gallery title", "/share/static/style.css", "/share/share-key/download", "og:image"} {
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

func TestVideoProxyChunksRange(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/shared-links/me":
			_ = json.NewEncoder(w).Encode(types.SharedLink{
				Key:  "share-key",
				Type: types.AlbumTypeIndividual,
				Assets: []types.Asset{{
					ID:               testAssetID,
					Type:             types.AssetTypeVideo,
					OriginalFileName: "video.mp4",
				}},
			})
		case strings.HasSuffix(r.URL.Path, "/api/assets/"+testAssetID):
			_ = json.NewEncoder(w).Encode(types.Asset{ID: testAssetID})
		case strings.HasSuffix(r.URL.Path, "/video/playback"):
			if got := r.Header.Get("Range"); got != "bytes=10-2500009" {
				t.Fatalf("expected chunked range, got %q", got)
			}
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Range", "bytes 10-2500009/9000000")
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
	cfg.IPP.AllowDownloadAll = types.DownloadAllAlways
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

func newTestApp(t *testing.T, cfg config.Config, upstream *httptest.Server) *App {
	t.Helper()
	t.Chdir("../..")
	baseURL := "http://127.0.0.1:1"
	httpClient := http.DefaultClient
	if upstream != nil {
		baseURL = upstream.URL
		httpClient = upstream.Client()
	}
	sessions := session.NewForTest(bytesOf(1, 32), bytesOf(2, 32), func() time.Time {
		return time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	})
	app, err := New(cfg, &immich.Client{BaseURL: baseURL, HTTPClient: httpClient}, sessions)
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
			assets := []types.Asset{{
				ID:               testAssetID,
				Type:             types.AssetTypeImage,
				OriginalFileName: "photo.jpg",
				OriginalMimeType: "image/jpeg",
			}}
			if assetCount > 1 {
				assets = append(assets, types.Asset{
					ID:               "123e4567-e89b-12d3-a456-426614174001",
					Type:             types.AssetTypeImage,
					OriginalFileName: "photo2.jpg",
					OriginalMimeType: "image/jpeg",
				})
			}
			_ = json.NewEncoder(w).Encode(types.SharedLink{
				Key:           "share-key",
				Type:          types.AlbumTypeIndividual,
				Description:   "Gallery title",
				AllowDownload: true,
				Assets:        assets,
			})
		case strings.HasPrefix(r.URL.Path, "/api/assets/") && strings.Count(r.URL.Path, "/") == 3:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(types.Asset{ID: testAssetID})
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

func bytesOf(value byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = value
	}
	return out
}
