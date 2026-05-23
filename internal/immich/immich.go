package immich

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alangrainger/immich-public-proxy/internal/types"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	now        func() time.Time
	logger     *slog.Logger
}

var (
	idRe  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	keyRe = regexp.MustCompile(`^[\w-]+$`)
)

func NewClient(baseURL string, httpClient *http.Client, now func() time.Time, logger *slog.Logger) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		now:        now,
		logger:     logger,
	}
}

func (c *Client) APIURL() string {
	return c.baseURL + "/api"
}

func (c *Client) BuildURL(baseURL string, params map[string]string) string {
	values := url.Values{}
	for key, value := range params {
		if value != "" {
			values.Set(key, value)
		}
	}
	if len(values) == 0 {
		return baseURL
	}
	return baseURL + "?" + values.Encode()
}

func (c *Client) Accessible(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIURL()+"/server/ping", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Debug("immich ping failed", "error", err)
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *Client) FetchSharedLink(ctx context.Context, key, password string, keyType types.KeyType) (types.SharedLink, types.ShareAccess, error) {
	if keyType == "" {
		keyType = types.KeyTypeKey
	}
	requestURL := c.BuildURL(c.APIURL()+"/shared-links/me", map[string]string{
		string(keyType): key,
		"password":      password,
	})
	resp, err := c.get(ctx, requestURL)
	if err != nil {
		return types.SharedLink{}, types.ShareAccessInvalid, err
	}
	defer resp.Body.Close()

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "application/json") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return types.SharedLink{}, types.ShareAccessInvalid, fmt.Errorf("unexpected immich content type %q for key %s: %s", contentType, key, string(body))
	}

	var message struct {
		Message string `json:"message"`
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return types.SharedLink{}, types.ShareAccessInvalid, fmt.Errorf("decode shared link response: %w", err)
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &message)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		if message.Message == "Invalid share key" || message.Message == "Invalid share slug" {
			return types.SharedLink{}, types.ShareAccessInvalid, nil
		}
		return types.SharedLink{}, types.ShareAccessPasswordRequired, nil
	}
	if resp.StatusCode != http.StatusOK {
		return types.SharedLink{}, types.ShareAccessInvalid, fmt.Errorf("unexpected immich shared link status %d", resp.StatusCode)
	}

	var link types.SharedLink
	if err := json.Unmarshal(raw, &link); err != nil {
		return types.SharedLink{}, types.ShareAccessInvalid, fmt.Errorf("decode shared link: %w", err)
	}
	link.KeyType = keyType
	link.Password = password

	if link.Type == types.AlbumTypeAlbum && link.Album != nil {
		album, err := c.fetchAlbum(ctx, key, password, keyType, link.Album.ID)
		if err != nil {
			return types.SharedLink{}, types.ShareAccessInvalid, err
		}
		link.Assets = album.Assets
	}

	if link.ExpiresAt != nil && link.ExpiresAt.Before(c.now()) {
		return types.SharedLink{}, types.ShareAccessInvalid, nil
	}

	link.Assets = normalizeAssets(link.Assets, key, keyType, password)
	sortAssets(link)
	return link, types.ShareAccessGranted, nil
}

func (c *Client) StreamAsset(ctx context.Context, asset types.Asset, size types.ImageSize, rangeHeader string, downloadOriginal bool) (*http.Response, error) {
	path, params, headers, err := c.assetRequest(asset, size, rangeHeader, downloadOriginal)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BuildURL(c.APIURL()+path, params), nil)
	if err != nil {
		return nil, fmt.Errorf("build asset request: %w", err)
	}
	req.Header = headers
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch asset %s: %w", asset.ID, err)
	}
	return resp, nil
}

func (c *Client) DownloadAsset(ctx context.Context, asset types.Asset, original bool) (*http.Response, error) {
	size := types.ImageSizePreview
	if original {
		size = types.ImageSizeOriginal
	}
	return c.StreamAsset(ctx, asset, size, "", original)
}

func (c *Client) GetVideoContentType(ctx context.Context, asset types.Asset) (string, error) {
	resp, err := c.StreamAsset(ctx, asset, types.ImageSizePreview, "", false)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return resp.Header.Get("Content-Type"), nil
}

func (c *Client) get(ctx context.Context, requestURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) fetchAlbum(ctx context.Context, key, password string, keyType types.KeyType, albumID string) (types.Album, error) {
	requestURL := c.BuildURL(c.APIURL()+"/albums/"+url.PathEscape(albumID), map[string]string{
		string(keyType): key,
		"password":      password,
	})
	resp, err := c.get(ctx, requestURL)
	if err != nil {
		return types.Album{}, fmt.Errorf("fetch album %s: %w", albumID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return types.Album{}, fmt.Errorf("unexpected album status %d", resp.StatusCode)
	}
	var album types.Album
	if err := json.NewDecoder(resp.Body).Decode(&album); err != nil {
		return types.Album{}, fmt.Errorf("decode album %s: %w", albumID, err)
	}
	if album.ID == "" {
		return types.Album{}, errors.New("missing album id")
	}
	return album, nil
}

func (c *Client) assetRequest(asset types.Asset, size types.ImageSize, rangeHeader string, downloadOriginal bool) (string, map[string]string, http.Header, error) {
	keyType := asset.KeyType
	if keyType == "" {
		keyType = types.KeyTypeKey
	}
	params := map[string]string{
		string(keyType): asset.Key,
		"password":      asset.Password,
	}
	headers := http.Header{}

	switch asset.Type {
	case types.AssetTypeVideo:
		path := "/assets/" + url.PathEscape(asset.ID) + "/video/playback"
		if normalizedRange, ok := normalizeRange(rangeHeader); ok {
			headers.Set("Range", normalizedRange)
		}
		return path, params, headers, nil
	case types.AssetTypeImage:
		switch ValidateImageSize(size) {
		case types.ImageSizeOriginal:
			if downloadOriginal {
				return "/assets/" + url.PathEscape(asset.ID) + "/original", params, headers, nil
			}
			params["size"] = "preview"
			return "/assets/" + url.PathEscape(asset.ID) + "/thumbnail", params, headers, nil
		case types.ImageSizePreview:
			params["size"] = "preview"
			return "/assets/" + url.PathEscape(asset.ID) + "/thumbnail", params, headers, nil
		case types.ImageSizeThumbnail:
			return "/assets/" + url.PathEscape(asset.ID) + "/thumbnail", params, headers, nil
		default:
			return "", nil, nil, fmt.Errorf("unsupported image size %q", size)
		}
	default:
		return "", nil, nil, fmt.Errorf("unsupported asset type %q", asset.Type)
	}
}

func normalizeAssets(assets []types.Asset, key string, keyType types.KeyType, password string) []types.Asset {
	filtered := make([]types.Asset, 0, len(assets))
	for _, asset := range assets {
		if asset.IsTrashed {
			continue
		}
		asset.Key = key
		asset.KeyType = keyType
		asset.Password = password
		filtered = append(filtered, asset)
	}
	return filtered
}

func sortAssets(link types.SharedLink) {
	if link.Album == nil {
		return
	}
	switch link.Album.Order {
	case "asc":
		sort.SliceStable(link.Assets, func(i, j int) bool {
			return link.Assets[i].FileCreatedAt < link.Assets[j].FileCreatedAt
		})
	case "desc":
		sort.SliceStable(link.Assets, func(i, j int) bool {
			return link.Assets[i].FileCreatedAt > link.Assets[j].FileCreatedAt
		})
	}
}

func IsID(id string) bool {
	return idRe.MatchString(id)
}

func IsKey(key string) bool {
	return keyRe.MatchString(key)
}

func ValidateImageSize(size types.ImageSize) types.ImageSize {
	switch size {
	case types.ImageSizeThumbnail, types.ImageSizePreview, types.ImageSizeOriginal:
		return size
	default:
		return types.ImageSizePreview
	}
}

func IsImageSize(size string) bool {
	switch types.ImageSize(size) {
	case types.ImageSizeThumbnail, types.ImageSizePreview, types.ImageSizeOriginal:
		return true
	default:
		return false
	}
}

func PreviewImageSize(asset types.Asset) types.ImageSize {
	if asset.OriginalMimeType == "image/gif" {
		return types.ImageSizeOriginal
	}
	return types.ImageSizePreview
}

func KeyTypeFromShare(shareType string) types.KeyType {
	if shareType == "s" {
		return types.KeyTypeSlug
	}
	return types.KeyTypeKey
}

func normalizeRange(raw string) (string, bool) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "bytes="))
	if trimmed == "" {
		return "", false
	}
	parts := strings.SplitN(trimmed, "-", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", false
	}
	start := parts[0]
	end := parts[1]
	if end == "" {
		end = incrementRangeEnd(start)
		if end == "" {
			return "", false
		}
	}
	return "bytes=" + start + "-" + end, true
}

func incrementRangeEnd(start string) string {
	value, err := strconv.ParseInt(start, 10, 64)
	if err != nil {
		return ""
	}
	return strconv.FormatInt(value+2499999, 10)
}
