package config

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUsesYAMLThenEnvironmentThenFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("gomodel:\n  url: https://yaml.example/\n  api_key: yaml-key\nproviders:\n  opencode:\n    api_key: yaml-open\n  commandcode:\n    cookie: yaml-cookie\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"GOMODEL_API_KEY": "env-key", "PROVIDERS_OPENCODE_API_KEY": "env-open"}
	cfg, err := Load([]string{"--config", path, "--gomodel.url=https://flag.example/", "--providers.commandcode.cookie=flag-cookie"}, func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GoModel.URL != "https://flag.example" || cfg.GoModel.APIKey != "env-key" || cfg.Providers.OpenCode.APIKey != "env-open" || cfg.Providers.CommandCode.Cookie != "flag-cookie" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadSupportsLegacyEnvironmentNames(t *testing.T) {
	env := map[string]string{"GOMODEL_TOKEN": "legacy-key", "OPENCODE_API_KEY": "legacy-open", "COMMANDCODE_COOKIE": "legacy-cookie"}
	cfg, err := Load([]string{}, func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GoModel.APIKey != "legacy-key" || cfg.Providers.OpenCode.APIKey != "legacy-open" || cfg.Providers.CommandCode.Cookie != "legacy-cookie" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadRequiresGoModelAPIKey(t *testing.T) {
	if _, err := validate(Config{}); err == nil {
		t.Fatal("expected missing API key error")
	}
}

func TestLoadRejectsUnknownYAMLKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("gomodel:\n  api_key: key\n  typo: value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load([]string{"--config=" + path}, func(string) string { return "" }); err == nil {
		t.Fatal("expected unknown YAML field error")
	}
}

func TestLoadHelpDoesNotRequireConfiguration(t *testing.T) {
	if _, err := Load([]string{"--help"}, func(string) string { return "" }); err != flag.ErrHelp {
		t.Fatalf("err=%v, want ErrHelp", err)
	}
	if help := Help(); !strings.Contains(help, "--gomodel.url") || !strings.Contains(help, "PROVIDERS_COMMANDCODE_COOKIE") {
		t.Fatalf("help is missing configuration details:\n%s", help)
	}
}
