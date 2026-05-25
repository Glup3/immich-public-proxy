package story

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/glup3/immich-public-proxy/internal/config"
	"github.com/glup3/immich-public-proxy/internal/immich"
)

type StoryAsset struct {
	Asset       immich.Asset
	Caption     string
	IsHighlight bool
	PlaceLabel  string
	DayTitle    string
	DayKey      string
	HasLocation bool
	Time        time.Time
}

type TimelineSection struct {
	DayKey    string
	Title     string
	DateLabel string
	Items     []StoryAsset
}

type MapStop struct {
	ID        string
	Label     string
	Latitude  float64
	Longitude float64
	AssetIDs  []string
	DayKeys   []string
}

type Summary struct {
	StartDate   string
	EndDate     string
	LastUpdated string
	DayCount    int
	AssetCount  int
	PhotoCount  int
	VideoCount  int
	StopCount   int
}

type stopAccumulator struct {
	id        string
	label     string
	latSum    float64
	lngSum    float64
	count     int
	assetIDs  []string
	dayKeys   []string
	seenDays  map[string]struct{}
	firstTime time.Time
}

type Story struct {
	Intro      string
	Status     string
	TripLabel  string
	Assets     []StoryAsset
	Highlights []StoryAsset
	Timeline   []TimelineSection
	MapStops   []MapStop
	Summary    Summary
}

func Build(cfg config.TravelModeConfig, share *immich.SharedLink) Story {
	assets := make([]StoryAsset, 0, len(share.Assets))
	dayTitles := map[string]string{}
	for _, asset := range share.Assets {
		storyAsset := buildAsset(cfg, asset)
		if storyAsset.DayKey != "" && storyAsset.DayTitle != "" && dayTitles[storyAsset.DayKey] == "" {
			dayTitles[storyAsset.DayKey] = storyAsset.DayTitle
		}
		assets = append(assets, storyAsset)
	}

	timeline := buildTimeline(assets, dayTitles)
	highlights := make([]StoryAsset, 0)
	if cfg.ShowHighlights {
		for _, asset := range assets {
			if asset.IsHighlight {
				highlights = append(highlights, asset)
			}
		}
	}

	stops := buildStops(cfg, assets)
	summary := buildSummary(cfg, assets, stops)
	intro, status, tripLabel := parseAlbumDescription(share.Album)

	return Story{
		Intro:      intro,
		Status:     status,
		TripLabel:  tripLabel,
		Assets:     assets,
		Highlights: highlights,
		Timeline:   timeline,
		MapStops:   stops,
		Summary:    summary,
	}
}

func buildAsset(cfg config.TravelModeConfig, asset immich.Asset) StoryAsset {
	caption, isHighlight, placeLabel, dayTitle := ParseDescription(assetDescription(asset), cfg)
	ts := assetTime(asset)
	dayKey := ""
	if !ts.IsZero() {
		dayKey = ts.Format("2006-01-02")
	}
	return StoryAsset{
		Asset:       asset,
		Caption:     caption,
		IsHighlight: isHighlight,
		PlaceLabel:  placeLabel,
		DayTitle:    dayTitle,
		DayKey:      dayKey,
		HasLocation: asset.Latitude != nil && asset.Longitude != nil,
		Time:        ts,
	}
}

func ParseDescription(input string, cfg config.TravelModeConfig) (caption string, isHighlight bool, placeLabel string, dayTitle string) {
	lines := strings.Split(input, "\n")
	out := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case strings.EqualFold(line, cfg.HighlightTag):
			isHighlight = true
		case strings.HasPrefix(strings.ToLower(line), strings.ToLower(cfg.PlaceTagPrefix)):
			placeLabel = strings.TrimSpace(line[len(cfg.PlaceTagPrefix):])
		case strings.HasPrefix(strings.ToLower(line), strings.ToLower(cfg.DayTagPrefix)):
			dayTitle = strings.TrimSpace(line[len(cfg.DayTagPrefix):])
		case line != "":
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n\n")), isHighlight, placeLabel, dayTitle
}

func parseAlbumDescription(album *immich.SharedLinkAlbum) (intro, status, tripLabel string) {
	if album == nil {
		return "", "", ""
	}
	section := ""
	var introLines []string
	var statusLines []string
	var tripLines []string
	for _, raw := range strings.Split(album.Description, "\n") {
		line := strings.TrimSpace(raw)
		switch strings.ToLower(line) {
		case "#status":
			section = "status"
			continue
		case "#trip":
			section = "trip"
			continue
		}
		switch section {
		case "status":
			if line != "" {
				statusLines = append(statusLines, line)
			}
		case "trip":
			if line != "" {
				tripLines = append(tripLines, line)
			}
		default:
			if line != "" {
				introLines = append(introLines, line)
			}
		}
	}
	return strings.Join(introLines, "\n\n"), strings.Join(statusLines, " "), strings.Join(tripLines, " ")
}

func buildTimeline(assets []StoryAsset, dayTitles map[string]string) []TimelineSection {
	if len(assets) == 0 {
		return nil
	}
	sections := make([]TimelineSection, 0)
	indexByDay := map[string]int{}
	for _, asset := range assets {
		dayKey := asset.DayKey
		if dayKey == "" {
			dayKey = "undated"
		}
		index, ok := indexByDay[dayKey]
		if !ok {
			title := dayTitles[dayKey]
			sections = append(sections, TimelineSection{
				DayKey:    dayKey,
				Title:     title,
				DateLabel: formatDayLabel(dayKey),
			})
			index = len(sections) - 1
			indexByDay[dayKey] = index
		}
		sections[index].Items = append(sections[index].Items, asset)
		if sections[index].Title == "" && asset.DayTitle != "" {
			sections[index].Title = asset.DayTitle
		}
	}
	return sections
}

func buildSummary(cfg config.TravelModeConfig, assets []StoryAsset, stops []MapStop) Summary {
	summary := Summary{
		AssetCount: len(assets),
		StopCount:  len(stops),
	}
	daySet := map[string]struct{}{}
	var start time.Time
	var end time.Time
	for _, asset := range assets {
		switch asset.Asset.Type {
		case immich.AssetTypeVideo:
			summary.VideoCount++
		default:
			summary.PhotoCount++
		}
		if asset.DayKey != "" {
			daySet[asset.DayKey] = struct{}{}
		}
		if asset.Time.IsZero() {
			continue
		}
		if start.IsZero() || asset.Time.Before(start) {
			start = asset.Time
		}
		if end.IsZero() || asset.Time.After(end) {
			end = asset.Time
		}
	}
	summary.DayCount = len(daySet)
	if !start.IsZero() {
		summary.StartDate = start.Format("Jan 2, 2006")
	}
	if !end.IsZero() {
		summary.EndDate = end.Format("Jan 2, 2006")
		if cfg.ShowLastUpdated {
			summary.LastUpdated = end.Format("Jan 2, 2006")
		}
	}
	return summary
}

func buildStops(cfg config.TravelModeConfig, assets []StoryAsset) []MapStop {
	if !cfg.ShowMap || cfg.LocationPrecision == "none" {
		return nil
	}
	clusters := map[string]*stopAccumulator{}
	for _, asset := range assets {
		if !asset.HasLocation {
			continue
		}
		lat := *asset.Asset.Latitude
		lng := *asset.Asset.Longitude
		keyLat, keyLng := lat, lng
		if cfg.LocationPrecision == "approximate" {
			keyLat, keyLng = approximateLocation(lat, lng, cfg.ApproximateGridMeters)
		}
		clusterKey := fmt.Sprintf("%.5f,%.5f", keyLat, keyLng)
		acc := clusters[clusterKey]
		if acc == nil {
			acc = &stopAccumulator{
				id:       fmt.Sprintf("stop-%d", len(clusters)+1),
				seenDays: map[string]struct{}{},
			}
			clusters[clusterKey] = acc
		}
		acc.latSum += keyLat
		acc.lngSum += keyLng
		acc.count++
		acc.assetIDs = append(acc.assetIDs, asset.Asset.ID)
		if asset.DayKey != "" {
			if _, ok := acc.seenDays[asset.DayKey]; !ok {
				acc.dayKeys = append(acc.dayKeys, asset.DayKey)
				acc.seenDays[asset.DayKey] = struct{}{}
			}
		}
		if acc.label == "" && asset.PlaceLabel != "" {
			acc.label = asset.PlaceLabel
		}
		if acc.firstTime.IsZero() || (!asset.Time.IsZero() && asset.Time.Before(acc.firstTime)) {
			acc.firstTime = asset.Time
		}
	}
	stops := make([]MapStop, 0, len(clusters))
	for _, acc := range clusters {
		label := acc.label
		if label == "" {
			label = fmt.Sprintf("Stop %d", len(stops)+1)
		}
		stops = append(stops, MapStop{
			ID:        acc.id,
			Label:     label,
			Latitude:  acc.latSum / float64(acc.count),
			Longitude: acc.lngSum / float64(acc.count),
			AssetIDs:  acc.assetIDs,
			DayKeys:   acc.dayKeys,
		})
	}
	sort.SliceStable(stops, func(i, j int) bool {
		ii := clusters[keyForStop(clusters, stops[i])].firstTime
		jj := clusters[keyForStop(clusters, stops[j])].firstTime
		if ii.IsZero() {
			return false
		}
		if jj.IsZero() {
			return true
		}
		return ii.Before(jj)
	})
	for i := range stops {
		if strings.HasPrefix(stops[i].Label, "Stop ") {
			stops[i].Label = fmt.Sprintf("Stop %d", i+1)
		}
	}
	return stops
}

func keyForStop(clusters map[string]*stopAccumulator, stop MapStop) string {
	for key, acc := range clusters {
		if acc.id == stop.ID {
			return key
		}
	}
	return ""
}

func assetDescription(asset immich.Asset) string {
	if asset.ExifInfo != nil {
		return asset.ExifInfo.Description
	}
	return ""
}

func assetTime(asset immich.Asset) time.Time {
	for _, candidate := range []string{asset.LocalDateTime, asset.FileCreatedAt} {
		if candidate == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, candidate); err == nil {
			return ts
		}
	}
	return time.Time{}
}

func formatDayLabel(dayKey string) string {
	if dayKey == "undated" {
		return "Undated"
	}
	ts, err := time.Parse("2006-01-02", dayKey)
	if err != nil {
		return dayKey
	}
	return ts.Format("Monday, Jan 2")
}

func approximateLocation(lat, lng float64, meters int) (float64, float64) {
	if meters <= 0 {
		return lat, lng
	}
	latStep := float64(meters) / 111320.0
	cosLat := math.Cos(lat * math.Pi / 180.0)
	if cosLat == 0 {
		cosLat = 0.00001
	}
	lngStep := float64(meters) / (111320.0 * cosLat)
	return math.Round(lat/latStep) * latStep, math.Round(lng/lngStep) * lngStep
}
