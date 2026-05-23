package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	IPP          IPPConfig      `json:"ipp"`
	LightGallery map[string]any `json:"lightGallery"`
}

type IPPConfig struct {
	ResponseHeaders        map[string]string `json:"responseHeaders"`
	SingleImageGallery     bool              `json:"singleImageGallery"`
	SingleItemAutoOpen     bool              `json:"singleItemAutoOpen"`
	DownloadOriginalPhoto  bool              `json:"downloadOriginalPhoto"`
	DownloadedFilename     int               `json:"downloadedFilename"`
	AllowDownloadAll       int               `json:"allowDownloadAll"`
	AllowSlugLinks         bool              `json:"allowSlugLinks"`
	ShowHomePage           bool              `json:"showHomePage"`
	ShowGalleryTitle       bool              `json:"showGalleryTitle"`
	ShowGalleryDescription bool              `json:"showGalleryDescription"`
	ShowMetadata           MetadataConfig    `json:"showMetadata"`
	CustomInvalidResponse  any               `json:"customInvalidResponse"`
}

type MetadataConfig struct {
	Description bool `json:"description"`
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
			DownloadedFilename:     0,
			AllowDownloadAll:       0,
			AllowSlugLinks:         true,
			ShowHomePage:           true,
			ShowGalleryTitle:       false,
			ShowGalleryDescription: false,
			ShowMetadata: MetadataConfig{
				Description: false,
			},
			CustomInvalidResponse: false,
		},
		LightGallery: map[string]any{
			"controls":        true,
			"download":        true,
			"customSlideName": true,
			"mobileSettings": map[string]any{
				"controls":      false,
				"showCloseIcon": true,
				"download":      true,
			},
		},
	}
}

func Load() (Config, error) {
	cfg := Default()
	if inline := os.Getenv("CONFIG"); inline != "" {
		if err := mergeJSON([]byte(inline), &cfg); err != nil {
			return cfg, fmt.Errorf("parse CONFIG: %w", err)
		}
		return cfg, nil
	}

	candidates := []string{}
	if path := os.Getenv("IPP_CONFIG"); path != "" {
		candidates = append(candidates, path)
	} else {
		candidates = append(candidates, "/app/config.json", "config.json", filepath.Join("app", "config.json"))
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := mergeJSON(data, &cfg); err != nil {
				return cfg, fmt.Errorf("parse %s: %w", path, err)
			}
			return cfg, nil
		}
		if !errors.Is(err, os.ErrNotExist) && os.Getenv("IPP_CONFIG") != "" {
			return cfg, err
		}
	}

	return cfg, nil
}

func mergeJSON(data []byte, cfg *Config) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return err
	}
	mergeDefaults(raw, cfg)
	return nil
}

func mergeDefaults(raw map[string]any, cfg *Config) {
	defaults := Default()
	if _, ok := raw["ipp"]; !ok {
		cfg.IPP = defaults.IPP
	}
	ippRaw, _ := raw["ipp"].(map[string]any)
	if _, ok := ippRaw["responseHeaders"]; !ok && cfg.IPP.ResponseHeaders == nil {
		cfg.IPP.ResponseHeaders = defaults.IPP.ResponseHeaders
	}
	if headersRaw, ok := ippRaw["responseHeaders"]; ok {
		cfg.IPP.ResponseHeaders = map[string]string{}
		if headers, ok := headersRaw.(map[string]any); ok {
			for key, value := range headers {
				if str, ok := value.(string); ok {
					cfg.IPP.ResponseHeaders[key] = str
				}
			}
		}
	}
	if _, ok := ippRaw["singleItemAutoOpen"]; !ok {
		cfg.IPP.SingleItemAutoOpen = defaults.IPP.SingleItemAutoOpen
	}
	if _, ok := ippRaw["downloadOriginalPhoto"]; !ok {
		cfg.IPP.DownloadOriginalPhoto = defaults.IPP.DownloadOriginalPhoto
	}
	if _, ok := ippRaw["allowSlugLinks"]; !ok {
		cfg.IPP.AllowSlugLinks = defaults.IPP.AllowSlugLinks
	}
	if _, ok := ippRaw["showHomePage"]; !ok {
		cfg.IPP.ShowHomePage = defaults.IPP.ShowHomePage
	}
	if _, ok := ippRaw["customInvalidResponse"]; !ok {
		cfg.IPP.CustomInvalidResponse = defaults.IPP.CustomInvalidResponse
	}
	if _, ok := raw["lightGallery"]; !ok || cfg.LightGallery == nil {
		cfg.LightGallery = defaults.LightGallery
	}
}

func (c Config) AddResponseHeaders(header interface{ Set(string, string) }) {
	for key, value := range c.IPP.ResponseHeaders {
		header.Set(key, value)
	}
}
