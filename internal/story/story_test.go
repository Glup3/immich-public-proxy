package story

import (
	"testing"

	"github.com/glup3/immich-public-proxy/internal/config"
	"github.com/glup3/immich-public-proxy/internal/immich"
)

func TestParseDescription(t *testing.T) {
	cfg := config.Default().IPP.TravelMode
	cfg.ApproximateGridMeters = 20000
	caption, highlight, place, day := ParseDescription("Sunrise ferry\n#highlight\n#place: Miyanoura Port\n#day: Arrival", cfg)
	if caption != "Sunrise ferry" {
		t.Fatalf("unexpected caption %q", caption)
	}
	if !highlight || place != "Miyanoura Port" || day != "Arrival" {
		t.Fatalf("unexpected parse result highlight=%v place=%q day=%q", highlight, place, day)
	}
}

func TestBuildGroupsByLocalDayAndClustersStops(t *testing.T) {
	cfg := config.Default().IPP.TravelMode
	lat1, lng1 := 48.2082, 16.3738
	lat2, lng2 := 48.2082, 16.3738
	lat3, lng3 := 35.0116, 135.7681
	story := Build(cfg, &immich.SharedLink{
		Album: &immich.SharedLinkAlbum{
			Description: "Intro text\n\n#status\nOn the train\n\n#trip\nSpring trip",
		},
		Assets: []immich.Asset{
			{
				ID:            "a",
				Type:          immich.AssetTypeImage,
				LocalDateTime: "2024-02-01T09:00:00+01:00",
				Latitude:      &lat1,
				Longitude:     &lng1,
				ExifInfo:      &immich.ExifInfo{Description: "Morning\n#highlight\n#place: Vienna"},
			},
			{
				ID:            "b",
				Type:          immich.AssetTypeVideo,
				LocalDateTime: "2024-02-01T10:00:00+01:00",
				Latitude:      &lat2,
				Longitude:     &lng2,
			},
			{
				ID:            "c",
				Type:          immich.AssetTypeImage,
				LocalDateTime: "2024-02-02T11:00:00+09:00",
				Latitude:      &lat3,
				Longitude:     &lng3,
				ExifInfo:      &immich.ExifInfo{Description: "#day: Kyoto Day\nTemple walk"},
			},
		},
	})
	if story.Intro != "Intro text" || story.Status != "On the train" || story.TripLabel != "Spring trip" {
		t.Fatalf("unexpected album parsing: %#v", story)
	}
	if len(story.Timeline) != 2 {
		t.Fatalf("expected 2 timeline sections, got %d", len(story.Timeline))
	}
	if story.Timeline[1].Title != "Kyoto Day" {
		t.Fatalf("expected day title, got %q", story.Timeline[1].Title)
	}
	if len(story.Highlights) != 1 || story.Highlights[0].Asset.ID != "a" {
		t.Fatalf("unexpected highlights: %#v", story.Highlights)
	}
	if len(story.MapStops) != 2 {
		t.Fatalf("expected 2 clustered stops, got %d", len(story.MapStops))
	}
	if story.Summary.DayCount != 2 || story.Summary.AssetCount != 3 || story.Summary.PhotoCount != 2 || story.Summary.VideoCount != 1 {
		t.Fatalf("unexpected summary: %#v", story.Summary)
	}
}
