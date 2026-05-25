package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
)

type DownloadedFilenameMode int

const (
	DownloadedFilenameOriginal DownloadedFilenameMode = iota
	DownloadedFilenameAssetID
	DownloadedFilenameShortID
)

type DownloadAllMode int

const (
	DownloadAllDisabled DownloadAllMode = iota
	DownloadAllPerImmich
	DownloadAllAlways
)

type InvalidResponseMode struct {
	Drop        bool
	StatusCode  int
	RedirectURL string
}

func (m *InvalidResponseMode) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	switch {
	case len(trimmed) == 0, bytes.Equal(trimmed, []byte("null")), bytes.Equal(trimmed, []byte("false")):
		*m = InvalidResponseMode{}
		return nil
	case bytes.Equal(trimmed, []byte("true")):
		*m = InvalidResponseMode{Drop: true}
		return nil
	}

	var status int
	if err := json.Unmarshal(trimmed, &status); err == nil {
		*m = InvalidResponseMode{StatusCode: status}
		return nil
	}

	var redirect string
	if err := json.Unmarshal(trimmed, &redirect); err == nil {
		*m = InvalidResponseMode{RedirectURL: redirect}
		return nil
	}

	return errors.New("invalid customInvalidResponse value")
}

func (m InvalidResponseMode) Enabled() bool {
	return m.Drop || m.StatusCode > 0 || m.RedirectURL != ""
}

type Config struct {
	IPP          IPPConfig       `json:"ipp"`
	LightGallery json.RawMessage `json:"lightGallery"`
}

type IPPConfig struct {
	ResponseHeaders        map[string]string      `json:"responseHeaders"`
	SingleImageGallery     bool                   `json:"singleImageGallery"`
	SingleItemAutoOpen     bool                   `json:"singleItemAutoOpen"`
	DownloadOriginalPhoto  bool                   `json:"downloadOriginalPhoto"`
	DownloadedFilename     DownloadedFilenameMode `json:"downloadedFilename"`
	AllowDownloadAll       DownloadAllMode        `json:"allowDownloadAll"`
	AllowSlugLinks         bool                   `json:"allowSlugLinks"`
	ShowHomePage           bool                   `json:"showHomePage"`
	ShowGalleryTitle       bool                   `json:"showGalleryTitle"`
	ShowGalleryDescription bool                   `json:"showGalleryDescription"`
	ShowMetadata           MetadataConfig         `json:"showMetadata"`
	CustomInvalidResponse  InvalidResponseMode    `json:"customInvalidResponse"`
	TravelMode             TravelModeConfig       `json:"travelMode"`
}

type MetadataConfig struct {
	Description bool `json:"description"`
}

type TravelModeConfig struct {
	Enabled               bool   `json:"enabled"`
	DefaultView           string `json:"defaultView"`
	GroupBy               string `json:"groupBy"`
	ShowMap               bool   `json:"showMap"`
	LocationPrecision     string `json:"locationPrecision"`
	ApproximateGridMeters int    `json:"approximateGridMeters"`
	ShowHighlights        bool   `json:"showHighlights"`
	HighlightTag          string `json:"highlightTag"`
	PlaceTagPrefix        string `json:"placeTagPrefix"`
	DayTagPrefix          string `json:"dayTagPrefix"`
	ShowLastUpdated       bool   `json:"showLastUpdated"`
}

type LoadOptions struct {
	InlineJSON string
	FilePaths  []string
}

func Default() Config {
	return Config{
		IPP: IPPConfig{
			ResponseHeaders: map[string]string{
				"Cache-Control":               "public, max-age=2592000",
				"Access-Control-Allow-Origin": "*",
			},
			SingleImageGallery:     false,
			SingleItemAutoOpen:     true,
			DownloadOriginalPhoto:  true,
			DownloadedFilename:     DownloadedFilenameOriginal,
			AllowDownloadAll:       DownloadAllDisabled,
			AllowSlugLinks:         true,
			ShowHomePage:           true,
			ShowGalleryTitle:       false,
			ShowGalleryDescription: false,
			ShowMetadata: MetadataConfig{
				Description: false,
			},
			TravelMode: TravelModeConfig{
				Enabled:               true,
				DefaultView:           "timeline",
				GroupBy:               "day",
				ShowMap:               true,
				LocationPrecision:     "approximate",
				ApproximateGridMeters: 5000,
				ShowHighlights:        true,
				HighlightTag:          "#highlight",
				PlaceTagPrefix:        "#place:",
				DayTagPrefix:          "#day:",
				ShowLastUpdated:       true,
			},
		},
		LightGallery: json.RawMessage(`{
			"controls": true,
			"download": true,
			"customSlideName": true,
			"mobileSettings": {
				"controls": false,
				"showCloseIcon": true,
				"download": true
			}
		}`),
	}
}

func Load(options LoadOptions) (Config, error) {
	cfg := Default()
	if options.InlineJSON != "" {
		if err := decodeInto(&cfg, []byte(options.InlineJSON)); err != nil {
			return cfg, fmt.Errorf("parse inline config: %w", err)
		}
		applyPostDecodeDefaults(&cfg)
		return cfg, cfg.Validate()
	}

	for _, path := range options.FilePaths {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := decodeInto(&cfg, data); err != nil {
				return cfg, fmt.Errorf("parse %s: %w", path, err)
			}
			applyPostDecodeDefaults(&cfg)
			return cfg, cfg.Validate()
		}
		if !errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("read %s: %w", path, err)
		}
	}

	return cfg, cfg.Validate()
}

func decodeInto(cfg *Config, data []byte) error {
	var probe struct {
		IPP *struct {
			ResponseHeaders json.RawMessage `json:"responseHeaders"`
		} `json:"ipp"`
		LightGallery *json.RawMessage `json:"lightGallery"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.IPP != nil && probe.IPP.ResponseHeaders != nil {
		cfg.IPP.ResponseHeaders = nil
	}
	if probe.LightGallery != nil {
		cfg.LightGallery = nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(cfg)
}

func (c Config) Validate() error {
	if c.IPP.DownloadedFilename < DownloadedFilenameOriginal || c.IPP.DownloadedFilename > DownloadedFilenameShortID {
		return fmt.Errorf("invalid downloadedFilename value %d", c.IPP.DownloadedFilename)
	}
	if c.IPP.AllowDownloadAll < DownloadAllDisabled || c.IPP.AllowDownloadAll > DownloadAllAlways {
		return fmt.Errorf("invalid allowDownloadAll value %d", c.IPP.AllowDownloadAll)
	}
	if c.IPP.CustomInvalidResponse.StatusCode < 0 {
		return fmt.Errorf("invalid customInvalidResponse status %d", c.IPP.CustomInvalidResponse.StatusCode)
	}
	if c.IPP.CustomInvalidResponse.RedirectURL != "" && c.IPP.CustomInvalidResponse.StatusCode != 0 {
		return errors.New("customInvalidResponse cannot use redirect and status code together")
	}
	switch c.IPP.TravelMode.DefaultView {
	case "timeline", "gallery":
	default:
		return fmt.Errorf("invalid travelMode.defaultView %q", c.IPP.TravelMode.DefaultView)
	}
	if c.IPP.TravelMode.GroupBy != "day" {
		return fmt.Errorf("invalid travelMode.groupBy %q", c.IPP.TravelMode.GroupBy)
	}
	switch c.IPP.TravelMode.LocationPrecision {
	case "none", "approximate", "exact":
	default:
		return fmt.Errorf("invalid travelMode.locationPrecision %q", c.IPP.TravelMode.LocationPrecision)
	}
	if c.IPP.TravelMode.ApproximateGridMeters <= 0 {
		return fmt.Errorf("invalid travelMode.approximateGridMeters %d", c.IPP.TravelMode.ApproximateGridMeters)
	}
	return nil
}

func (c Config) AddResponseHeaders(header http.Header) {
	for key, value := range c.IPP.ResponseHeaders {
		header.Set(key, value)
	}
}

func applyPostDecodeDefaults(cfg *Config) {
	if cfg.LightGallery == nil {
		cfg.LightGallery = Default().LightGallery
	}
	if cfg.IPP.ResponseHeaders == nil {
		cfg.IPP.ResponseHeaders = Default().IPP.ResponseHeaders
	}
}
