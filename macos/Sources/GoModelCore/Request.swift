import Foundation

public struct RequestRecord: Sendable, Identifiable {
    public var id: String
    public var logicalID: String
    public var timestamp: Date
    public var startedAt: Date
    public var appPath = ""
    public var sessionID = ""
    public var sourceModel = ""
    public var destinationModel = ""
    public var provider = ""
    public var endpoint = ""
    public var auditLogID = ""
    public var inputTokens: Int?
    public var outputTokens: Int?
    public var cacheRatio: Double?
    public var duration: TimeInterval?
    public var status: Int?
    public var error = ""
    public var preview = ""
    public var terminal = false
    public var success = false
    public var failover = false
    public var attempts: [RequestAttempt] = []
    var liveTerminal = false

    public init(id: String, timestamp: Date) {
        self.id = id
        self.logicalID = id
        self.timestamp = timestamp
        self.startedAt = timestamp
    }

    public var isMCP: Bool {
        provider.lowercased() == "mcp" || endpoint == "/mcp" || endpoint.hasPrefix("/mcp/")
    }

    /// Expands a completed logical request into the same per-attempt rows used
    /// by the TUI. The final row keeps the logical ID and request-level usage.
    public func logRows() -> [RequestRecord] {
        guard terminal, attempts.count > 1 else { return [self] }
        var collapsedAttempts: [RequestAttempt] = []
        for attempt in attempts {
            if let previous = collapsedAttempts.last, !attempt.success, !previous.success,
               attempt.deduplicationKey == previous.deduplicationKey {
                collapsedAttempts[collapsedAttempts.count - 1] = attempt
            } else {
                collapsedAttempts.append(attempt)
            }
        }
        return collapsedAttempts.enumerated().map { index, attempt in
            var row = self
            row.logicalID = logicalID
            row.id = index == collapsedAttempts.count - 1 ? logicalID : "\(logicalID)/attempt-\(attempt.sequence)"
            row.timestamp = attempt.startedAt ?? timestamp
            row.provider = attempt.providerName.isEmpty ? attempt.providerType : attempt.providerName
            if !attempt.model.isEmpty {
                row.destinationModel = row.provider.isEmpty || attempt.model.hasPrefix(row.provider + "/")
                    ? attempt.model : row.provider + "/" + attempt.model
            }
            row.status = attempt.status
            row.error = attempt.error
            row.duration = attempt.duration
            row.success = attempt.success
            row.failover = index > 0
            if index < collapsedAttempts.count - 1 {
                row.inputTokens = nil; row.outputTokens = nil; row.cacheRatio = nil
                row.preview = ""
            }
            return row
        }
    }
}

public struct RequestAttempt: Sendable {
    public var sequence: Int
    public var providerType: String
    public var providerName: String
    public var model: String
    public var status: Int?
    public var success: Bool
    public var error: String
    public var startedAt: Date?
    public var duration: TimeInterval?

    var deduplicationKey: String { "\(providerName)|\(providerType)|\(status ?? 0)|\(error)" }
}

public struct AuditEvent: Sendable {
    public var id: String
    public var type: String
    public var data: Data
    public var historical: Bool
    public var hasExplicitID: Bool

    public init(id: String = "", type: String, data: Data, historical: Bool = false, hasExplicitID: Bool = false) {
        self.id = id
        self.type = type
        self.data = data
        self.historical = historical
        self.hasExplicitID = hasExplicitID || !id.isEmpty
    }

    public static func history(_ entry: [String: Any]) throws -> AuditEvent {
        let payload: [String: Any] = [
            "request_id": entry["request_id"] ?? "",
            "timestamp": entry["timestamp"] ?? "",
            "type": "audit.completed", "data": entry,
        ]
        return AuditEvent(type: "audit.completed", data: try JSONSerialization.data(withJSONObject: payload), historical: true)
    }
}

public enum AuditError: LocalizedError {
    case malformedEvent
    public var errorDescription: String? { "Skipped a malformed audit event." }
}

public struct RequestStore: Sendable {
    public private(set) var records: [String: RequestRecord] = [:]
    public init() {}

    @discardableResult
    public mutating func apply(_ event: AuditEvent, now: Date = Date()) throws -> Bool {
        if event.type == "heartbeat" || event.data.isEmpty { return false }
        guard let payload = try JSONSerialization.jsonObject(with: event.data) as? [String: Any] else {
            throw AuditError.malformedEvent
        }
        guard let id = payload["request_id"] as? String, !id.isEmpty else { return false }
        let type = event.type.isEmpty || event.type == "message" ? string(payload["type"]) : event.type
        let usageEvent = type.hasPrefix("usage.")
        guard type.hasPrefix("audit.") || usageEvent else { return false }
        guard let fields = payload["data"] as? [String: Any] else { throw AuditError.malformedEvent }
        let ending = !usageEvent && (type.hasSuffix(".completed") || type.hasSuffix(".failed"))
        var record = records[id] ?? RequestRecord(id: id, timestamp: parseTimestamp(string(payload["timestamp"])) ?? parseTimestamp(string(fields["timestamp"])) ?? now)
        if !usageEvent, let logID = fields["id"] as? String, !logID.isEmpty { record.auditLogID = logID }
        // A late historical snapshot must not overwrite a newer live completion,
        // and replayed started/progress events must not resurrect completed work.
        if record.liveTerminal && event.historical {
            mergeUsage(fields, into: &record, missingOnly: true)
            if record.preview.isEmpty { record.preview = collapsed(requestPreview(dictionary(fields["data"]))) }
            records[id] = record
            return true
        }
        if record.terminal && !ending && !usageEvent { return false }
        if usageEvent {
            mergeUsage(fields, into: &record)
            records[id] = record
            return true
        }
        if let at = parseTimestamp(string(payload["timestamp"])) ?? parseTimestamp(string(fields["timestamp"])) {
            record.timestamp = at
        }
        func nonempty(_ key: String, _ old: String) -> String {
            let value = string(fields[key])
            return value.isEmpty ? old : value
        }
        record.appPath = nonempty("user_path", record.appPath)
        record.sessionID = nonempty("session_id", record.sessionID)
        record.sourceModel = nonempty("requested_model", record.sourceModel)
        record.provider = string(fields["provider_name"], fields["provider"]).nilIfEmpty ?? record.provider
        record.endpoint = string(fields["path"], fields["endpoint"]).nilIfEmpty ?? record.endpoint
        let model = string(fields["resolved_model"], fields["model"], fields["requested_model"])
        if !model.isEmpty {
            record.destinationModel = record.provider.isEmpty || model.hasPrefix(record.provider + "/") ? model : record.provider + "/" + model
        }
        mergeUsage(fields, into: &record)
        if let value = number(fields["status_code"] ?? fields["status"]), value >= 0, value < 1000 { record.status = Int(value) }
        if let ns = number(fields["duration_ns"]), ns >= 0 { record.duration = ns / 1e9 }
        else if let ms = number(fields["duration_ms"]), ms >= 0 { record.duration = ms / 1000 }
        let error = string(fields["error"], fields["error_type"])
        if !error.isEmpty { record.error = collapsed(error) }
        let data = dictionary(fields["data"])
        if let attempts = data["attempts"] as? [[String: Any]], attempts.count > 1 {
            record.failover = true
            record.attempts = attempts.enumerated().map { index, item in
                let statusValue = number(item["status_code"])
                let durationNS = number(item["duration_ns"])
                return RequestAttempt(
                    sequence: Int(number(item["seq"]) ?? Double(index + 1)),
                    providerType: string(item["provider_type"]), providerName: string(item["provider_name"]),
                    model: string(item["model"]), status: statusValue.map(Int.init),
                    success: (item["success"] as? Bool) ?? false,
                    error: string(item["error_message"], item["error_code"], item["error_type"]),
                    startedAt: parseTimestamp(string(item["started_at"])),
                    duration: durationNS.map { $0 / 1e9 })
            }
        }
        if let preview = requestPreview(data).nilIfEmpty { record.preview = collapsed(preview) }
        if record.isMCP {
            let body = dictionary(data["request_body"])
            let params = dictionary(body["params"])
            record.sourceModel = "MCP"
            record.destinationModel = [string(body["method"]), string(params["name"])].filter { !$0.isEmpty }.joined(separator: " ")
        }
        if ending {
            if record.duration == nil && !event.historical {
                record.duration = max(0, now.timeIntervalSince(record.startedAt))
            }
            record.terminal = true
            record.liveTerminal = !event.historical
            record.success = (200..<300).contains(record.status ?? 0) && !type.hasSuffix(".failed") && error.isEmpty
            if record.success { record.error = "" }
        }
        records[id] = record
        return true
    }

    public mutating func prune(before: Date) {
        records = records.filter { $0.value.timestamp >= before }
    }

    public mutating func retain(in ranges: [RequestTimeRange]) {
        records = records.filter { _, record in ranges.contains { $0.contains(record.timestamp) } }
    }

    /// Enrich only the bounded preview; never retain full inspector bodies or
    /// replace the authoritative live outcome, timestamp, or usage.
    @discardableResult
    public mutating func fillPreview(id: String, detail: Data) throws -> Bool {
        guard var record = records[id], record.preview.isEmpty else { return false }
        guard let entry = try JSONSerialization.jsonObject(with: detail) as? [String: Any],
              entry["request_id"] as? String == id else { throw AuditError.malformedEvent }
        let preview = collapsed(requestPreview(dictionary(entry["data"])))
        guard !preview.isEmpty else { return false }
        record.preview = preview
        if record.auditLogID.isEmpty { record.auditLogID = string(entry["id"]) }
        records[id] = record
        return true
    }

    public var sorted: [RequestRecord] {
        records.values.sorted { ($0.timestamp, $0.id) < ($1.timestamp, $1.id) }
    }

    public func buckets(now: Date, window: TimeInterval, count: Int = 60) -> [ChartBucket] {
        guard count > 0, window > 0 else { return [] }
        let start = now.addingTimeInterval(-window)
        let interval = window / Double(count)
        var buckets = (0..<count).map { ChartBucket(start: start.addingTimeInterval(Double($0) * interval)) }
        for record in records.values where record.terminal && !record.isMCP && record.timestamp >= start && record.timestamp <= now {
            let index = min(count - 1, max(0, Int(record.timestamp.timeIntervalSince(start) / interval)))
            if record.success { buckets[index].success += 1 } else { buckets[index].failed += 1 }
        }
        return buckets
    }
}

private func mergeUsage(_ fields: [String: Any], into record: inout RequestRecord, missingOnly: Bool = false) {
    let data = dictionary(fields["data"])
    let response = dictionary(data["response_body"])
    let sources = [fields, dictionary(fields["usage"]), dictionary(data["usage"]), dictionary(response["usage"])]
    func firstNumber(_ keys: [String]) -> Double? {
        for source in sources {
            for key in keys { if let value = number(source[key]) { return value } }
        }
        return nil
    }
    if !missingOnly || record.inputTokens == nil,
       let value = firstNumber(["input_tokens", "prompt_tokens"]), value >= 0, value < Double(Int.max) { record.inputTokens = Int(value) }
    if !missingOnly || record.outputTokens == nil,
       let value = firstNumber(["output_tokens", "completion_tokens"]), value >= 0, value < Double(Int.max) { record.outputTokens = Int(value) }
    guard !missingOnly || record.cacheRatio == nil else { return }
    if let ratio = firstNumber(["cached_input_ratio"]) {
        record.cacheRatio = min(1, max(0, ratio))
    } else if let input = record.inputTokens, input > 0 {
        var cached = firstNumber(["cached_input_tokens", "cache_read_input_tokens"])
        for source in sources where cached == nil {
            cached = number(dictionary(source["input_tokens_details"])["cached_tokens"])
                ?? number(dictionary(source["prompt_tokens_details"])["cached_tokens"])
        }
        if let cached { record.cacheRatio = min(1, max(0, cached / Double(input))) }
    }
}

public struct ChartBucket: Equatable, Sendable {
    public var start: Date
    public var success = 0
    public var failed = 0
    public var total: Int { success + failed }
    public init(start: Date, success: Int = 0, failed: Int = 0) {
        self.start = start; self.success = success; self.failed = failed
    }
}

public func parseTimestamp(_ text: String) -> Date? {
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return formatter.date(from: text) ?? ISO8601DateFormatter().date(from: text)
}

func string(_ values: Any?...) -> String {
    for value in values {
        if let text = value as? String, !text.isEmpty { return text }
        if let number = value as? NSNumber { return number.stringValue }
    }
    return ""
}

func number(_ value: Any?) -> Double? {
    let parsed: Double?
    if let number = value as? NSNumber { parsed = number.doubleValue }
    else if let text = value as? String { parsed = Double(text) }
    else { parsed = nil }
    return parsed.flatMap { $0.isFinite ? $0 : nil }
}

func dictionary(_ value: Any?) -> [String: Any] {
    if let object = value as? [String: Any] { return object }
    if let text = value as? String, let data = text.data(using: .utf8),
       let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] { return object }
    return [:]
}

func collapsed(_ text: String) -> String {
    // Bound retained previews; full request/response bodies never enter the store.
    String(text.prefix(4096)).split(whereSeparator: { $0.isWhitespace || $0.isNewline || $0.asciiValue.map { $0 < 32 || $0 == 127 } == true }).joined(separator: " ")
}

extension String {
    var nilIfEmpty: String? { isEmpty ? nil : self }
}

private func contentText(_ value: Any?) -> String {
    if let text = value as? String { return text }
    return (value as? [[String: Any]] ?? []).compactMap { $0["text"] as? String }.joined(separator: " ")
}

private func itemPreview(_ item: [String: Any]) -> String {
    if string(item["type"]) == "reasoning" { return "" }
    if ["function_call", "custom_tool_call"].contains(string(item["type"])) {
        return "tool: " + string(item["name"]) + " " + string(item["arguments"], item["input"])
    }
    let content = contentText(item["content"])
    if !content.isEmpty { return content }
    if let output = item["output"] { return contentText(output) }
    return (item["tool_calls"] as? [[String: Any]] ?? []).compactMap { call in
        let function = dictionary(call["function"])
        guard let name = function["name"] as? String else { return nil }
        return "tool: " + name + " " + string(function["arguments"])
    }.joined(separator: " | ")
}

private func requestPreview(_ data: [String: Any]) -> String {
    let request = dictionary(data["request_body"])
    if let messages = request["messages"] as? [[String: Any]] ?? request["input"] as? [[String: Any]] {
        for message in messages.reversed() {
            if let text = itemPreview(message).nilIfEmpty { return text }
        }
    }
    if let input = request["input"] as? String, !input.isEmpty { return input }
    let response = dictionary(data["response_body"])
    if let text = contentText(response["content"]).nilIfEmpty { return text }
    if let text = response["output_text"] as? String, !text.isEmpty { return text }
    for item in (response["output"] as? [[String: Any]] ?? []).reversed() {
        if let text = itemPreview(item).nilIfEmpty { return text }
    }
    for choice in (response["choices"] as? [[String: Any]] ?? []).reversed() {
        if let text = itemPreview(dictionary(choice["message"])).nilIfEmpty { return text }
    }
    return ""
}
