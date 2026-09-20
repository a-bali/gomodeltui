# gomodeltui

Read-only terminal UI for GoModel request activity and live audit logs.

## Development

```sh
nix develop
go test ./...
go run ./cmd/gomodeltui
```

## Configuration

Configuration is read from YAML, then environment variables, then command-line
arguments. Later sources override earlier ones. The default file is
`~/.config/gomodeltui/config.yaml` (or `$XDG_CONFIG_HOME/gomodeltui/config.yaml`);
use `--config /path/to/config.yaml` or `GOMODELTUI_CONFIG` to select another file.

```yaml
gomodel:
  url: http://localhost:8080
  api_key: your-gomodel-api-key

providers:
  opencode:
    api_key: your-opencode-api-key
  commandcode:
    cookie: "__Secure-commandcode_prod_.session_token=…"
  # codex:
  #   auth_path: ~/.codex/auth.json
```

The same dotted keys work in every source:

| Key | Environment | Command line |
| --- | --- | --- |
| `gomodel.url` | `GOMODEL_URL` | `--gomodel.url` |
| `gomodel.api_key` | `GOMODEL_API_KEY` | `--gomodel.api_key` |
| `providers.opencode.api_key` | `PROVIDERS_OPENCODE_API_KEY` | `--providers.opencode.api_key` |
| `providers.commandcode.cookie` | `PROVIDERS_COMMANDCODE_COOKIE` | `--providers.commandcode.cookie` |
| `providers.codex.auth_path` | `PROVIDERS_CODEX_AUTH_PATH` | `--providers.codex.auth_path` |

Restrict the YAML file to its owner because it can contain credentials
(`chmod 600 ~/.config/gomodeltui/config.yaml`).

## Provider usage

Press `u` to view quotas for accounts available on this machine. The screen
refreshes every minute and `r` refreshes it immediately. Providers are shown
only when their credential is available:

- Codex: the local `~/.codex/auth.json` sign-in, or `providers.codex.auth_path`.
- OpenCode Go: `providers.opencode.api_key`.
- Command Code: `providers.commandcode.cookie`.

Credentials are read only and are never written to disk by gomodeltui.
