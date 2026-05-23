package immich

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildURLOmitsEmptyAndEncodes(t *testing.T) {
	got := buildURL("http://example.test/path", map[string]string{
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
	if normalizeImageSize(ImageSizeOriginal) != ImageSizeOriginal || normalizeImageSize("large") != ImageSizePreview {
		t.Fatal("image size normalization mismatch")
	}
}

func TestFetchSharedLinkFiltersAndSortsAlbum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/shared-links/me":
			_ = json.NewEncoder(w).Encode(SharedLink{
				Key:  "share-key",
				Type: AlbumTypeAlbum,
				Album: &SharedLinkAlbum{
					ID:    "album-id",
					Order: "asc",
				},
			})
		case "/api/albums/album-id":
			_ = json.NewEncoder(w).Encode(Album{
				ID: "album-id",
				Assets: []Asset{
					{ID: "b", Type: AssetTypeImage, FileCreatedAt: "2024-02-01T00:00:00Z"},
					{ID: "trashed", Type: AssetTypeImage, IsTrashed: true},
					{ID: "a", Type: AssetTypeImage, FileCreatedAt: "2024-01-01T00:00:00Z"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client(), nil, nil)
	link, access, err := client.FetchSharedLink(context.Background(), "share-key", "pw", KeyTypeKey)
	if err != nil {
		t.Fatal(err)
	}
	if access != ShareAccessGranted {
		t.Fatalf("expected granted access, got %v", access)
	}
	if len(link.Assets) != 2 {
		t.Fatalf("expected trashed asset filtered, got %d assets", len(link.Assets))
	}
	if link.Assets[0].ID != "a" || link.Assets[1].ID != "b" {
		t.Fatalf("expected assets sorted asc, got %#v", link.Assets)
	}
	if link.Assets[0].Key != "share-key" || link.Assets[0].Password != "pw" {
		t.Fatal("expected key/password populated on assets")
	}
}

func TestFetchSharedLinkPasswordRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"password required"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client(), nil, nil)
	_, access, err := client.FetchSharedLink(context.Background(), "share-key", "", KeyTypeKey)
	if err != nil {
		t.Fatal(err)
	}
	if access != ShareAccessPasswordRequired {
		t.Fatalf("expected password required, got %v", access)
	}
}

func TestFetchSharedLinkDecodesExifDateFields(t *testing.T) {
	lat := 48.2082
	lng := 16.3738
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SharedLink{
			Key:  "share-key",
			Type: AlbumTypeIndividual,
			Assets: []Asset{{
				ID:            "asset-id",
				Type:          AssetTypeImage,
				FileCreatedAt: "2024-02-01T00:00:00Z",
				LocalDateTime: "2024-01-31T20:00:00-04:00",
				Latitude:      &lat,
				Longitude:     &lng,
				ExifInfo: &ExifInfo{
					LocalDateTime:    "2024-01-31T20:00:00-04:00",
					DateTimeOriginal: "2024-01-31T20:00:00-04:00",
					TimeZone:         "-04:00",
					Latitude:         &lat,
					Longitude:        &lng,
				},
			}},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client(), nil, nil)
	link, access, err := client.FetchSharedLink(context.Background(), "share-key", "", KeyTypeKey)
	if err != nil {
		t.Fatal(err)
	}
	if access != ShareAccessGranted {
		t.Fatalf("expected granted access, got %v", access)
	}
	if len(link.Assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(link.Assets))
	}
	asset := link.Assets[0]
	if asset.LocalDateTime != "2024-01-31T20:00:00-04:00" {
		t.Fatalf("unexpected asset localDateTime: %q", asset.LocalDateTime)
	}
	if asset.Latitude == nil || *asset.Latitude != lat || asset.Longitude == nil || *asset.Longitude != lng {
		t.Fatalf("unexpected asset coordinates: lat=%v lng=%v", asset.Latitude, asset.Longitude)
	}
	if asset.ExifInfo == nil || asset.ExifInfo.LocalDateTime != "2024-01-31T20:00:00-04:00" || asset.ExifInfo.DateTimeOriginal != "2024-01-31T20:00:00-04:00" || asset.ExifInfo.TimeZone != "-04:00" || asset.ExifInfo.Latitude == nil || *asset.ExifInfo.Latitude != lat || asset.ExifInfo.Longitude == nil || *asset.ExifInfo.Longitude != lng {
		t.Fatalf("unexpected exif info: %#v", asset.ExifInfo)
	}
}
