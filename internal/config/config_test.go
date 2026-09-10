package config

import "testing"

func TestFromEnv(t *testing.T) {
	t.Setenv("GOMODEL_URL", "https://example.test/")
	t.Setenv("GOMODEL_TOKEN", " token ")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://example.test" || cfg.Token != "token" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestFromEnvRequiresToken(t *testing.T) {
	t.Setenv("GOMODEL_URL", "")
	t.Setenv("GOMODEL_TOKEN", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected missing token error")
	}
}
