package types

import "net/http"

const (
	AssetTypeImage = "IMAGE"
	AssetTypeVideo = "VIDEO"

	KeyTypeKey  = "key"
	KeyTypeSlug = "slug"

	AlbumTypeAlbum      = "ALBUM"
	AlbumTypeIndividual = "INDIVIDUAL"

	ImageSizeThumbnail = "thumbnail"
	ImageSizePreview   = "preview"
	ImageSizeOriginal  = "original"

	DownloadAllDisabled  = 0
	DownloadAllPerImmich = 1
	DownloadAllAlways    = 2
)

type ExifInfo struct {
	Description string `json:"description,omitempty"`
}

type Asset struct {
	ID               string    `json:"id"`
	Key              string    `json:"key,omitempty"`
	KeyType          string    `json:"keyType,omitempty"`
	OriginalFileName string    `json:"originalFileName,omitempty"`
	OriginalMimeType string    `json:"originalMimeType,omitempty"`
	Password         string    `json:"password,omitempty"`
	FileCreatedAt    string    `json:"fileCreatedAt,omitempty"`
	Type             string    `json:"type"`
	IsTrashed        bool      `json:"isTrashed"`
	Visibility       string    `json:"visibility,omitempty"`
	ExifInfo         *ExifInfo `json:"exifInfo,omitempty"`
}

type Album struct {
	ID     string  `json:"id"`
	Assets []Asset `json:"assets"`
}

type SharedLinkAlbum struct {
	ID          string `json:"id"`
	AlbumName   string `json:"albumName,omitempty"`
	Order       string `json:"order,omitempty"`
	Description string `json:"description,omitempty"`
}

type SharedLink struct {
	Key           string           `json:"key"`
	KeyType       string           `json:"keyType,omitempty"`
	Type          string           `json:"type"`
	Description   string           `json:"description,omitempty"`
	Assets        []Asset          `json:"assets"`
	AllowDownload bool             `json:"allowDownload,omitempty"`
	Password      string           `json:"password,omitempty"`
	Album         *SharedLinkAlbum `json:"album,omitempty"`
	ExpiresAt     *string          `json:"expiresAt"`
}

type SharedLinkResult struct {
	Valid            bool
	Key              string
	PasswordRequired bool
	Link             *SharedLink
}

type IncomingShareRequest struct {
	Request  *http.Request
	Key      string
	KeyType  string
	Password string
	Mode     string
	Size     string
	Range    string
}
