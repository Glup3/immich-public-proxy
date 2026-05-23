package immich

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alangrainger/immich-public-proxy/internal/types"
)

func TestBuildURLOmitsEmptyAndEncodes(t *testing.T) {
	client := &Client{}
	got := client.BuildURL("http://example.test/path", map[string]string{
		"key":      "abc 123",
		"password": "",
	})
	if got != "http://example.test/path?key=abc+123" {
		t.Fatalf("unexpected url: %s", got)
	}
}

func TestValidationHelpers(t *testing.T) {
	if !IsKey("abc_DEF-123") || IsKey("abc/123") {
		t.Fatal("key validation mismatch")
	}
	if !IsID("123e4567-e89b-12d3-a456-426614174000") || IsID("not-an-id") {
		t.Fatal("id validation mismatch")
	}
	if !IsImageSize(types.ImageSizeOriginal) || IsImageSize("large") {
		t.Fatal("image size validation mismatch")
	}
}

func TestGetShareByKeyFiltersAndSortsAlbum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/shared-links/me":
			_ = json.NewEncoder(w).Encode(types.SharedLink{
				Key:  "share-key",
				Type: types.AlbumTypeAlbum,
				Album: &types.SharedLinkAlbum{
					ID:    "album-id",
					Order: "asc",
				},
			})
		case "/api/albums/album-id":
			_ = json.NewEncoder(w).Encode(types.Album{
				ID: "album-id",
				Assets: []types.Asset{
					{ID: "b", Type: types.AssetTypeImage, FileCreatedAt: "2024-02-01T00:00:00Z"},
					{ID: "trashed", Type: types.AssetTypeImage, IsTrashed: true},
					{ID: "a", Type: types.AssetTypeImage, FileCreatedAt: "2024-01-01T00:00:00Z"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, HTTPClient: server.Client()}
	res := client.GetShareByKey("share-key", "pw", types.KeyTypeKey)
	if !res.Valid || res.Link == nil {
		t.Fatal("expected valid share")
	}
	if len(res.Link.Assets) != 2 {
		t.Fatalf("expected trashed asset filtered, got %d assets", len(res.Link.Assets))
	}
	if res.Link.Assets[0].ID != "a" || res.Link.Assets[1].ID != "b" {
		t.Fatalf("expected assets sorted asc, got %#v", res.Link.Assets)
	}
	if res.Link.Assets[0].Key != "share-key" || res.Link.Assets[0].Password != "pw" {
		t.Fatal("expected key/password populated on assets")
	}
}

func TestGetShareByKeyPasswordRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"password required"}`))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, HTTPClient: server.Client()}
	res := client.GetShareByKey("share-key", "", types.KeyTypeKey)
	if !res.Valid || !res.PasswordRequired {
		t.Fatalf("expected password required, got %#v", res)
	}
}
