import Foundation
import GoModelCore

@MainActor
final class Monitor {
    typealias HistoryLoader = @Sendable (RequestTimeRange, @escaping @Sendable ([AuditEvent]) async -> Void) async throws -> Void
    static let windows: [(String, TimeInterval)] = [("5 minutes", 300), ("15 minutes", 900), ("1 hour", 3600),
        ("3 hours", 10_800), ("6 hours", 21_600), ("12 hours", 43_200), ("24 hours", 86_400)]
    private(set) var store = RequestStore()
    private(set) var activity = ActivityStore()
    private(set) var logWindow = LogTimeWindow()
    private(set) var chartEnd: Date?
    private var chartCoverage = HistoryCoverage()
    private var detailCoverage = HistoryCoverage()
    private(set) var connected = false
    private(set) var connectionStatus = "Not configured"
    private(set) var historyStatus = ""
    private(set) var eventWarning = ""
    private(set) var configuration: Configuration?
    private(set) var loadingHistory = false
    private(set) var revision = 0
    private(set) var includeMCP = false
    var window: TimeInterval = 3600
    var changed: (() -> Void)?
    private var client: GoModelClient?
    private var streamTask: Task<Void, Never>?
    private(set) var historyTask: Task<Void, Never>?
    private var generation = UUID()
    private var historyGeneration = UUID()
    private var lastEventID = ""
    private var demo = false
    private var lastPrune = Date.distantPast
    private var streamCoveredThrough: Date?
    private let clock: @Sendable () -> Date
    private var historyLoader: HistoryLoader?
    private var demoDetail: (@Sendable (String) -> Data?)?
    private var previewTasks: [String: Task<Void, Never>] = [:]
    private var previewRows: [RequestRecord] = []
    private var previewCompleted = Set<String>()
    private var previewAttempts: [String: Date] = [:]
    private var previewGeneration = UUID()

    init(now: @escaping @Sendable () -> Date = { Date() }, historyLoader: HistoryLoader? = nil) {
        clock = now; self.historyLoader = historyLoader
    }
    var now: Date { clock() }

    var retention: TimeInterval { 86_400 }
    var availableWindow: TimeInterval { min(window, retention) }
    var displayChartEnd: Date { chartEnd ?? ActivityStore.alignedEnd(now: now, window: availableWindow) }
    var loadingLogHistory: Bool {
        loadingHistory && !missingLogHistory(at: now).isEmpty
    }

    private func missingLogHistory(at date: Date) -> [RequestTimeRange] {
        let retained = RequestTimeRange(start: date.addingTimeInterval(-retention), end: date)
        guard let range = logWindow.range(now: date).intersection(retained) else { return [] }
        return detailCoverage.missing(in: range)
    }

    private func updateHistoryStatus(at date: Date) {
        let missing = !missingLogHistory(at: date).isEmpty ||
            (logWindow.selection == nil && !chartCoverage.missing(in: chartRange(at: date)).isEmpty)
        historyStatus = loadingHistory && missing ? "Loading history…" : ""
    }

    func logRecords(at date: Date? = nil) -> [RequestRecord] {
        let date = date ?? now
        let range = logWindow.range(now: date)
        return store.sorted.filter {
            $0.timestamp >= range.start && ($0.timestamp < range.end || logWindow.selection == nil && $0.timestamp == range.end)
        }
    }

    private func liveRange(at date: Date) -> RequestTimeRange {
        RequestTimeRange(start: date.addingTimeInterval(-3600), end: date)
    }
    private func chartRange(at date: Date) -> RequestTimeRange {
        let end = chartEnd ?? ActivityStore.alignedEnd(now: date, window: availableWindow)
        return RequestTimeRange(start: end.addingTimeInterval(-availableWindow), end: min(end, date))
    }
    private func retainedRanges(at date: Date) -> [RequestTimeRange] {
        let retained = RequestTimeRange(start: date.addingTimeInterval(-retention), end: date.addingTimeInterval(0.001))
        return [liveRange(at: date), logWindow.range(now: date)].compactMap {
            RequestTimeRange(start: $0.start, end: $0.end.addingTimeInterval(0.001)).intersection(retained)
        }
    }
    private func retainDetails() {
        let ranges = retainedRanges(at: now)
        store.retain(in: ranges)
        detailCoverage.retain(in: ranges)
        let ids = Set(store.records.keys)
        previewCompleted.formIntersection(ids)
        previewAttempts = previewAttempts.filter { ids.contains($0.key) }
    }

    func configure(path: String) {
        if demo { historyLoader = nil }
        demoDetail = nil
        stop()
        previewCompleted = []; previewAttempts = [:]
        store = RequestStore()
        activity = ActivityStore(); logWindow = LogTimeWindow(); chartEnd = nil
        chartCoverage = HistoryCoverage(); detailCoverage = HistoryCoverage(); streamCoveredThrough = nil
        revision += 1
        lastEventID = ""
        historyStatus = ""
        eventWarning = ""
        demo = false
        do {
            let configuration = try Configuration.load(path: path)
            self.configuration = configuration
            client = GoModelClient(configuration: configuration)
            reconnect()
        } catch {
            configuration = nil
            connectionStatus = error.localizedDescription
            changed?()
        }
    }

    func stop() {
        stopPreviews()
        generation = UUID()
        historyGeneration = UUID()
        streamTask?.cancel()
        historyTask?.cancel()
        streamTask = nil
        historyTask = nil
        loadingHistory = false
        historyStatus = ""
        client?.close()
        client = nil
        connected = false
    }

    func reconnect() {
        guard !demo, let configuration else { return }
        stopPreviews()
        streamTask?.cancel()
        historyTask?.cancel()
        client?.close()
        let client = GoModelClient(configuration: configuration)
        self.client = client
        let token = UUID()
        generation = token
        connected = false
        connectionStatus = "Connecting…"
        ensureHistory(force: true)
        changed?()
        streamTask = Task { [weak self] in
            var delay: UInt64 = 2
            while !Task.isCancelled {
                guard let self, self.generation == token else { return }
                do {
                    try await client.stream(lastEventID: self.lastEventID, connected: { [weak self] in
                        await self?.didConnect(token: token)
                    }, receive: { [weak self] event in
                        await self?.receive(event, token: token)
                    })
                } catch {
                    guard !Task.isCancelled, self.generation == token else { return }
                    let wasConnected = self.connected
                    self.connected = false
                    self.connectionStatus = "\(Self.safeError(error)) Retrying in \(delay)s."
                    self.changed?()
                    if wasConnected { delay = 2 }
                    do { try await Task.sleep(nanoseconds: delay * 1_000_000_000) } catch { return }
                    delay = min(30, delay * 2)
                }
            }
        }
    }

    private func didConnect(token: UUID) {
        guard generation == token else { return }
        connected = true
        connectionStatus = "Connected"
        ensureHistory(force: true)
        streamCoveredThrough = now
        changed?()
    }

    private func receive(_ event: AuditEvent, token: UUID) {
        guard generation == token else { return }
        if event.hasExplicitID { lastEventID = event.id }
        do {
            if try store.apply(event) {
                // Capture lightweight chart activity before old request details are discarded.
                if let id = (try? JSONSerialization.jsonObject(with: event.data) as? [String: Any])?["request_id"] as? String,
                   let record = store.records[id] { activity.apply(record) }
                revision += 1
            }
        } catch {
            eventWarning = "Skipped a malformed live event."
        }
        // Rendering is coalesced by the app's one-second timer.
    }

    func setIncludeMCP(_ value: Bool) {
        guard includeMCP != value else { return }
        includeMCP = value
        revision += 1
        changed?()
    }

    func selectWindow(_ value: TimeInterval) {
        window = value
        logWindow.selectWindow(min(value, retention)); chartEnd = nil
        retainDetails(); revision += 1
        ensureHistory()
        changed?()
    }

    func selectRange(_ range: RequestTimeRange, chartEnd: Date) {
        guard let range = range.intersection(RequestTimeRange(start: now.addingTimeInterval(-retention), end: now.addingTimeInterval(0.001))) else { return }
        self.chartEnd = chartEnd
        logWindow.select(range)
        retainDetails(); revision += 1
        ensureHistory(); changed?()
    }

    func pauseLog() {
        guard logWindow.selection == nil else { return }
        logWindow.pause(now: now)
    }

    func loadEarlier() {
        guard !loadingHistory,
              logWindow.loadEarlier(now: now, earliest: now.addingTimeInterval(-retention)) != nil else { return }
        revision += 1
        ensureHistory(); changed?()
    }

    func resumeLogFollowing() {
        guard logWindow.selection == nil, logWindow.browsingStart != nil else { return }
        logWindow.resumeFollowing()
        retainDetails(); revision += 1
        changed?()
    }

    func resumeLive() {
        logWindow.resumeLive(); chartEnd = nil; window = 3600
        retainDetails(); revision += 1
        ensureHistory(); changed?()
    }

    func detailLoader(for record: RequestRecord) -> @Sendable () async throws -> Data {
        if demo, let demoDetail {
            return {
                guard let data = demoDetail(record.logicalID) else { throw APIError.http(404) }
                return data
            }
        }
        let client = self.client
        return {
            guard let client else { throw SSEError.disconnected }
            return try await client.detail(for: record)
        }
    }

    func loadVisiblePreviews(_ rows: [RequestRecord]) {
        var seen = Set<String>()
        previewRows = rows.reversed().compactMap { row in
            guard seen.insert(row.logicalID).inserted else { return nil }
            return store.records[row.logicalID]
        }.prefix(60).reversed()
        let visible = Set(previewRows.map(\.id))
        for id in Array(previewTasks.keys) where !visible.contains(id) {
            previewTasks.removeValue(forKey: id)?.cancel()
            previewAttempts[id] = nil
        }
        startPreviews()
    }

    func stopPreviews() {
        previewGeneration = UUID()
        for (id, task) in previewTasks { task.cancel(); previewAttempts[id] = nil }
        previewTasks = [:]; previewRows = []
    }

    private func startPreviews() {
        for row in previewRows {
            guard previewTasks.count < 2 else { break }
            guard let record = store.records[row.id], record.terminal, record.preview.isEmpty,
                  record.error.isEmpty, !record.isMCP, !previewCompleted.contains(record.id), previewTasks[record.id] == nil,
                  now.timeIntervalSince(previewAttempts[record.id] ?? .distantPast) >= 30 else { continue }
            let loader = detailLoader(for: record), token = previewGeneration
            previewAttempts[record.id] = now
            previewTasks[record.id] = Task { [weak self] in
                do {
                    let data = try await loader()
                    try Task.checkCancellation()
                    guard let self, self.previewGeneration == token else { return }
                    if try self.store.fillPreview(id: record.id, detail: data) { self.revision += 1 }
                    self.previewCompleted.insert(record.id)
                } catch {
                    guard !Task.isCancelled else { return }
                    // Missing or not-yet-persisted bodies can be retried without
                    // interrupting live monitoring or replacing the row's text.
                }
                guard let self, self.previewGeneration == token, !Task.isCancelled else { return }
                self.previewTasks[record.id] = nil
                self.startPreviews(); self.changed?()
            }
        }
    }

    func ensureHistory(force: Bool = false) {
        let loader: HistoryLoader
        if let historyLoader { loader = historyLoader }
        else if let client {
            loader = { range, receive in try await client.history(start: range.start, end: range.end, receive: receive) }
        } else { return }
        let date = now
        // A connected SSE stream already covers the time since the last timer
        // tick. Don't scan audit history for that sub-second gap on every click.
        if !force { advanceStreamCoverage(to: date) }
        let retained = RequestTimeRange(start: date.addingTimeInterval(-retention), end: date)
        var planned = HistoryCoverage()
        var jobs: [RequestTimeRange] = []
        func add(_ ranges: [RequestTimeRange]) {
            for range in ranges {
                guard let range = range.intersection(retained) else { continue }
                for missing in planned.missing(in: range) { jobs.append(missing); planned.insert(missing) }
            }
        }
        if force {
            add([RequestTimeRange(start: max(retained.start, streamCoveredThrough ?? date.addingTimeInterval(-3600)), end: date)])
        }
        // Request details first; a wide chart must not force its full history into the log.
        add(detailCoverage.missing(in: logWindow.range(now: date)))
        add(detailCoverage.missing(in: liveRange(at: date)))
        add(chartCoverage.missing(in: chartRange(at: date)))
        historyTask?.cancel()
        let token = UUID()
        historyGeneration = token
        guard !jobs.isEmpty else {
            historyTask = nil; loadingHistory = false; historyStatus = ""
            return
        }
        loadingHistory = true
        updateHistoryStatus(at: date)
        historyTask = Task { [weak self] in
            do {
                for range in jobs {
                    try Task.checkCancellation()
                    try await loader(range) { [weak self] events in
                        await self?.receiveHistory(events, token: token)
                    }
                    guard let self, self.historyGeneration == token, !Task.isCancelled else { return }
                    self.chartCoverage.insert(range); self.detailCoverage.insert(range)
                    self.retainDetails()
                    self.updateHistoryStatus(at: self.now)
                    self.changed?()
                }
                guard let self, self.historyGeneration == token, !Task.isCancelled else { return }
                self.loadingHistory = false
                self.historyTask = nil
                self.historyStatus = ""
                self.changed?()
            } catch {
                guard let self, self.historyGeneration == token, !Task.isCancelled else { return }
                self.loadingHistory = false
                self.historyTask = nil
                self.historyStatus = "History incomplete. \(Self.safeError(error))"
                self.changed?()
            }
        }
    }

    private func receiveHistory(_ events: [AuditEvent], token: UUID) {
        guard historyGeneration == token else { return }
        for event in events {
            do {
                if try store.apply(event) {
                    if let payload = try JSONSerialization.jsonObject(with: event.data) as? [String: Any],
                       let id = payload["request_id"] as? String, let record = store.records[id] { activity.apply(record) }
                    revision += 1
                }
            } catch {
                eventWarning = "Skipped a malformed historical event."
            }
        }
        retainDetails()
    }

    private func advanceStreamCoverage(to date: Date) {
        if connected, let streamCoveredThrough, streamCoveredThrough < date {
            let range = RequestTimeRange(start: streamCoveredThrough, end: date)
            chartCoverage.insert(range); detailCoverage.insert(range)
            self.streamCoveredThrough = date
        }
    }

    func tick() {
        let date = now
        advanceStreamCoverage(to: date)
        if loadingHistory { updateHistoryStatus(at: date) }
        guard date.timeIntervalSince(lastPrune) >= 15 else { return }
        lastPrune = date
        let count = store.records.count
        retainDetails()
        activity.prune(before: date.addingTimeInterval(-retention))
        chartCoverage.retain(in: [RequestTimeRange(start: date.addingTimeInterval(-retention), end: date)])
        if store.records.count != count { revision += 1 }
    }

    static func safeError(_ error: Error) -> String {
        if error is APIError || error is SSEError { return error.localizedDescription }
        // URLSession errors may contain URLs. Never surface arbitrary server bodies or config values.
        return "Unable to reach GoModel."
    }

    func loadDemo() {
        stop()
        demo = true
        configuration = nil
        connected = true
        connectionStatus = "Demo · sample data"
        historyStatus = ""
        let now = self.now
        store = RequestStore(); activity = ActivityStore(); logWindow = LogTimeWindow(); chartEnd = nil
        var events: [AuditEvent] = []
        let paths = ["/Applications/Editor.app", "/agent/research", "/Applications/Terminal.app", "/app/release-bot"]
        let models = ["claude-sonnet-4-5", "gpt-5", "deepseek-v4.1-flash", "claude-opus-4-1"]
        let prompts = [
            "Summarize the deployment notes and highlight the two changes that need follow-up.",
            "Review this patch for race conditions and explain the smallest safe fix.",
            "Find the relevant API contract and draft a concise implementation plan.",
            "Compare the latest run with the previous release and list any regressions.",
            "Trace this error through the request path and suggest a focused diagnostic.",
            "Turn these meeting notes into decisions, owners, and next steps.",
            "Check the migration for compatibility with existing configuration files.",
            "Explain why this test is flaky and propose a deterministic replacement.",
        ]
        for index in 0..<780 {
            let at = now.addingTimeInterval(-Double(index * 5 + (index % 7)))
            let failed = index % 19 == 5
            let fields: [String: Any] = [
                "user_path": paths[index % paths.count], "session_id": String(format: "demo-session-%03d", 120+index%37),
                "provider_name": "opencode-go", "requested_model": "auto",
                "resolved_model": models[index%models.count],
                "status_code": failed ? 429 : 200, "input_tokens": 1_234 + index%9*816,
                "output_tokens": 82 + index%7*47, "cached_input_ratio": Double(72 + index%4*6) / 100,
                "duration_ms": failed ? 32_410 : 380 + (index%11)*173,
                "error": failed ? "Rate limit exceeded" : "",
                "data": ["request_body": ["messages": [["role": "user", "content": prompts[index % prompts.count]]]]],
            ]
            let payload: [String: Any] = ["request_id": "demo-\(index)", "timestamp": ISO8601DateFormatter().string(from: at), "data": fields]
            if let data = try? JSONSerialization.data(withJSONObject: payload) {
                let event = AuditEvent(type: failed ? "audit.failed" : "audit.completed", data: data)
                _ = try? store.apply(event)
                if let record = store.records["demo-\(index)"] { activity.apply(record) }
                events.append(event)
            }
        }
        let sample = events
        demoDetail = { id in
            guard let event = sample.first(where: { event in
                (try? JSONSerialization.jsonObject(with: event.data) as? [String: Any])?["request_id"] as? String == id
            }), let payload = try? JSONSerialization.jsonObject(with: event.data) as? [String: Any],
               var fields = payload["data"] as? [String: Any] else { return nil }
            fields["request_id"] = id; fields["timestamp"] = payload["timestamp"]
            fields["id"] = "audit-" + id
            return try? JSONSerialization.data(withJSONObject: fields)
        }
        historyLoader = { range, receive in
            let filtered = sample.filter { event in
                guard let payload = try? JSONSerialization.jsonObject(with: event.data) as? [String: Any],
                      let timestamp = payload["timestamp"] as? String, let date = parseTimestamp(timestamp) else { return false }
                return range.contains(date)
            }
            await receive(filtered)
        }
        chartCoverage = HistoryCoverage(); detailCoverage = HistoryCoverage()
        chartCoverage.insert(RequestTimeRange(start: now.addingTimeInterval(-86_400), end: now))
        detailCoverage.insert(liveRange(at: now))
        retainDetails()
        revision += 1
        changed?()
    }
}
