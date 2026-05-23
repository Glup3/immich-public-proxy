package render

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alangrainger/immich-public-proxy/internal/config"
	"github.com/alangrainger/immich-public-proxy/internal/immich"
	"github.com/alangrainger/immich-public-proxy/internal/invalid"
	"github.com/alangrainger/immich-public-proxy/internal/sanitize"
	"github.com/alangrainger/immich-public-proxy/internal/types"
)

type Renderer struct {
	Config    config.Config
	Immich    *immich.Client
	Invalid   invalid.Handler
	Templates *template.Template
}

type GalleryItem struct {
	HTML         template.HTML `json:"html"`
	ThumbnailURL string        `json:"thumbnailUrl"`
	PreviewURL   string        `json:"previewUrl"`
}

type GalleryData struct {
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

func New(cfg config.Config, immichClient *immich.Client, invalidHandler invalid.Handler) (*Renderer, error) {
	tmpl, err := template.ParseGlob("templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Renderer{
		Config:    cfg,
		Immich:    immichClient,
		Invalid:   invalidHandler,
		Templates: tmpl,
	}, nil
}

func (r *Renderer) Home(w http.ResponseWriter) {
	r.Config.AddResponseHeaders(w.Header())
	_ = r.Templates.ExecuteTemplate(w, "home.html", nil)
}

func (r *Renderer) Password(w http.ResponseWriter, key string, notifyInvalidPassword bool) {
	_ = r.Templates.ExecuteTemplate(w, "password.html", map[string]any{
		"Key":                   key,
		"NotifyInvalidPassword": notifyInvalidPassword,
	})
}

func (r *Renderer) Gallery(w http.ResponseWriter, req *http.Request, share *types.SharedLink, openItem int) {
	items := make([]GalleryItem, 0, len(share.Assets))
	for _, asset := range share.Assets {
		items = append(items, r.galleryItem(share, asset))
	}

	itemsJSON, _ := json.Marshal(map[string]any{
		"lgConfig": r.Config.LightGallery,
		"items":    items,
		"openItem": optionalOpenItem(openItem),
	})
	description := ""
	if r.Config.IPP.ShowGalleryDescription {
		description = r.Description(share)
	}
	publicBaseURL := strings.TrimRight(publicBaseURL(req), "/")
	title := r.Title(share)
	data := GalleryData{
		Items:         items,
		InitialItems:  firstN(items, 50),
		ItemsJSON:     template.JS(itemsJSON),
		OpenItem:      openItem,
		Title:         title,
		Description:   description,
		PublicBaseURL: publicBaseURL,
		Path:          "/share/" + share.Key,
		ShowDownload:  CanDownload(r.Config, share),
		ShowTitle:     r.Config.IPP.ShowGalleryTitle,
		HasMore:       len(items) > 50,
	}
	_ = r.Templates.ExecuteTemplate(w, "gallery.html", data)
}

func (r *Renderer) galleryItem(share *types.SharedLink, asset types.Asset) GalleryItem {
	var videoJSON, downloadURL string
	if asset.Type == types.AssetTypeVideo {
		video, _ := json.Marshal(map[string]any{
			"source": []map[string]string{{
				"src":  r.Immich.VideoURL(share.Key, asset.ID),
				"type": r.Immich.GetVideoContentType(asset),
			}},
			"attributes": map[string]string{
				"playsinline": "playsinline",
				"controls":    "controls",
			},
		})
		videoJSON = string(video)
		downloadURL = r.Immich.VideoURL(share.Key, asset.ID)
	}
	if r.Config.IPP.DownloadOriginalPhoto {
		downloadURL = r.Immich.PhotoURL(share.Key, asset.ID, types.ImageSizeOriginal)
	}

	thumbnailURL := r.Immich.PhotoURL(share.Key, asset.ID, types.ImageSizeThumbnail)
	previewURL := r.Immich.PhotoURL(share.Key, asset.ID, immich.PreviewImageSize(asset))
	description := ""
	if r.Config.IPP.ShowMetadata.Description && asset.ExifInfo != nil {
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
	b.WriteString(html.EscapeString(r.Filename(asset)))
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

func (r *Renderer) AssetBuffer(req types.IncomingShareRequest, w http.ResponseWriter, asset types.Asset, size string) {
	metaURL := r.Immich.BuildURL(r.Immich.APIURL()+"/assets/"+url.PathEscape(asset.ID), map[string]string{
		"key": asset.Key,
	})
	metaResp, err := r.Immich.HTTPClient.Get(metaURL)
	if err != nil {
		r.Invalid.Respond(w, http.StatusNotFound, "Failed response from Immich for asset "+asset.ID+" on this URL:\n"+metaURL)
		return
	}
	defer metaResp.Body.Close()

	var meta types.Asset
	_ = json.NewDecoder(metaResp.Body).Decode(&meta)
	if meta.IsTrashed || meta.Visibility == "locked" {
		r.Invalid.Respond(w, http.StatusNotFound, "Asset "+asset.ID+" is trashed or locked")
		return
	}

	headerList := []string{"content-type", "content-length", "last-modified", "etag"}
	size = immich.ValidateImageSize(size)
	subpath := ""
	sizeQueryParam := ""
	headers := http.Header{}
	if asset.Type == types.AssetTypeVideo {
		subpath = "/video/playback"
		start, end := parseRange(req.Range)
		headers.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		headerList = append(headerList, "cache-control", "content-range")
		w.Header().Set("Accept-Ranges", "bytes")
	} else if asset.Type == types.AssetTypeImage {
		if size == types.ImageSizeOriginal && r.Config.IPP.DownloadOriginalPhoto {
			subpath = "/original"
		} else if size == types.ImageSizePreview || size == types.ImageSizeOriginal {
			subpath = "/thumbnail"
			sizeQueryParam = "preview"
		} else {
			subpath = "/" + size
		}
	}

	keyType := asset.KeyType
	if keyType == "" {
		keyType = types.KeyTypeKey
	}
	assetURL := r.Immich.BuildURL(r.Immich.APIURL()+"/assets/"+url.PathEscape(asset.ID)+subpath, map[string]string{
		keyType:    asset.Key,
		"size":     sizeQueryParam,
		"password": asset.Password,
	})
	httpReq, _ := http.NewRequest(http.MethodGet, assetURL, nil)
	httpReq.Header = headers
	data, err := r.Immich.HTTPClient.Do(httpReq)
	if err != nil {
		r.Invalid.Respond(w, http.StatusNotFound, "Failed response from Immich for asset "+asset.ID+" on this URL:\n"+assetURL)
		return
	}
	defer data.Body.Close()

	if size == types.ImageSizeOriginal && asset.OriginalFileName != "" && r.Config.IPP.DownloadOriginalPhoto {
		w.Header().Set("Content-Disposition", `attachment; filename="`+r.Filename(asset)+`"`)
	}
	if data.StatusCode >= 200 && data.StatusCode < 300 {
		for _, header := range headerList {
			if value := data.Header.Get(header); value != "" {
				w.Header().Set(header, value)
			}
		}
		if asset.Type == types.AssetTypeVideo {
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = io.Copy(w, data.Body)
		return
	}

	immichMessage := ""
	var body map[string]any
	if err := json.NewDecoder(data.Body).Decode(&body); err == nil {
		if msg, ok := body["message"].(string); ok {
			immichMessage = "\nResponse from Immich: " + msg
		}
	}
	r.Invalid.Respond(w, http.StatusNotFound, "Failed response from Immich for asset "+asset.ID+" on this URL:\n"+assetURL+immichMessage)
}

func (r *Renderer) DownloadAll(w http.ResponseWriter, share *types.SharedLink) {
	downloadOriginalAsset := r.Config.IPP.DownloadOriginalPhoto
	w.Header().Set("Content-Type", "application/zip")
	filename := sanitize.Filename(r.Title(share))
	if filename == "" {
		filename = "photos"
	}
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename+".zip"))

	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()
	for _, asset := range share.Assets {
		endpoint := "thumbnail"
		size := "preview"
		if downloadOriginalAsset {
			endpoint = "original"
			size = ""
		}
		assetURL := r.Immich.BuildURL(r.Immich.APIURL()+"/assets/"+url.PathEscape(asset.ID)+"/"+endpoint, map[string]string{
			"key":      asset.Key,
			"password": asset.Password,
			"size":     size,
		})
		resp, err := r.Immich.HTTPClient.Get(assetURL)
		if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			invalid.Log("Failed to fetch asset: " + asset.ID)
			if resp != nil {
				_ = resp.Body.Close()
			}
			continue
		}
		writer, err := zipWriter.Create(r.Filename(asset))
		if err != nil {
			_ = resp.Body.Close()
			continue
		}
		_, _ = io.Copy(writer, resp.Body)
		_ = resp.Body.Close()
	}
}

func (r *Renderer) Title(share *types.SharedLink) string {
	if share.Description != "" {
		return share.Description
	}
	if share.Album != nil && share.Album.AlbumName != "" {
		return share.Album.AlbumName
	}
	return "Gallery"
}

func (r *Renderer) Description(share *types.SharedLink) string {
	if share.Album != nil {
		return share.Album.Description
	}
	return ""
}

func (r *Renderer) Filename(asset types.Asset) string {
	extension := filepath.Ext(asset.OriginalFileName)
	switch r.Config.IPP.DownloadedFilename {
	case 1:
		return asset.ID + extension
	case 2:
		prefix := asset.ID
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		return "img_" + prefix + extension
	default:
		if asset.OriginalFileName != "" {
			return asset.OriginalFileName
		}
		return asset.ID + extension
	}
}

func CanDownload(cfg config.Config, share *types.SharedLink) bool {
	switch cfg.IPP.AllowDownloadAll {
	case types.DownloadAllDisabled:
		return false
	case types.DownloadAllAlways:
		return true
	default:
		return share.AllowDownload
	}
}

func parseRange(raw string) (int64, int64) {
	trimmed := strings.TrimPrefix(raw, "bytes=")
	parts := strings.SplitN(trimmed, "-", 2)
	var start int64
	var end int64
	if len(parts) > 0 && parts[0] != "" {
		start, _ = strconv.ParseInt(parts[0], 10, 64)
	}
	if len(parts) > 1 && parts[1] != "" {
		end, _ = strconv.ParseInt(parts[1], 10, 64)
	} else {
		end = start + 2499999
	}
	return start, end
}

func firstN(items []GalleryItem, n int) []GalleryItem {
	if len(items) <= n {
		return items
	}
	return items[:n]
}

func optionalOpenItem(openItem int) any {
	if openItem > 0 {
		return openItem
	}
	return nil
}

func publicBaseURL(req *http.Request) string {
	if env := strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"); env != "" {
		return env
	}
	if value := strings.TrimRight(req.Header.Get("publicBaseUrl"), "/"); value != "" {
		return value
	}
	if value := strings.TrimRight(req.Header.Get("PublicBaseUrl"), "/"); value != "" {
		return value
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + req.Host
}
