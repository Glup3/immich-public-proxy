package render

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/glup3/immich-public-proxy/internal/config"
	"github.com/glup3/immich-public-proxy/internal/immich"
	"github.com/glup3/immich-public-proxy/internal/story"
)

var (
	illegalFilenameChars = regexp.MustCompile(`[/?<>\\:*|"]`)
	controlFilenameChars = regexp.MustCompile(`[\x00-\x1f\x80-\x9f]`)
	dotsOnlyFilename     = regexp.MustCompile(`^\.+$`)
	windowsReservedName  = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])(\..*)?$`)
	windowsTrailingChars = regexp.MustCompile(`[. ]+$`)
)

type Renderer struct {
	config        config.Config
	publicBaseURL string
}

type PasswordPageData struct {
	Key                   string
	NotifyInvalidPassword bool
}

type GalleryItem struct {
	ID               string           `json:"id"`
	Type             immich.AssetType `json:"type"`
	PreviewURL       string           `json:"previewUrl"`
	ThumbnailURL     string           `json:"thumbnailUrl"`
	DownloadURL      string           `json:"downloadUrl,omitempty"`
	VideoData        string           `json:"videoData,omitempty"`
	Description      string           `json:"description,omitempty"`
	DownloadFilename string           `json:"downloadFilename"`
	Width            int              `json:"width,omitempty"`
	Height           int              `json:"height,omitempty"`
	Thumbhash        string           `json:"thumbhash,omitempty"`
	FileCreatedAt    string           `json:"fileCreatedAt,omitempty"`
}

type LightboxConfig struct {
	ShowArrows   bool `json:"showArrows"`
	ShowDownload bool `json:"showDownload"`
	MobileArrows bool `json:"mobileArrows"`
}

type GalleryInitData struct {
	PageType       string         `json:"pageType,omitempty"`
	Items          []GalleryItem  `json:"items"`
	OpenItem       *int           `json:"openItem,omitempty"`
	LightboxConfig LightboxConfig `json:"lightboxConfig"`
	GroupByDate    bool           `json:"groupByDate"`
}

type GalleryPageData struct {
	InitData        GalleryInitData
	FirstPreviewURL string
	Title           string
	Description     string
	PublicBaseURL   string
	Path            string
	ShowDownload    bool
	ShowTitle       bool
}

type TravelHighlight struct {
	Title        string
	Caption      string
	ThumbnailURL string
	AssetID      string
}

type TravelTimelineItem struct {
	AssetID      string
	Caption      string
	PlaceLabel   string
	ThumbnailURL string
	PreviewURL   string
	Video        bool
}

type TravelTimelineSection struct {
	Anchor    string
	DateLabel string
	Title     string
	Items     []TravelTimelineItem
}

type TravelMapStop struct {
	ID        string
	Label     string
	Latitude  float64
	Longitude float64
	DayKey    string
	Anchor    string
}

type TravelSummaryChip struct {
	Label string
	Value string
}

type TravelPageData struct {
	InitData       GalleryInitData
	Title          string
	Intro          string
	Status         string
	TripLabel      string
	LastUpdated    string
	Path           string
	ShowDownload   bool
	Highlights     []TravelHighlight
	Timeline       []TravelTimelineSection
	MapStops       []TravelMapStop
	MapSVGPath     string
	SummaryChips   []TravelSummaryChip
	ShowMap        bool
	ShowHighlights bool
}

func New(cfg config.Config, publicBaseURL string) (*Renderer, error) {
	return &Renderer{
		config:        cfg,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}, nil
}

func (r *Renderer) Home(w http.ResponseWriter) error {
	r.config.AddResponseHeaders(w.Header())
	return homePage().Render(context.Background(), w)
}

func (r *Renderer) Password(w http.ResponseWriter, key string, notifyInvalidPassword bool) error {
	return passwordPage(PasswordPageData{
		Key:                   key,
		NotifyInvalidPassword: notifyInvalidPassword,
	}).Render(context.Background(), w)
}

func (r *Renderer) Gallery(w http.ResponseWriter, req *http.Request, share *immich.SharedLink, openItem int, showDownload bool) error {
	items := make([]GalleryItem, 0, len(share.Assets))
	for _, asset := range share.Assets {
		items = append(items, r.galleryItem(share, asset))
	}

	description := ""
	if r.config.IPP.ShowGalleryDescription {
		description = Description(share)
	}

	initData := GalleryInitData{
		PageType: "gallery",
		Items:    items,
		OpenItem: optionalOpenItem(openItem),
		LightboxConfig: LightboxConfig{
			ShowArrows:   true,
			ShowDownload: showDownload,
			MobileArrows: false,
		},
		GroupByDate: false,
	}
	if _, err := json.Marshal(initData); err != nil {
		return fmt.Errorf("marshal gallery payload: %w", err)
	}

	data := GalleryPageData{
		InitData:        initData,
		FirstPreviewURL: firstPreviewURL(items),
		Title:           Title(share),
		Description:     description,
		PublicBaseURL:   r.resolvePublicBaseURL(req),
		Path:            "/share/" + share.Key,
		ShowDownload:    showDownload,
		ShowTitle:       r.config.IPP.ShowGalleryTitle,
	}
	return galleryPage(data).Render(req.Context(), w)
}

func (r *Renderer) Travel(w http.ResponseWriter, req *http.Request, share *immich.SharedLink, showDownload bool) error {
	travel := story.Build(r.config.IPP.TravelMode, share)
	items := make([]GalleryItem, 0, len(share.Assets))
	itemByID := make(map[string]GalleryItem, len(share.Assets))
	for _, asset := range share.Assets {
		item := r.galleryItem(share, asset)
		items = append(items, item)
		itemByID[item.ID] = item
	}
	initData := GalleryInitData{
		PageType: "travel",
		Items:    items,
		LightboxConfig: LightboxConfig{
			ShowArrows:   true,
			ShowDownload: showDownload,
			MobileArrows: false,
		},
		GroupByDate: false,
	}
	if _, err := json.Marshal(initData); err != nil {
		return fmt.Errorf("marshal travel payload: %w", err)
	}
	data := TravelPageData{
		InitData:       initData,
		Title:          Title(share),
		Intro:          travel.Intro,
		Status:         travel.Status,
		TripLabel:      travel.TripLabel,
		LastUpdated:    travel.Summary.LastUpdated,
		Path:           "/share/" + share.Key,
		ShowDownload:   showDownload,
		ShowMap:        len(travel.MapStops) > 0,
		ShowHighlights: len(travel.Highlights) > 0,
		SummaryChips:   buildSummaryChips(travel.Summary),
	}
	for _, asset := range travel.Highlights {
		item := itemByID[asset.Asset.ID]
		title := asset.PlaceLabel
		if title == "" {
			title = formatTravelItemTitle(asset)
		}
		data.Highlights = append(data.Highlights, TravelHighlight{
			Title:        title,
			Caption:      asset.Caption,
			ThumbnailURL: item.ThumbnailURL,
			AssetID:      item.ID,
		})
	}
	for _, section := range travel.Timeline {
		out := TravelTimelineSection{
			Anchor:    timelineAnchor(section.DayKey),
			DateLabel: section.DateLabel,
			Title:     section.Title,
		}
		for _, asset := range section.Items {
			item := itemByID[asset.Asset.ID]
			out.Items = append(out.Items, TravelTimelineItem{
				AssetID:      asset.Asset.ID,
				Caption:      asset.Caption,
				PlaceLabel:   asset.PlaceLabel,
				ThumbnailURL: item.ThumbnailURL,
				PreviewURL:   item.PreviewURL,
				Video:        asset.Asset.Type == immich.AssetTypeVideo,
			})
		}
		data.Timeline = append(data.Timeline, out)
	}
	for _, stop := range travel.MapStops {
		anchor := ""
		if len(stop.DayKeys) > 0 {
			anchor = timelineAnchor(stop.DayKeys[0])
		}
		data.MapStops = append(data.MapStops, TravelMapStop{
			ID:        stop.ID,
			Label:     stop.Label,
			Latitude:  stop.Latitude,
			Longitude: stop.Longitude,
			DayKey:    firstOrEmpty(stop.DayKeys),
			Anchor:    anchor,
		})
	}
	data.MapSVGPath = mapSVGPath(data.MapStops)
	return renderTravelPage(w, data)
}

func (r *Renderer) galleryItem(share *immich.SharedLink, asset immich.Asset) GalleryItem {
	var videoData string
	downloadURL := ""
	if asset.Type == immich.AssetTypeVideo {
		mimeType := asset.OriginalMimeType
		if mimeType == "" {
			mimeType = "video/mp4"
		}
		video, err := json.Marshal(struct {
			Source []struct {
				Src  string `json:"src"`
				Type string `json:"type"`
			} `json:"source"`
			Attrs struct {
				PlaysInline string `json:"playsinline"`
				Controls    string `json:"controls"`
			} `json:"attributes"`
		}{
			Source: []struct {
				Src  string `json:"src"`
				Type string `json:"type"`
			}{{
				Src:  videoURL(share.Key, asset.ID),
				Type: mimeType,
			}},
			Attrs: struct {
				PlaysInline string `json:"playsinline"`
				Controls    string `json:"controls"`
			}{
				PlaysInline: "playsinline",
				Controls:    "controls",
			},
		})
		if err == nil {
			videoData = string(video)
		}
		downloadURL = videoURL(share.Key, asset.ID)
	}
	if asset.Type == immich.AssetTypeImage && r.config.IPP.DownloadOriginalPhoto {
		downloadURL = photoURL(share.Key, asset.ID, immich.ImageSizeOriginal)
	}

	description := ""
	if r.config.IPP.ShowMetadata.Description && asset.ExifInfo != nil {
		description = html.EscapeString(asset.ExifInfo.Description)
	}

	width, height := assetDimensions(asset)

	return GalleryItem{
		ID:               asset.ID,
		Type:             asset.Type,
		PreviewURL:       photoURL(share.Key, asset.ID, previewImageSize(asset)),
		ThumbnailURL:     photoURL(share.Key, asset.ID, immich.ImageSizeThumbnail),
		DownloadURL:      downloadURL,
		VideoData:        videoData,
		Description:      description,
		DownloadFilename: Filename(r.config, asset),
		Width:            width,
		Height:           height,
		Thumbhash:        asset.Thumbhash,
		FileCreatedAt:    asset.FileCreatedAt,
	}
}

func Title(share *immich.SharedLink) string {
	if share.Description != "" {
		return share.Description
	}
	if share.Album != nil && share.Album.AlbumName != "" {
		return share.Album.AlbumName
	}
	return "Gallery"
}

func Description(share *immich.SharedLink) string {
	if share.Album != nil {
		return share.Album.Description
	}
	return ""
}

func firstPreviewURL(items []GalleryItem) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].PreviewURL
}

func Filename(cfg config.Config, asset immich.Asset) string {
	extension := filepath.Ext(asset.OriginalFileName)
	switch cfg.IPP.DownloadedFilename {
	case config.DownloadedFilenameAssetID:
		return asset.ID + extension
	case config.DownloadedFilenameShortID:
		prefix := asset.ID
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		return "img_" + prefix + extension
	default:
		if asset.OriginalFileName != "" {
			return sanitizeFilename(asset.OriginalFileName)
		}
		return asset.ID + extension
	}
}

func SafeTitleFilename(title string) string {
	return sanitizeFilename(title)
}

func CanDownload(cfg config.Config, share *immich.SharedLink) bool {
	switch cfg.IPP.AllowDownloadAll {
	case config.DownloadAllDisabled:
		return false
	case config.DownloadAllAlways:
		return true
	default:
		return share.AllowDownload
	}
}

func optionalOpenItem(openItem int) *int {
	if openItem > 0 {
		return &openItem
	}
	return nil
}

func buildSummaryChips(summary story.Summary) []TravelSummaryChip {
	var chips []TravelSummaryChip
	if summary.StartDate != "" && summary.EndDate != "" {
		chips = append(chips, TravelSummaryChip{Label: "Dates", Value: summary.StartDate + " to " + summary.EndDate})
	}
	if summary.DayCount > 0 {
		chips = append(chips, TravelSummaryChip{Label: "Days", Value: fmt.Sprintf("%d", summary.DayCount)})
	}
	if summary.AssetCount > 0 {
		chips = append(chips, TravelSummaryChip{Label: "Moments", Value: fmt.Sprintf("%d", summary.AssetCount)})
	}
	if summary.StopCount > 0 {
		chips = append(chips, TravelSummaryChip{Label: "Stops", Value: fmt.Sprintf("%d", summary.StopCount)})
	}
	return chips
}

func formatTravelItemTitle(asset story.StoryAsset) string {
	if !asset.Time.IsZero() {
		return asset.Time.Format("Mon Jan 2")
	}
	return "Highlight"
}

func timelineAnchor(dayKey string) string {
	if dayKey == "" {
		return "day-undated"
	}
	return "day-" + dayKey
}

func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func renderTravelPage(w http.ResponseWriter, data TravelPageData) error {
	const travelPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta name="viewport" content="width=device-width, initial-scale=1.0"/>
  <title>{{ .Title }}</title>
  <link rel="icon" href="/assets/favicon.ico" type="image/x-icon"/>
  <link type="text/css" rel="stylesheet" href="/assets/style.css"/>
  <link type="text/css" rel="stylesheet" href="/assets/photoswipe/photoswipe.css"/>
  <link type="text/css" rel="stylesheet" href="/assets/photoswipe-overrides.css"/>
</head>
<body class="travel-page">
  <main class="travel-shell">
    <header class="travel-hero">
      <div class="travel-hero-copy">
        {{ if .TripLabel }}<p class="travel-kicker">{{ .TripLabel }}</p>{{ end }}
        <h1>{{ .Title }}</h1>
        {{ if .Intro }}<p class="travel-intro">{{ .Intro }}</p>{{ end }}
        {{ if .Status }}<p class="travel-status">{{ .Status }}</p>{{ end }}
        {{ if .LastUpdated }}<p class="travel-last-updated">Last updated {{ .LastUpdated }}</p>{{ end }}
      </div>
      {{ if .ShowDownload }}<a class="travel-download" href="{{ .Path }}/download">Download all</a>{{ end }}
    </header>
    {{ if .SummaryChips }}<section class="travel-summary">{{ range .SummaryChips }}<div class="travel-chip"><span>{{ .Label }}</span><strong>{{ .Value }}</strong></div>{{ end }}</section>{{ end }}
    <nav class="travel-nav">
      <a href="#latest-update">Latest</a>
      {{ if .ShowMap }}<a href="#trip-map">Map</a>{{ end }}
      <a href="#all-photos">All photos</a>
    </nav>
    {{ if .ShowHighlights }}
    <section class="travel-section">
      <div class="travel-section-heading"><h2>Highlights</h2></div>
      <div class="travel-highlights">
        {{ range .Highlights }}
        <button class="travel-highlight-card" type="button" data-open-asset="{{ .AssetID }}">
          <img src="{{ .ThumbnailURL }}" alt="{{ .Caption }}"/>
          <span>{{ .Title }}</span>
        </button>
        {{ end }}
      </div>
    </section>
    {{ end }}
    {{ if .ShowMap }}
    <section class="travel-section" id="trip-map">
      <div class="travel-section-heading"><h2>Route</h2><p>Approximate locations only.</p></div>
      <div class="travel-map-card">
        <svg viewBox="0 0 100 100" class="travel-map" aria-hidden="true">
          {{ if .MapSVGPath }}<path d="{{ .MapSVGPath }}" />{{ end }}
          {{ range .MapStops }}<a href="#{{ .Anchor }}"><circle cx="{{ printf "%.2f" .Longitude }}" cy="{{ printf "%.2f" .Latitude }}" r="3.2"></circle><text x="{{ printf "%.2f" .Longitude }}" y="{{ printf "%.2f" .Latitude }}">{{ .Label }}</text></a>{{ end }}
        </svg>
        <ol class="travel-stop-list">
          {{ range .MapStops }}<li><a href="#{{ .Anchor }}">{{ .Label }}</a></li>{{ end }}
        </ol>
      </div>
    </section>
    {{ end }}
    <section class="travel-section" id="latest-update">
      <div class="travel-section-heading"><h2>Updates</h2></div>
      <div class="travel-timeline">
        {{ range .Timeline }}
        <article class="travel-day" id="{{ .Anchor }}">
          <div class="travel-day-header">
            <p>{{ .DateLabel }}</p>
            {{ if .Title }}<h3>{{ .Title }}</h3>{{ end }}
          </div>
          <div class="travel-day-grid">
            {{ range .Items }}
            <button class="travel-story-card{{ if .Video }} is-video{{ end }}" type="button" data-open-asset="{{ .AssetID }}">
              <img src="{{ .ThumbnailURL }}" alt="{{ .Caption }}"/>
              <div class="travel-story-copy">
                {{ if .PlaceLabel }}<span class="travel-place">{{ .PlaceLabel }}</span>{{ end }}
                {{ if .Caption }}<p>{{ .Caption }}</p>{{ end }}
              </div>
            </button>
            {{ end }}
          </div>
        </article>
        {{ end }}
      </div>
    </section>
    <section class="travel-section" id="all-photos">
      <div class="travel-section-heading"><h2>All photos</h2></div>
      <div id="gallery"></div>
    </section>
  </main>
  <script id="ipp-init" type="application/json">{{ .InitJSON }}</script>
  <script type="module" src="/assets/web.js"></script>
</body>
</html>`
	tmpl, err := template.New("travel").Parse(travelPageHTML)
	if err != nil {
		return err
	}
	initJSON, err := json.Marshal(data.InitData)
	if err != nil {
		return err
	}
	payload := struct {
		TravelPageData
		InitJSON template.JS
	}{
		TravelPageData: data,
		InitJSON:       template.JS(initJSON),
	}
	return tmpl.Execute(w, payload)
}

func mapSVGPath(stops []TravelMapStop) string {
	if len(stops) == 0 {
		return ""
	}
	minLat, maxLat := stops[0].Latitude, stops[0].Latitude
	minLng, maxLng := stops[0].Longitude, stops[0].Longitude
	for _, stop := range stops[1:] {
		if stop.Latitude < minLat {
			minLat = stop.Latitude
		}
		if stop.Latitude > maxLat {
			maxLat = stop.Latitude
		}
		if stop.Longitude < minLng {
			minLng = stop.Longitude
		}
		if stop.Longitude > maxLng {
			maxLng = stop.Longitude
		}
	}
	scale := func(value, min, max float64) float64 {
		if max == min {
			return 50
		}
		return 10 + ((value-min)/(max-min))*80
	}
	var b strings.Builder
	for i, stop := range stops {
		x := scale(stop.Longitude, minLng, maxLng)
		y := 100 - scale(stop.Latitude, minLat, maxLat)
		stops[i].Longitude = x
		stops[i].Latitude = y
		if len(stops) < 2 {
			continue
		}
		if i == 0 {
			fmt.Fprintf(&b, "M %.2f %.2f ", x, y)
			continue
		}
		fmt.Fprintf(&b, "L %.2f %.2f ", x, y)
	}
	return strings.TrimSpace(b.String())
}

func assetDimensions(asset immich.Asset) (int, int) {
	if asset.ExifInfo == nil {
		return 0, 0
	}
	width := asset.ExifInfo.ExifImageWidth
	height := asset.ExifInfo.ExifImageHeight
	if width == 0 || height == 0 {
		return width, height
	}
	switch asset.ExifInfo.Orientation {
	case "5", "6", "7", "8":
		return height, width
	default:
		return width, height
	}
}

func (r *Renderer) resolvePublicBaseURL(req *http.Request) string {
	if r.publicBaseURL != "" {
		return r.publicBaseURL
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + req.Host
}

func previewImageSize(asset immich.Asset) immich.ImageSize {
	if asset.OriginalMimeType == "image/gif" {
		return immich.ImageSizeOriginal
	}
	return immich.ImageSizePreview
}

func photoURL(key, id string, size immich.ImageSize) string {
	parts := []string{"", "share", "photo", key, id}
	if size != "" {
		parts = append(parts, string(size))
	}
	return strings.Join(parts, "/")
}

func videoURL(key, id string) string {
	return "/share/video/" + key + "/" + id
}

func sanitizeFilename(input string) string {
	output := illegalFilenameChars.ReplaceAllString(input, "")
	output = controlFilenameChars.ReplaceAllString(output, "")
	output = dotsOnlyFilename.ReplaceAllString(output, "")
	output = windowsReservedName.ReplaceAllString(output, "")
	output = windowsTrailingChars.ReplaceAllString(output, "")
	if len(output) > 254 {
		output = output[:254]
	}
	return strings.TrimSpace(output)
}
