import Foundation

/// Never follow a redirect with the admin credential, including same-host redirects.
private final class RefuseRedirects: NSObject, URLSessionTaskDelegate, Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask,
                    willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest,
                    completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

public final class GoModelClient: Sendable {
    private let configuration: Configuration
    private let session: URLSession

    public init(configuration: Configuration) {
        self.configuration = configuration
        let settings = URLSessionConfiguration.ephemeral
        settings.httpShouldSetCookies = false
        settings.httpCookieStorage = nil
        settings.urlCredentialStorage = nil
        settings.urlCache = nil
        settings.timeoutIntervalForRequest = 90
        settings.timeoutIntervalForResource = 86_400
        session = URLSession(configuration: settings, delegate: RefuseRedirects(), delegateQueue: nil)
    }

    public func close() { session.invalidateAndCancel() }

    /// Full bodies are fetched on demand for inspection or missing visible-row
    /// previews; the live log retains only a bounded text preview.
    public func detail(logID: String) async throws -> Data {
        let (data, response) = try await session.data(for: request(path: "admin/audit/detail", query: [URLQueryItem(name: "log_id", value: logID)]))
        try check(response)
        return data
    }

    public func detail(for record: RequestRecord) async throws -> Data {
        if !record.auditLogID.isEmpty { return try await detail(logID: record.auditLogID) }
        // Live events identify the logical request, but the detail endpoint needs
        // its persisted audit ID. Search metadata rather than guessing that ID.
        var offset = 0
        while true {
            try Task.checkCancellation()
            let query = [URLQueryItem(name: "search", value: record.logicalID), URLQueryItem(name: "limit", value: "100"),
                URLQueryItem(name: "offset", value: String(offset))]
            let (data, response) = try await session.data(for: request(path: "admin/audit/log", query: query))
            try check(response)
            guard let page = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let entries = page["entries"] as? [[String: Any]], let total = number(page["total"]), total >= 0 else { throw APIError.invalidHistory }
            for entry in entries where entry["request_id"] as? String == record.logicalID {
                if let id = entry["id"] as? String, !id.isEmpty { return try await detail(logID: id) }
            }
            offset += entries.count
            if entries.isEmpty || Double(offset) >= total { throw APIError.http(404) }
        }
    }

    private func request(path: String, query: [URLQueryItem] = []) throws -> URLRequest {
        let url = configuration.baseURL.appendingPathComponent(path)
        guard var components = URLComponents(url: url, resolvingAgainstBaseURL: false) else { throw ConfigurationError.invalidURL }
        if !query.isEmpty { components.queryItems = query }
        guard let endpoint = components.url else { throw ConfigurationError.invalidURL }
        var request = URLRequest(url: endpoint)
        request.httpMethod = "GET"
        request.setValue("Bearer " + configuration.apiKey, forHTTPHeaderField: "Authorization")
        return request
    }

    private func check(_ response: URLResponse) throws {
        guard let response = response as? HTTPURLResponse else { throw SSEError.unexpectedResponse }
        guard (200..<300).contains(response.statusCode) else { throw APIError.http(response.statusCode) }
    }

    public func stream(lastEventID: String, connected: @Sendable () async -> Void,
                       receive: @Sendable (AuditEvent) async -> Void) async throws {
        var request = try request(path: "admin/live/logs")
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        if !lastEventID.isEmpty { request.setValue(lastEventID, forHTTPHeaderField: "Last-Event-ID") }
        let (bytes, response) = try await session.bytes(for: request)
        defer { bytes.task.cancel() }
        try check(response)
        guard response.mimeType?.lowercased() == "text/event-stream" else { throw SSEError.unexpectedResponse }
        await connected()
        var parser = SSEFramer()
        for try await byte in bytes {
            try Task.checkCancellation()
            if let event = try parser.consume(byte: byte) { await receive(event) }
        }
        throw SSEError.disconnected
    }

    public func history(start: Date, end: Date,
                        receive: @escaping @Sendable ([AuditEvent]) async -> Void) async throws {
        try await withThrowingTaskGroup(of: Void.self) { group in
            group.addTask { try await self.loadHistory(start: start, end: end, receive: receive) }
            group.addTask {
                try await Task.sleep(for: .seconds(120))
                throw APIError.historyTimeout
            }
            defer { group.cancelAll() }
            try await group.next()
        }
    }

    private func loadHistory(start: Date, end: Date,
                             receive: @Sendable ([AuditEvent]) async -> Void) async throws {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.dateFormat = "yyyy-MM-dd"
        var offset = 0
        var previousPage: [String]?
        while true {
            try Task.checkCancellation()
            // GoModel interprets date filters in UTC and returns newest first.
            // Request only intersecting days; stop once we cross the exact start.
            let query = [
                URLQueryItem(name: "start_date", value: formatter.string(from: start)),
                URLQueryItem(name: "end_date", value: formatter.string(from: end)),
                URLQueryItem(name: "limit", value: "100"),
                URLQueryItem(name: "offset", value: String(offset)),
            ]
            var pageRequest = try request(path: "admin/audit/log", query: query)
            pageRequest.timeoutInterval = 20
            let (data, response) = try await session.data(for: pageRequest)
            try check(response)
            guard let page = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let entries = page["entries"] as? [[String: Any]],
                  let total = number(page["total"]), total >= 0 else { throw APIError.invalidHistory }
            var events: [AuditEvent] = []
            var identities: [String] = []
            var crossedStart = false
            for entry in entries {
                guard let at = parseTimestamp(string(entry["timestamp"])),
                      let id = entry["request_id"] as? String, !id.isEmpty else { throw APIError.invalidHistory }
                if at >= start && at <= end { events.append(try AuditEvent.history(entry)) }
                identities.append((entry["id"] as? String ?? id) + "@" + string(entry["timestamp"]))
                if at < start { crossedStart = true }
            }
            // New writes can shift offset pages; never loop on the same page
            // or report complete coverage when pagination makes no progress.
            if !identities.isEmpty && identities == previousPage { throw APIError.historyPagination }
            previousPage = identities
            await receive(events)
            offset += entries.count
            if crossedStart || entries.isEmpty || Double(offset) >= total { return }
        }
    }
}

public enum APIError: LocalizedError {
    case http(Int), invalidHistory, historyTimeout, historyPagination
    public var errorDescription: String? {
        switch self {
        case .http(let status): return "GoModel returned HTTP \(status). Check the server URL and admin API key."
        case .invalidHistory: return "GoModel returned malformed audit history. Loaded history may be incomplete."
        case .historyTimeout: return "History loading timed out. Reconnect to retry."
        case .historyPagination: return "History pagination stopped making progress. Reconnect to retry."
        }
    }
}
