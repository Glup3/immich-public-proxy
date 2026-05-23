package render

import (
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
	templates     *template.Template
	publicBaseURL string
}

type PasswordPageData struct {
	Key                   string
	NotifyInvalidPassword bool
}

type GalleryItem struct {
	HTML         template.HTML `json:"html"`
	ThumbnailURL string        `json:"thumbnailUrl"`
	PreviewURL   string        `json:"previewUrl"`
}

type GalleryPageData struct {
	Items         []GalleryItem
	InitialItems  []GalleryItem
	ItemsJSON     template.JS
	OpenItem      int
	Title         string
	Description   string
	PublicBaseURL string
	Path          string
	ShowDownload  bool
	ShowTitle     bool
	HasMore       bool
}

func New(cfg config.Config, publicBaseURL string) (*Renderer, error) {
	tmpl, err := template.ParseGlob("templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Renderer{
		config:        cfg,
		templates:     tmpl,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}, nil
}

func (r *Renderer) Home(w http.ResponseWriter) error {
	r.config.AddResponseHeaders(w.Header())
	return r.templates.ExecuteTemplate(w, "home.html", nil)
}

func (r *Renderer) Password(w http.ResponseWriter, key string, notifyInvalidPassword bool) error {
	return r.templates.ExecuteTemplate(w, "password.html", PasswordPageData{
		Key:                   key,
		NotifyInvalidPassword: notifyInvalidPassword,
	})
}

func (r *Renderer) Gallery(w http.ResponseWriter, req *http.Request, share *immich.SharedLink, openItem int, showDownload bool) error {
	items := make([]GalleryItem, 0, len(share.Assets))
	for _, asset := range share.Assets {
		items = append(items, r.galleryItem(share, asset))
	}

	itemsJSON, err := json.Marshal(struct {
		LGConfig json.RawMessage `json:"lgConfig"`
		Items    []GalleryItem   `json:"items"`
		OpenItem *int            `json:"openItem"`
	}{
		LGConfig: r.config.LightGallery,
		Items:    items,
		OpenItem: optionalOpenItem(openItem),
	})
	if err != nil {
		return fmt.Errorf("marshal gallery payload: %w", err)
	}

	description := ""
	if r.config.IPP.ShowGalleryDescription {
		description = Description(share)
	}
	data := GalleryPageData{
		Items:         items,
		InitialItems:  firstN(items, 50),
		ItemsJSON:     template.JS(itemsJSON),
		OpenItem:      openItem,
		Title:         Title(share),
		Description:   description,
		PublicBaseURL: r.resolvePublicBaseURL(req),
		Path:          "/share/" + share.Key,
		ShowDownload:  showDownload,
		ShowTitle:     r.config.IPP.ShowGalleryTitle,
		HasMore:       len(items) > 50,
	}
	return r.templates.ExecuteTemplate(w, "gallery.html", data)
}

func (r *Renderer) galleryItem(share *immich.SharedLink, asset immich.Asset) GalleryItem {
	var videoJSON, downloadURL string
	if asset.Type == immich.AssetTypeVideo {
		mimeType := asset.OriginalMimeType
		if mimeType == "" {
			mimeType = "video/mp4"
		}
		video, _ := json.Marshal(struct {
			Source []map[string]string `json:"source"`
			Attrs  map[string]string   `json:"attributes"`
		}{
			Source: []map[string]string{{
				"src":  videoURL(share.Key, asset.ID),
				"type": mimeType,
			}},
			Attrs: map[string]string{
				"playsinline": "playsinline",
				"controls":    "controls",
			},
		})
		videoJSON = string(video)
		downloadURL = videoURL(share.Key, asset.ID)
	}
	if r.config.IPP.DownloadOriginalPhoto {
		downloadURL = photoURL(share.Key, asset.ID, immich.ImageSizeOriginal)
	}

	thumbnailURL := photoURL(share.Key, asset.ID, immich.ImageSizeThumbnail)
	previewURL := photoURL(share.Key, asset.ID, previewImageSize(asset))
	description := ""
	if r.config.IPP.ShowMetadata.Description && asset.ExifInfo != nil {
		description = asset.ExifInfo.Description
	}
	descriptionEsc := html.EscapeString(description)

	var b strings.Builder
	if videoJSON != "" {
		b.WriteString(`<a data-video='`)
		b.WriteString(html.EscapeString(videoJSON))
		b.WriteString(`'`)
	} else {
		b.WriteString(`<a href="`)
		b.WriteString(html.EscapeString(previewURL))
		b.WriteString(`"`)
	}
	if downloadURL != "" {
		b.WriteString(` data-download-url="`)
		b.WriteString(html.EscapeString(downloadURL))
		b.WriteString(`"`)
	}
	if description != "" {
		b.WriteString(` data-sub-html='<p>`)
		b.WriteString(descriptionEsc)
		b.WriteString(`</p>'`)
	}
	b.WriteString(` data-download="`)
	b.WriteString(html.EscapeString(Filename(r.config, asset)))
	b.WriteString(`" data-slide-name="`)
	b.WriteString(html.EscapeString(asset.ID))
	b.WriteString(`"><img alt="`)
	b.WriteString(descriptionEsc)
	b.WriteString(`" loading="lazy" src="`)
	b.WriteString(html.EscapeString(thumbnailURL))
	b.WriteString(`" onerror="this.closest('a').classList.add('thumb-error')"/>`)
	if videoJSON != "" {
		b.WriteString(`<div class="play-icon"></div>`)
	}
	b.WriteString(`</a>`)

	return GalleryItem{
		HTML:         template.HTML(b.String()),
		ThumbnailURL: thumbnailURL,
		PreviewURL:   previewURL,
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

func firstN(items []GalleryItem, n int) []GalleryItem {
	if len(items) <= n {
		return items
	}
	return items[:n]
}

func optionalOpenItem(openItem int) *int {
	if openItem > 0 {
		return &openItem
	}
	return nil
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
