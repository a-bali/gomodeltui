# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.2.0] - 2026-10-21

### Added

- Xiaomi MiMo quota fetching can refresh its short-lived console session
  automatically from an optional `providers.mimo.account_cookie` containing the
  long-lived Xiaomi Account `userId`/`passToken` cookies, and reports an
  actionable message when the session expires instead of a bare HTTP error.
  The same automatic refresh is available in the macOS menu bar app.
- The macOS menu bar icons show a custom hover tooltip with request activity,
  connection status, and per-provider details: every quota window's exact
  available percentage with a reset ETA, and balance amounts or fetch errors for
  balance-based providers, along with how long ago the data was refreshed.
  Right-clicking (or Control-clicking) any provider icon opens the same context
  menu as the main menu bar icon.
- The macOS right-click menu has an **About GoModelBar** item showing the app
  version, build date, and a link to the project's GitHub homepage.

### Changed

- The macOS app refreshes provider quotas and balances every five minutes in the
  background (and after system wake) in addition to manual refresh, keeping the
  menu bar indicators and tooltips current.
- Require Go 1.26 or newer and update the TUI rendering dependencies, including
  Bubble Tea and the compatible ANSI/cell buffer packages.

### Fixed

- The macOS dropdown now dismisses when clicking outside it, including in other
  apps and on the desktop. The request JSON inspector opened from a log row is
  now an independent regular window (not a panel) that stays open when the app
  is deactivated or the user clicks outside it, and the dropdown closes when
  the inspector opens so the two do not overlap. Clicking a menu bar icon while
  the dropdown is open now closes it instead of immediately reopening it.

## [1.1.0] - 2026-10-20

### Added

- TUI quota support for OpenRouter key limits and spend, optional credit
  balances, DeepSeek balances, and Xiaomi MiMo balance and token-plan usage.
- Native provider quota and balance display above the chart, including OpenCode
  Go, Command Code, ChatGPT / Codex, OpenRouter, DeepSeek, and Xiaomi MiMo, with
  remaining-quota bars, reset countdown tooltips, provider links, manual refresh,
  and visible errors.
- Optional native menu bar usage indicators for selected providers, with fixed
  width status items, colored quota bars or balance amounts, and selections
  stored in macOS preferences separately from the YAML configuration.
- Native usage icons label each provider, order quota bars from shortest to
  longest window, and show two-decimal balances in compact color-coded pills.
- Native request rows show color-coded three-character session IDs, app-path
  color pills, compact input/output/cache values, and ticking latency for pending
  requests that finalizes when the response arrives.
- Native request logs expand completed failovers into separate failed-attempt
  and failover rows, matching the TUI.
- TUI pending request latency refreshes every 0.1 seconds and freezes at the
  measured elapsed time when a live response arrives without a duration field.
- Removed the configurable `log_retention` setting; both apps use a fixed
  24-hour in-memory history limit, which covers every available chart window.
- TUI backfill now follows the selected chart window; widening the window loads
  older audit history, and the request log displays that same time range.
- TUI visible-row preview hydration and complete Enter inspection with audit-ID
  lookup, bounded concurrent requests, cancellation, and `R` content retry.
- TUI `M` toggle includes/excludes MCP in the main log, chart, and totals while
  retaining separate MCP and model statistics; MCP is excluded by default.
- Draggable native chart/log divider with a saved split position, minimum section
  heights, keyboard adjustment, and double-click reset.
- Resizable native dropdown with bottom-corner grips, keyboard resizing, saved
  dimensions, and limits that keep it within the current display.
- Subtle background highlighting for hovered native log rows, updated while
  scrolling or receiving requests.
- Native macOS menu bar app with a standard-width one-hour activity icon and
  red error strip, a half-screen dropdown with a native stacked bar chart,
  and compact colored request lines with usage, latency, and text previews.
- macOS app icon, drag-and-drop DMG installer with an Applications shortcut,
  and a right-click Start at login control.
- Right-click controls for reconnect/config/quit, a connection status circle,
  and a bottom-center Live button for returning from scrolling or time filters.
- Native log MCP checkbox (off by default) and fixed width request text.
- Native live log limited to the last hour with lazy scroll backfill, chart
  click/drag time filters, on-demand history, and a Live reset control.
- Clickable native log rows open a full JSON inspector with syntax colors,
  indentation, collapsed child objects/arrays, and search with previous/next
  navigation that reveals and highlights matches inside collapsed branches.
- Native JSON inspector word-wrap checkbox, indented continuation lines,
  and search navigation within wrapped values.
- Native JSON inspector Expand All / Collapse All button and single-click
  expansion/collapse anywhere on container rows.
- Read-only native SSE monitoring and paginated history, reconnect/wake
  recovery, existing YAML configuration support, and an offline demo mode.
- `make macos` app packaging, `make macos-dmg` release packaging, `make
  macos-test`, and macOS CI checks. Tag releases now attach a macOS DMG to the
  GoReleaser draft alongside the TUI binaries and archives.

### Fixed

- TUI chart uses clock-aligned stacked green/red bars without idle rebinning.
- TUI input/output tokens use comma separators and distinguish unknown from zero;
  usage parsing includes nested prompt/completion tokens and cached-token ratios.
- Content hydration preserves previews, viewport position, and live outcomes
  without replaying terminal events or counting requests again.
- Native MCP checkbox now includes MCP calls in both activity charts and request
  totals as well as the log; MCP stays excluded by default.
- Native log timestamps occupy a fixed-width, right-padded slot. Text tightening
  and font scaling are disabled so backfilled rows keep the same spacing.
- Native history loading fetches only intersecting UTC days and stops pagination
  at the requested start time. Repeated pages and stalled loads surface an error
  instead of leaving the loading message active indefinitely; cancellation and
  live coverage updates clear stale loading status.
- Native request log input/output token counts use comma thousands separators.
- Native dropdown counts share the title row, using compact request/outcome
  totals. A single bottom Live button replaces both previous return controls.
- Chart selection reuses connected stream coverage instead of backfilling tiny
  timer gaps; loading status clears once the displayed interval is covered.
- Native activity charts use fixed clock-aligned buckets and skip unchanged
  redraws, preventing idle jitter. Green/red stacked bars replace area fills.
- Native log row tooltips removed; clickable rows show a pointing-hand cursor
  that updates as rows change or scroll.
- Native dropdown log fits its width and scrolls only vertically; horizontal
  scrolling is available in the JSON popup when word wrapping is off.
- Native JSON tree expansion preserves the clicked item's scroll position;
  manual collapse is no longer undone by an active search.
- Native log previews are restored lazily for visible requests when compact
  audit history omits bodies, and preserved when late history enriches live rows.
- Native request usage now updates from late `usage.*` events, preserving
  token and cache values after completion without double-counting requests.
- TUI hotkey legends use consistent colons after key labels.
- Native right-click menu uses system context-menu placement so all entries
  remain visible below the menu bar.
- Native Start at login menu item can register a previously unseen app service
  and opens Login Items in System Settings when approval is required.

## [1.0.0] - 2026-10-08

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

[Unreleased]: https://github.com/a-bali/gomodeltui/compare/v1.2.0...HEAD
[1.2.0]: https://github.com/a-bali/gomodeltui/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/a-bali/gomodeltui/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/a-bali/gomodeltui/releases/tag/v1.0.0
