package immich

import "time"

type AssetType string

const (
	AssetTypeImage AssetType = "IMAGE"
	AssetTypeVideo AssetType = "VIDEO"
)

type KeyType string

const (
	KeyTypeKey  KeyType = "key"
	KeyTypeSlug KeyType = "slug"
)

type AlbumType string

const (
	AlbumTypeAlbum      AlbumType = "ALBUM"
	AlbumTypeIndividual AlbumType = "INDIVIDUAL"
)

type ImageSize string

const (
	ImageSizeThumbnail ImageSize = "thumbnail"
	ImageSizePreview   ImageSize = "preview"
	ImageSizeOriginal  ImageSize = "original"
)

type ShareAccess int

const (
	ShareAccessGranted ShareAccess = iota
	ShareAccessPasswordRequired
	ShareAccessInvalid
)

type ExifInfo struct {
	Description string `json:"description,omitempty"`
}

type Asset struct {
	ID               string    `json:"id"`
	Key              string    `json:"key,omitempty"`
	KeyType          KeyType   `json:"keyType,omitempty"`
	OriginalFileName string    `json:"originalFileName,omitempty"`
	OriginalMimeType string    `json:"originalMimeType,omitempty"`
	Password         string    `json:"password,omitempty"`
	FileCreatedAt    string    `json:"fileCreatedAt,omitempty"`
	Type             AssetType `json:"type"`
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
	KeyType       KeyType          `json:"keyType,omitempty"`
	Type          AlbumType        `json:"type"`
	Description   string           `json:"description,omitempty"`
	Assets        []Asset          `json:"assets"`
	AllowDownload bool             `json:"allowDownload,omitempty"`
	Password      string           `json:"password,omitempty"`
	Album         *SharedLinkAlbum `json:"album,omitempty"`
	ExpiresAt     *time.Time       `json:"expiresAt"`
}
