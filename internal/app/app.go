package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alangrainger/immich-public-proxy/internal/config"
)

type Runtime struct {
	Port          string
	ImmichURL     string
	PublicBaseURL string
	SessionSecret string
	Config        config.Config
}

func LoadFromEnv() (Runtime, error) {
	inlineConfig := os.Getenv("CONFIG")
	configPath := os.Getenv("IPP_CONFIG")
	filePaths := []string{}
	if configPath != "" {
		filePaths = append(filePaths, configPath)
	} else {
		filePaths = append(filePaths, "/app/config.json", "config.json", filepath.Join("app", "config.json"))
	}

	cfg, err := config.Load(config.LoadOptions{
		InlineJSON: inlineConfig,
		FilePaths:  filePaths,
	})
	if err != nil {
		return Runtime{}, err
	}

	runtime := Runtime{
		Port:          envOrDefault("IPP_PORT", "3000"),
		ImmichURL:     os.Getenv("IMMICH_URL"),
		PublicBaseURL: os.Getenv("PUBLIC_BASE_URL"),
		SessionSecret: firstNonEmpty(os.Getenv("IPP_SESSION_SECRET"), os.Getenv("IPP_SECRET")),
		Config:        cfg,
	}

	if runtime.ImmichURL == "" {
		return Runtime{}, errors.New("IMMICH_URL is required")
	}
	if runtime.SessionSecret == "" {
		return Runtime{}, errors.New("IPP_SESSION_SECRET is required")
	}
	return runtime, nil
}

func envOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (r Runtime) Address() string {
	return fmt.Sprintf(":%s", r.Port)
}
