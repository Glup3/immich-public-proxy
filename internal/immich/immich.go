package immich

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/alangrainger/immich-public-proxy/internal/invalid"
	"github.com/alangrainger/immich-public-proxy/internal/types"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

var (
	idRe  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	keyRe = regexp.MustCompile(`^[\w-]+$`)
)

func New() *Client {
	return &Client{
		BaseURL: strings.TrimRight(os.Getenv("IMMICH_URL"), "/"),
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *Client) APIURL() string {
	return strings.TrimRight(c.BaseURL, "/") + "/api"
}

func (c *Client) Accessible() bool {
	resp, err := c.Request("/server/ping")
	if err != nil {
		return false
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	return resp.StatusCode == http.StatusOK
}

func (c *Client) Request(endpoint string) (*http.Response, error) {
	resp, err := c.HTTPClient.Get(c.APIURL() + endpoint)
	if err != nil {
		invalid.Log("Unable to reach Immich on " + c.BaseURL)
		invalid.Log("From the server IPP is running on, see if you can curl to " + c.APIURL() + "/server/ping and receive a JSON result.")
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	invalid.Log(fmt.Sprintf("Immich API status %d", resp.StatusCode))
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if len(body) > 0 {
		fmt.Println(string(body))
	}
	return resp, nil
}

func (c *Client) GetShareByKey(key, password, keyType string) types.SharedLinkResult {
	if keyType == "" {
		keyType = types.KeyTypeKey
	}
	requestURL := c.BuildURL(c.APIURL()+"/shared-links/me", map[string]string{
		keyType:    key,
		"password": password,
	})
	resp, err := c.HTTPClient.Get(requestURL)
	if err != nil {
		invalid.Log("Unable to reach Immich on " + c.BaseURL)
		return types.SharedLinkResult{Valid: false}
	}
	defer resp.Body.Close()

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "application/json") {
		invalid.Log(fmt.Sprintf("Immich response %d for key %s", resp.StatusCode, key))
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		fmt.Println(contentType)
		fmt.Println(string(body))
		invalid.Log("Unexpected response from Immich API at " + c.APIURL())
		invalid.Log("Please make sure the IPP container is able to reach this path.")
		return types.SharedLinkResult{Valid: false}
	}

	var raw map[string]any
	data, _ := io.ReadAll(resp.Body)
	if len(data) > 0 {
		_ = json.Unmarshal(data, &raw)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		message, _ := raw["message"].(string)
		if message == "Invalid share key" || message == "Invalid share slug" {
			invalid.Log("Invalid share key " + key)
			return types.SharedLinkResult{Valid: false}
		}
		return types.SharedLinkResult{Valid: true, PasswordRequired: true}
	}
	if resp.StatusCode != http.StatusOK {
		if len(data) > 0 {
			fmt.Println(string(data))
		}
		return types.SharedLinkResult{Valid: false}
	}

	var link types.SharedLink
	if err := json.Unmarshal(data, &link); err != nil {
		return types.SharedLinkResult{Valid: false}
	}
	link.KeyType = keyType

	if link.Type == types.AlbumTypeAlbum && link.Album != nil {
		albumURL := c.BuildURL(c.APIURL()+"/albums/"+url.PathEscape(link.Album.ID), map[string]string{
			keyType:    key,
			"password": password,
		})
		albumResp, err := c.HTTPClient.Get(albumURL)
		if err != nil {
			return types.SharedLinkResult{Valid: false}
		}
		defer albumResp.Body.Close()
		var album types.Album
		if err := json.NewDecoder(albumResp.Body).Decode(&album); err != nil || album.ID == "" {
			invalid.Log("Invalid album ID - " + link.Album.ID)
			return types.SharedLinkResult{Valid: false}
		}
		link.Assets = album.Assets
	}

	link.Password = password
	if link.ExpiresAt != nil && *link.ExpiresAt != "" {
		expires, err := time.Parse(time.RFC3339, *link.ExpiresAt)
		if err == nil && expires.Before(time.Now()) {
			invalid.Log("Expired link " + key)
			return types.SharedLinkResult{Valid: false}
		}
	}

	filtered := make([]types.Asset, 0, len(link.Assets))
	for _, asset := range link.Assets {
		if asset.IsTrashed {
			continue
		}
		asset.Key = key
		asset.KeyType = keyType
		asset.Password = password
		filtered = append(filtered, asset)
	}
	link.Assets = filtered

	if link.Album != nil {
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

	return types.SharedLinkResult{Valid: true, Link: &link}
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

func (c *Client) GetVideoContentType(asset types.Asset) string {
	resp, err := c.Request(c.BuildURL("/assets/"+url.PathEscape(asset.ID)+"/video/playback", map[string]string{
		asset.KeyType: asset.Key,
		"password":    asset.Password,
	}))
	if err != nil || resp == nil {
		return ""
	}
	defer resp.Body.Close()
	return resp.Header.Get("Content-Type")
}

func (c *Client) PhotoURL(key, id, size string) string {
	parts := []string{"", "share", "photo", key, id}
	if size != "" {
		parts = append(parts, size)
	}
	return strings.Join(parts, "/")
}

func (c *Client) VideoURL(key, id string) string {
	return "/share/video/" + key + "/" + id
}

func IsID(id string) bool {
	return idRe.MatchString(id)
}

func IsKey(key string) bool {
	return keyRe.MatchString(key)
}

func ValidateImageSize(size string) string {
	if size == "" || (size != types.ImageSizeThumbnail && size != types.ImageSizePreview && size != types.ImageSizeOriginal) {
		return types.ImageSizePreview
	}
	return size
}

func IsImageSize(size string) bool {
	return size == types.ImageSizeThumbnail || size == types.ImageSizePreview || size == types.ImageSizeOriginal
}

func PreviewImageSize(asset types.Asset) string {
	if asset.OriginalMimeType == "image/gif" {
		return types.ImageSizeOriginal
	}
	return types.ImageSizePreview
}

func KeyTypeFromShare(shareType string) string {
	if shareType == "s" {
		return types.KeyTypeSlug
	}
	return types.KeyTypeKey
}
