package render

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
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
