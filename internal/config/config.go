package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	URL   string
	Token string
}

func FromEnv() (Config, error) {
	cfg := Config{
		URL:   strings.TrimRight(strings.TrimSpace(os.Getenv("GOMODEL_URL")), "/"),
		Token: strings.TrimSpace(os.Getenv("GOMODEL_TOKEN")),
	}
	if cfg.URL == "" {
		cfg.URL = "http://localhost:8080"
	}
	if cfg.Token == "" {
		return Config{}, fmt.Errorf("GOMODEL_TOKEN is required")
	}
	return cfg, nil
}
