package render

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
		{
			name:  "keeps normal filename",
			input: "photo.jpg",
			want:  "photo.jpg",
		},
		{
			name:  "removes path separators",
			input: "../nested/path/photo.jpg",
			want:  "..nestedpathphoto.jpg",
		},
		{
			name:  "removes control characters",
			input: "bad\x00name\x1f.jpg",
			want:  "badname.jpg",
		},
		{
			name:  "removes windows reserved name",
			input: "con",
			want:  "",
		},
		{
			name:  "removes trailing windows characters",
			input: "photo. ",
			want:  "photo",
		},
		{
			name:  "truncates long names",
			input: strings.Repeat("a", 300),
			want:  strings.Repeat("a", 254),
		},
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

func TestGroupDatePrefersExifLocalDateTime(t *testing.T) {
	t.Parallel()

	label, sortKey, ok := groupDate(immich.Asset{
		FileCreatedAt: "2024-04-11T10:00:00Z",
		ExifInfo: &immich.ExifInfo{
			LocalDateTime: "2024-04-10T23:30:00-05:00",
		},
	})
	if !ok || label != "2024-04-10" || sortKey != "2024-04-10" {
		t.Fatalf("unexpected group date: ok=%v label=%q sortKey=%q", ok, label, sortKey)
	}
}

func TestGroupDateFallsBackToFileCreatedAt(t *testing.T) {
	t.Parallel()

	label, _, ok := groupDate(immich.Asset{
		FileCreatedAt: "2024-04-11T10:00:00Z",
		ExifInfo: &immich.ExifInfo{
			LocalDateTime: "not-a-date",
		},
	})
	if !ok || label != "2024-04-11" {
		t.Fatalf("unexpected fallback group date: ok=%v label=%q", ok, label)
	}
}

func TestGroupDateUsesDateTimeOriginal(t *testing.T) {
	t.Parallel()

	label, _, ok := groupDate(immich.Asset{
		ExifInfo: &immich.ExifInfo{
			DateTimeOriginal: "2024-03-02T08:15:00Z",
		},
	})
	if !ok || label != "2024-03-02" {
		t.Fatalf("unexpected exif original date: ok=%v label=%q", ok, label)
	}
}

func TestBuildGalleryGroupsPreservesOrderAndAppendsUndated(t *testing.T) {
	t.Parallel()

	assets := []immich.Asset{
		{ID: "a", FileCreatedAt: "2024-02-01T00:00:00Z"},
		{ID: "b"},
		{ID: "c", FileCreatedAt: "2024-02-01T12:00:00Z"},
		{ID: "d", FileCreatedAt: "2024-02-02T00:00:00Z"},
	}
	items := []GalleryItem{
		{HTML: "a"},
		{HTML: "b"},
		{HTML: "c"},
		{HTML: "d"},
	}

	groups := buildGalleryGroups(assets, items)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0].Title != "2024-02-01" || len(groups[0].Items) != 2 || groups[0].Items[0].HTML != "a" || groups[0].Items[1].HTML != "c" {
		t.Fatalf("unexpected first group: %#v", groups[0])
	}
	if groups[1].Title != "2024-02-02" || len(groups[1].Items) != 1 || groups[1].Items[0].HTML != "d" {
		t.Fatalf("unexpected second group: %#v", groups[1])
	}
	if groups[2].Title != "Undated" || len(groups[2].Items) != 1 || groups[2].Items[0].HTML != "b" {
		t.Fatalf("unexpected undated group: %#v", groups[2])
	}
}

func TestGalleryRendersDateGroupsAndFlatItemsJSON(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	cfg := config.Default()
	renderer, err := New(cfg, "https://example.test")
	if err != nil {
		t.Fatal(err)
	}

	share := &immich.SharedLink{
		Key: "share-key",
		Assets: []immich.Asset{
			{ID: "a", Type: immich.AssetTypeImage, FileCreatedAt: "2024-02-01T00:00:00Z"},
			{ID: "b", Type: immich.AssetTypeImage},
			{ID: "c", Type: immich.AssetTypeImage, FileCreatedAt: "2024-02-02T00:00:00Z"},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/share/share-key", nil)
	if err := renderer.Gallery(rec, req, share, 0, false); err != nil {
		t.Fatal(err)
	}

	body := rec.Body.String()
	for _, want := range []string{"2024-02-01", "2024-02-02", "Undated"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q: %s", want, body)
		}
	}

	start := strings.Index(body, "lgallery.init(")
	if start == -1 {
		t.Fatalf("gallery init payload not found: %s", body)
	}
	start += len("lgallery.init(")
	end := strings.Index(body[start:], ")\n")
	if end == -1 {
		t.Fatalf("gallery init payload end not found: %s", body)
	}

	var payload struct {
		Items []GalleryItem `json:"items"`
	}
	if err := json.NewDecoder(bytes.NewBufferString(body[start : start+end])).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Items) != 3 {
		t.Fatalf("expected 3 flat items in payload, got %d", len(payload.Items))
	}
}
