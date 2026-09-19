# gomodeltui

Read-only terminal UI for GoModel request activity and live audit logs.

## Development

```sh
nix develop
go test ./...
go run ./cmd/gomodeltui
```

Set `GOMODEL_URL` and `GOMODEL_TOKEN` before starting the application.

## Provider usage

Press `u` to view quotas for accounts available on this machine. The screen
refreshes every minute and `r` refreshes it immediately. Providers are shown
only when their credential is available:

- Codex: the local `~/.codex/auth.json` sign-in.
- OpenCode Go: `OPENCODE_API_KEY`.
- Command Code: `COMMANDCODE_COOKIE` containing the signed-in web-session
  `Cookie` header.

Credentials are read only and are never written to disk by gomodeltui.
