package config

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	GoModel   GoModelConfig   `yaml:"gomodel"`
	Providers ProvidersConfig `yaml:"providers"`
}

type GoModelConfig struct {
	URL    string `yaml:"url"`
	APIKey string `yaml:"api_key"`
}

type ProvidersConfig struct {
	OpenCode    OpenCodeConfig    `yaml:"opencode"`
	CommandCode CommandCodeConfig `yaml:"commandcode"`
	Codex       CodexConfig       `yaml:"codex"`
}

type OpenCodeConfig struct {
	APIKey string `yaml:"api_key"`
}
type CommandCodeConfig struct {
	Cookie string `yaml:"cookie"`
}
type CodexConfig struct {
	AuthPath string `yaml:"auth_path"`
}

type option struct {
	key, env, legacy string
	value            *string
}

func Load(args []string, getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	path, explicit, err := configPath(args, getenv)
	if err != nil {
		return Config{}, err
	}
	cfg, err := readFile(path, explicit)
	if err != nil {
		return Config{}, err
	}
	options := optionsFor(&cfg)
	for _, option := range options {
		if value := strings.TrimSpace(getenv(option.env)); value != "" {
			*option.value = value
		} else if value := strings.TrimSpace(getenv(option.legacy)); option.legacy != "" && value != "" {
			*option.value = value
		}
	}
	if err := applyFlags(args, options); err != nil {
		return Config{}, err
	}
	return validate(cfg)
}

func DefaultPath(getenv func(string) string) (string, error) {
	if root := strings.TrimSpace(getenv("XDG_CONFIG_HOME")); root != "" {
		return filepath.Join(root, "gomodeltui", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".config", "gomodeltui", "config.yaml"), nil
}

func configPath(args []string, getenv func(string) string) (string, bool, error) {
	path, err := DefaultPath(getenv)
	if err != nil {
		return "", false, err
	}
	if fromEnv := strings.TrimSpace(getenv("GOMODELTUI_CONFIG")); fromEnv != "" {
		path = fromEnv
	}
	for index := 0; index < len(args); index++ {
		if args[index] == "--config" {
			if index+1 == len(args) {
				return "", false, fmt.Errorf("--config requires a path")
			}
			return args[index+1], true, nil
		}
		if strings.HasPrefix(args[index], "--config=") {
			return strings.TrimPrefix(args[index], "--config="), true, nil
		}
	}
	return path, strings.TrimSpace(getenv("GOMODELTUI_CONFIG")) != "", nil
}

func readFile(path string, required bool) (Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) && !required {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && err != io.EOF {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	return cfg, nil
}

func optionsFor(cfg *Config) []option {
	return []option{
		{"gomodel.url", "GOMODEL_URL", "", &cfg.GoModel.URL},
		{"gomodel.api_key", "GOMODEL_API_KEY", "GOMODEL_TOKEN", &cfg.GoModel.APIKey},
		{"providers.opencode.api_key", "PROVIDERS_OPENCODE_API_KEY", "OPENCODE_API_KEY", &cfg.Providers.OpenCode.APIKey},
		{"providers.commandcode.cookie", "PROVIDERS_COMMANDCODE_COOKIE", "COMMANDCODE_COOKIE", &cfg.Providers.CommandCode.Cookie},
		{"providers.codex.auth_path", "PROVIDERS_CODEX_AUTH_PATH", "", &cfg.Providers.Codex.AuthPath},
	}
}

func applyFlags(args []string, options []option) error {
	flags := flag.NewFlagSet("gomodeltui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := ""
	flags.StringVar(&configPath, "config", "", "configuration file path")
	for _, option := range options {
		flags.StringVar(option.value, option.key, *option.value, option.key)
	}
	return flags.Parse(args)
}

func validate(cfg Config) (Config, error) {
	cfg.GoModel.URL = strings.TrimRight(strings.TrimSpace(cfg.GoModel.URL), "/")
	if cfg.GoModel.URL == "" {
		cfg.GoModel.URL = "http://localhost:8080"
	}
	cfg.GoModel.APIKey = strings.TrimSpace(cfg.GoModel.APIKey)
	if cfg.GoModel.APIKey == "" {
		return Config{}, fmt.Errorf("gomodel.api_key is required")
	}
	cfg.Providers.OpenCode.APIKey = strings.TrimSpace(cfg.Providers.OpenCode.APIKey)
	cfg.Providers.CommandCode.Cookie = strings.TrimSpace(cfg.Providers.CommandCode.Cookie)
	cfg.Providers.Codex.AuthPath = strings.TrimSpace(cfg.Providers.Codex.AuthPath)
	if strings.HasPrefix(cfg.Providers.Codex.AuthPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("expand providers.codex.auth_path: %w", err)
		}
		cfg.Providers.Codex.AuthPath = filepath.Join(home, strings.TrimPrefix(cfg.Providers.Codex.AuthPath, "~/"))
	}
	return cfg, nil
}
