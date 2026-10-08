# AGENTS.md

Guidance for AI agents (and new contributors) working in this repository.

## Project overview

`gomodeltui` is a **read-only** terminal UI (TUI) for GoModel request activity:
it streams the GoModel admin audit log over SSE, backfills recent history, and
renders live activity, a request log/inspector, latency statistics, MCP
statistics, and provider quotas.

Hard constraints:

- **Read-only by design.** Never add code that writes to the GoModel instance,
  mutates provider account state, or transmits credentials anywhere except the
  provider endpoint that issued them. Credentials are only read from local
  files or environment variables.
- Single binary, no database, no background daemon — state lives in memory.

## Tech stack

- Go 1.23+ (see `go.mod`), standard layout with `cmd/` + `internal/`.
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) (Elm-style
  `Update`/`Msg` loop) for the TUI, Lip Gloss for styling, `charmbracelet/x/ansi`
  for ANSI handling.
- YAML config via `gopkg.in/yaml.v3`.
- Releases via GoReleaser (`.goreleaser.yaml`), Nix flake dev shell (`flake.nix`).

## Repository layout

```
cmd/gomodeltui/     main entrypoint (version/commit/date via ldflags)
internal/config/    YAML + env + flag configuration loading
internal/gomodel/   GoModel API client: SSE stream, audit backfill, reducers
internal/chart/     Braille activity chart rendering
internal/latency/   latency histogram/statistics store
internal/usage/     provider quota fetching and snapshots
internal/ui/        Bubble Tea model, views, popups (largest package)
docs/               screenshots only (referenced from README.md)
```

Key files:

- `internal/ui/model.go` — the root Bubble Tea `Model`: message loop, SSE
  connection lifecycle, reconnect/backfill, keybindings.
- `internal/ui/*_view.go` — one file per screen (chart, latency, MCP, usage).
- `internal/gomodel/reducer.go` — folds raw audit events into request records.
- `config.example.yaml` — documented sample config; keep it in sync with
  `internal/config/config.go`.

## Build, test, lint

```sh
make build        # bin/gomodeltui (with version ldflags)
make test         # go test ./...
make test-race    # go test -race ./...
make vet          # go vet ./...
make fmt          # gofmt
make fmt-check    # fails if cmd/ or internal/ are unformatted
make run          # go run ./cmd/gomodeltui
make snapshot     # goreleaser release --snapshot --clean
```

CI (`.github/workflows/ci.yml`) runs exactly this gate on every PR — all four
must pass before a change is done:

```sh
test -z "$(gofmt -l cmd internal)"
go vet ./...
go test -race ./...
go build ./...
```

Integration tests are behind the `integration` build tag and require a live
GoModel instance (`GOMODEL_URL`, `GOMODEL_API_KEY`):

```sh
go test -tags=integration ./internal/ui/ -run Smoke
```

## Conventions

- **Formatting**: gofmt only; no formatter config, no third-party linter in CI
  (golangci-lint is available in the Nix shell but not enforced).
- **Commit messages**: Conventional Commits with a scope, e.g.
  `feat(latency): cycle histogram distribution modes`,
  `fix(ui): quit on Ctrl-C from every screen`, `chore(config): ...`.
- **Changelog**: update `CHANGELOG.md` ([Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
  SemVer) under `[Unreleased]` for user-visible changes; GoReleaser consumes it
  for releases.
- **Screens/UI**: add keybindings to the header help text in
  `internal/ui/model.go` and document them in `README.md` alongside the
  feature description. Keep MCP traffic separate from model traffic in
  statistics.
- **Tests**: colocate `<file>_test.go` next to the source; table-driven where
  practical. Pure logic (reducers, stats, chart math) should stay testable
  without a terminal or network.
- **Dependencies**: keep the tree small — Bubble Tea/Lip Gloss/ansi/yaml only
  for now; run `make tidy` after changes to `go.mod`.
- **Errors**: the TUI must never crash on malformed stream/API data — surface
  errors in the status line instead of exiting.

## Working notes

- The working tree may contain unrelated in-progress changes; stage only the
  files relevant to your task.
- `docs/*.png` screenshots illustrate README features — if you change a
  screen's layout meaningfully, note it in the PR rather than silently
  letting the docs drift.
