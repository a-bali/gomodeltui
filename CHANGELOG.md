# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Live activity dashboard backed by the GoModel SSE admin log stream, with
  automatic reconnect and `Last-Event-ID` resume.
- Audit-log backfill so recently completed requests appear at startup.
- btop-style Braille activity chart with seven time windows (5m through 24h)
  and success/error colouring.
- Request log with status, route, session, token, cache, and latency columns,
  plus search (`/`, `n`) and follow mode.
- Request detail view with structured and raw JSON, routing attempts, and
  expandable/collapsible conversation messages.
- Latency screen with per provider/model p50, p95, and max, and histogram views
  in linear, logarithmic, and p99 + overflow distributions.
- MCP method statistics screen that keeps MCP traffic out of model metrics.
- Provider quota screen for OpenCode Go, Command Code, and ChatGPT / Codex.
- YAML, environment-variable, and command-line configuration with a documented
  sample file (`config.example.yaml`).
- Release tooling: GitHub Actions CI and release workflows, GoReleaser
  configuration, and version metadata exposed via `--version`.
