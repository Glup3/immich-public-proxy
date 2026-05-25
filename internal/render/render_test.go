package render

import (
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/glup3/immich-public-proxy/internal/config"
	"github.com/glup3/immich-public-proxy/internal/immich"
)

func TestSanitizeFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "keeps normal filename", input: "photo.jpg", want: "photo.jpg"},
		{name: "removes path separators", input: "../nested/path/photo.jpg", want: "..nestedpathphoto.jpg"},
		{name: "removes control characters", input: "bad\x00name\x1f.jpg", want: "badname.jpg"},
		{name: "removes windows reserved name", input: "con", want: ""},
		{name: "removes trailing windows characters", input: "photo. ", want: "photo"},
		{name: "truncates long names", input: strings.Repeat("a", 300), want: strings.Repeat("a", 254)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeFilename(tt.input); got != tt.want {
				t.Fatalf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAssetDimensionsSwapsExifOrientation(t *testing.T) {
	t.Parallel()

	width, height := assetDimensions(immich.Asset{
		ExifInfo: &immich.ExifInfo{
			ExifImageWidth:  1200,
			ExifImageHeight: 800,
			Orientation:     "6",
		},
	})
	if width != 800 || height != 1200 {
		t.Fatalf("unexpected dimensions: width=%d height=%d", width, height)
	}
}

func TestGalleryRendersPhotoSwipeInitPayload(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.IPP.AllowDownloadAll = config.DownloadAllAlways
	cfg.IPP.ShowGalleryTitle = true
	cfg.IPP.ShowGalleryDescription = true
	cfg.IPP.ShowMetadata.Description = true

	renderer, err := New(cfg, "https://example.test")
	if err != nil {
		t.Fatal(err)
	}

	share := &immich.SharedLink{
		Key:         "share-key",
		Description: "Gallery title",
		Album: &immich.SharedLinkAlbum{
			Description: "Album description",
		},
		Assets: []immich.Asset{
			{
				ID:               "a",
				Type:             immich.AssetTypeImage,
				OriginalFileName: "photo.jpg",
				OriginalMimeType: "image/jpeg",
				FileCreatedAt:    "2024-02-01T00:00:00Z",
				Thumbhash:        "thumbhash-value",
				ExifInfo: &immich.ExifInfo{
					Description:     `<script>alert(1)</script>`,
					ExifImageWidth:  1200,
					ExifImageHeight: 800,
					Orientation:     "6",
				},
			},
			{
				ID:               "b",
				Type:             immich.AssetTypeVideo,
				OriginalFileName: "video.mp4",
				OriginalMimeType: "video/mp4",
			},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/share/share-key", nil)
	if err := renderer.Gallery(rec, req, share, 1, true); err != nil {
		t.Fatal(err)
	}

	body := rec.Body.String()
	for _, expected := range []string{
		`id="gallery"`,
		`id="ipp-init"`,
		`type="module" src="/assets/web.js"`,
		`/assets/photoswipe/photoswipe.css`,
		`/assets/photoswipe-overrides.css`,
		`Album description`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected body to contain %q", expected)
		}
	}
	if strings.Contains(body, "lightgallery") {
		t.Fatalf("did not expect legacy lightgallery assets: %s", body)
	}

	initJSON := extractScriptJSON(t, body, "ipp-init")
	var payload struct {
		Items          []GalleryItem  `json:"items"`
		OpenItem       int            `json:"openItem"`
		LightboxConfig LightboxConfig `json:"lightboxConfig"`
		GroupByDate    bool           `json:"groupByDate"`
	}
	if err := json.Unmarshal([]byte(initJSON), &payload); err != nil {
		t.Fatalf("decode init payload: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(payload.Items))
	}
	if payload.OpenItem != 1 {
		t.Fatalf("expected openItem 1, got %d", payload.OpenItem)
	}
	if !payload.LightboxConfig.ShowDownload || !payload.LightboxConfig.ShowArrows || payload.LightboxConfig.MobileArrows {
		t.Fatalf("unexpected lightbox config: %#v", payload.LightboxConfig)
	}
	if payload.GroupByDate {
		t.Fatal("expected groupByDate false")
	}

	image := payload.Items[0]
	if image.ID != "a" || image.Width != 800 || image.Height != 1200 {
		t.Fatalf("unexpected image payload: %#v", image)
	}
	if image.Thumbhash != "thumbhash-value" {
		t.Fatalf("expected thumbhash, got %#v", image)
	}
	if image.Description != "&lt;script&gt;alert(1)&lt;/script&gt;" {
		t.Fatalf("expected escaped description, got %q", image.Description)
	}
	if image.DownloadURL != "/share/photo/share-key/a/original" {
		t.Fatalf("unexpected image download URL: %q", image.DownloadURL)
	}

	video := payload.Items[1]
	if video.VideoData == "" || video.DownloadURL != "/share/video/share-key/b" {
		t.Fatalf("unexpected video payload: %#v", video)
	}
}

func TestTravelRendersTimelineAndMap(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	renderer, err := New(cfg, "https://example.test")
	if err != nil {
		t.Fatal(err)
	}
	lat1, lng1 := 48.2082, 16.3738
	lat2, lng2 := 35.0116, 135.7681
	share := &immich.SharedLink{
		Key: "share-key",
		Album: &immich.SharedLinkAlbum{
			AlbumName:   "Japan Spring",
			Description: "Intro text\n\n#status\nIn Kyoto\n\n#trip\nSpring 2026",
		},
		Assets: []immich.Asset{
			{
				ID:               "a",
				Type:             immich.AssetTypeImage,
				OriginalFileName: "a.jpg",
				OriginalMimeType: "image/jpeg",
				LocalDateTime:    "2024-02-01T09:00:00+01:00",
				Latitude:         &lat1,
				Longitude:        &lng1,
				ExifInfo: &immich.ExifInfo{
					Description:     "Sunrise ferry\n#highlight\n#place: Vienna",
					ExifImageWidth:  1200,
					ExifImageHeight: 800,
				},
			},
			{
				ID:               "b",
				Type:             immich.AssetTypeVideo,
				OriginalFileName: "b.mp4",
				OriginalMimeType: "video/mp4",
				LocalDateTime:    "2024-02-02T09:00:00+09:00",
				Latitude:         &lat2,
				Longitude:        &lng2,
				ExifInfo:         &immich.ExifInfo{Description: "#day: Kyoto Day\nTemple walk"},
			},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/share/share-key", nil)
	if err := renderer.Travel(rec, req, share, true); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	for _, expected := range []string{"Japan Spring", "Last updated", "Highlights", "Route", "Approximate locations only.", "Kyoto Day", `id="gallery"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected body to contain %q", expected)
		}
	}
	initJSON := extractScriptJSON(t, body, "ipp-init")
	var payload struct {
		PageType string        `json:"pageType"`
		Items    []GalleryItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(initJSON), &payload); err != nil {
		t.Fatalf("decode init payload: %v", err)
	}
	if payload.PageType != "travel" || len(payload.Items) != 2 {
		t.Fatalf("unexpected travel init payload: %#v", payload)
	}
}

func extractScriptJSON(t *testing.T, body string, id string) string {
	t.Helper()

	re := regexp.MustCompile(`<script[^>]*id="` + regexp.QuoteMeta(id) + `\"[^>]*>([\s\S]*?)</script>`)
	match := re.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("script %q not found in body: %s", id, body)
	}
	return strings.TrimSpace(match[1])
}
