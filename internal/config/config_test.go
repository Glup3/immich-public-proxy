package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsWithoutConfigFile(t *testing.T) {
	t.Setenv("CONFIG", "")
	t.Setenv("IPP_CONFIG", "")
	t.Chdir(t.TempDir())

	cfg, err := Load()
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
	t.Setenv("CONFIG", `{"ipp":{"showHomePage":false},"lightGallery":{"download":false}}`)
	t.Setenv("IPP_CONFIG", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IPP.ShowHomePage {
		t.Fatal("expected inline showHomePage=false")
	}
	if !cfg.IPP.DownloadOriginalPhoto {
		t.Fatal("expected missing bool default to remain true")
	}
	if cfg.LightGallery["download"] != false {
		t.Fatal("expected inline lightGallery override")
	}
}

func TestLoadIPPConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"ipp":{"allowDownloadAll":2}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG", "")
	t.Setenv("IPP_CONFIG", path)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IPP.AllowDownloadAll != 2 {
		t.Fatalf("expected allowDownloadAll=2, got %d", cfg.IPP.AllowDownloadAll)
	}
	if !cfg.IPP.AllowSlugLinks {
		t.Fatal("expected defaults merged for omitted fields")
	}
}

func TestLoadAllowsEmptyResponseHeaders(t *testing.T) {
	t.Setenv("CONFIG", `{"ipp":{"responseHeaders":{}}}`)
	t.Setenv("IPP_CONFIG", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.IPP.ResponseHeaders) != 0 {
		t.Fatalf("expected empty response headers, got %#v", cfg.IPP.ResponseHeaders)
	}
}
