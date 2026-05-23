package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsWithoutConfigFile(t *testing.T) {
	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IPP.DownloadOriginalPhoto {
		t.Fatal("expected default downloadOriginalPhoto=true")
	}
	if !cfg.IPP.AllowSlugLinks {
		t.Fatal("expected default allowSlugLinks=true")
	}
	if cfg.IPP.ResponseHeaders["Cache-Control"] == "" {
		t.Fatal("expected default response headers")
	}
}

func TestLoadInlineConfigMergesDefaults(t *testing.T) {
	cfg, err := Load(LoadOptions{
		InlineJSON: `{"ipp":{"showHomePage":false},"lightGallery":{"download":false}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IPP.ShowHomePage {
		t.Fatal("expected inline showHomePage=false")
	}
	if !cfg.IPP.DownloadOriginalPhoto {
		t.Fatal("expected missing bool default to remain true")
	}
	var lightGallery map[string]any
	if err := json.Unmarshal(cfg.LightGallery, &lightGallery); err != nil {
		t.Fatal(err)
	}
	if lightGallery["download"] != false {
		t.Fatal("expected inline lightGallery override")
	}
}

func TestLoadIPPConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"ipp":{"allowDownloadAll":2}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{FilePaths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IPP.AllowDownloadAll != DownloadAllAlways {
		t.Fatalf("expected allowDownloadAll=2, got %d", cfg.IPP.AllowDownloadAll)
	}
	if !cfg.IPP.AllowSlugLinks {
		t.Fatal("expected defaults merged for omitted fields")
	}
}

func TestLoadAllowsEmptyResponseHeaders(t *testing.T) {
	cfg, err := Load(LoadOptions{
		InlineJSON: `{"ipp":{"responseHeaders":{}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.IPP.ResponseHeaders) != 0 {
		t.Fatalf("expected empty response headers, got %#v", cfg.IPP.ResponseHeaders)
	}
}

func TestLoadLegacyInvalidResponseConfig(t *testing.T) {
	cfg, err := Load(LoadOptions{
		InlineJSON: `{"ipp":{"customInvalidResponse":"https://example.com"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IPP.CustomInvalidResponse.RedirectURL != "https://example.com" {
		t.Fatalf("unexpected redirect url: %#v", cfg.IPP.CustomInvalidResponse)
	}
}
