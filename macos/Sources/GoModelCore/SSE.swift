import Foundation

/// Incremental SSE framing; supports multiline data, comments, CRLF, and id-only frames.
public struct SSEParser: Sendable {
    private var id: String?
    private var type = ""
    private var data = ""
    private var bytes = 0
    private var dataLines = 0
    private let maximumBytes = 8 * 1024 * 1024
    public init() {}

    public mutating func consume(line: String) throws -> AuditEvent? {
        if line.isEmpty {
            defer { id = nil; type = ""; data = ""; bytes = 0; dataLines = 0 }
            guard id != nil || !data.isEmpty else { return nil }
            return AuditEvent(id: id ?? "", type: type.isEmpty ? "message" : type, data: Data(data.utf8), hasExplicitID: id != nil)
        }
        bytes += line.utf8.count
        guard bytes <= maximumBytes else { throw SSEError.oversizedEvent }
        if line.hasPrefix(":") { return nil }
        let parts = line.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        let field = String(parts[0])
        var value = parts.count > 1 ? String(parts[1]) : ""
        if value.hasPrefix(" ") { value.removeFirst() }
        switch field {
        case "id": if !value.contains("\0") { id = value }
        case "event": type = value
        case "data":
            if dataLines > 0 { data += "\n" }
            data += value
            dataLines += 1
        default: break
        }
        return nil
    }
}

/// Bound line allocation before decoding, including a server that never sends a newline.
public struct SSEFramer: Sendable {
    private var parser = SSEParser()
    private var line = Data()
    private var skipLF = false
    private var firstLine = true
    public init() {}

    public mutating func consume(byte: UInt8) throws -> AuditEvent? {
        if byte == 10, skipLF { skipLF = false; return nil }
        skipLF = false
        if byte == 10 || byte == 13 {
            skipLF = byte == 13
            var text = String(decoding: line, as: UTF8.self)
            line.removeAll(keepingCapacity: true)
            if firstLine {
                if text.hasPrefix("\u{feff}") { text.removeFirst() }
                firstLine = false
            }
            return try parser.consume(line: text)
        }
        guard line.count < 8 * 1024 * 1024 else { throw SSEError.oversizedEvent }
        line.append(byte)
        return nil
    }
}

public enum SSEError: LocalizedError {
    case oversizedEvent, unexpectedResponse, disconnected
    public var errorDescription: String? {
        switch self {
        case .oversizedEvent: return "The live stream sent an oversized event. Reconnecting."
        case .unexpectedResponse: return "GoModel did not return an event stream."
        case .disconnected: return "Live stream disconnected. Reconnecting."
        }
    }
}
