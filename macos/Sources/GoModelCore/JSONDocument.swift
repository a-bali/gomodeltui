import Foundation

/// Immutable nodes keep AppKit outline identity stable as branches open and close.
public final class JSONNode: NSObject, @unchecked Sendable {
    public enum Kind: Sendable { case object, array, string, number, boolean, null, closing }
    public let id: Int
    public let parentID: Int?
    public let kind: Kind
    public let prefix: String
    public let value: String
    public let comma: Bool
    public let children: [JSONNode]
    public var expandable: Bool { !children.isEmpty }
    public var count: Int { max(0, children.count - 1) }
    public var searchText: String { prefix + value + (comma && !expandable ? "," : "") }

    init(id: Int, parentID: Int?, kind: Kind, prefix: String, value: String, comma: Bool, children: [JSONNode] = []) {
        self.id = id; self.parentID = parentID; self.kind = kind; self.prefix = prefix
        self.value = value; self.comma = comma; self.children = children
    }
    public func text(expanded: Bool) -> String {
        if expandable && !expanded {
            let noun = kind == .object ? "member" : "item"
            return prefix + (kind == .object ? "{ … }" : "[ … ]") + (comma ? "," : "") + "  // \(count) \(noun)\(count == 1 ? "" : "s")"
        }
        return prefix + value + (comma && !expandable ? "," : "")
    }
}

public struct JSONMatch: Equatable, Sendable {
    public let nodeID: Int
    public let range: NSRange
}

public struct JSONDocument: Sendable {
    public let root: JSONNode
    public let nodes: [JSONNode]
    public let byID: [Int: JSONNode]

    public init(data: Data) throws {
        let parser = try JSONTreeParser(data: data)
        root = try parser.parse()
        nodes = parser.nodes.sorted { $0.id < $1.id }
        byID = Dictionary(uniqueKeysWithValues: nodes.map { ($0.id, $0) })
    }

    /// Includes hidden descendants, in document order, and every occurrence per value.
    public func matches(for query: String) throws -> [JSONMatch] {
        guard !query.isEmpty else { return [] }
        var matches: [JSONMatch] = []
        for node in nodes where node.kind != .closing {
            try Task.checkCancellation()
            let text = node.searchText as NSString
            var cursor = 0
            while cursor < text.length {
                try Task.checkCancellation()
                let range = text.range(of: query, options: [.caseInsensitive], range: NSRange(location: cursor, length: text.length - cursor))
                guard range.location != NSNotFound, range.length > 0 else { break }
                matches.append(JSONMatch(nodeID: node.id, range: range))
                cursor = NSMaxRange(range)
            }
        }
        return matches
    }

    public func ancestors(of node: JSONNode) -> [JSONNode] {
        var ancestors: [JSONNode] = [], parentID = node.parentID
        while let id = parentID, let parent = byID[id] {
            ancestors.append(parent); parentID = parent.parentID
        }
        return ancestors.reversed()
    }
}

private enum JSONDocumentError: Error { case invalidJSON }

/// Preserve number literals (including large integers), while decoding strings
/// for readable Unicode and search. No request value is shortened or rounded.
private final class JSONTreeParser {
    let bytes: [UInt8]
    var index = 0
    var nextID = 0
    var nodes: [JSONNode] = []
    init(data: Data) throws {
        guard String(data: data, encoding: .utf8) != nil else { throw JSONDocumentError.invalidJSON }
        bytes = Array(data)
        if bytes.starts(with: [239, 187, 191]) { index = 3 }
    }
    var current: UInt8? { index < bytes.count ? bytes[index] : nil }
    func whitespace() { while let c = current, [9, 10, 13, 32].contains(c) { index += 1 } }
    @discardableResult func consume(_ byte: UInt8) -> Bool {
        guard current == byte else { return false }
        index += 1; return true
    }
    func require(_ byte: UInt8) throws {
        guard consume(byte) else { throw JSONDocumentError.invalidJSON }
    }
    func parse() throws -> JSONNode {
        whitespace()
        let root = try value(key: nil, parent: nil, depth: 0)
        whitespace()
        guard index == bytes.count else { throw JSONDocumentError.invalidJSON }
        return root
    }
    func quotedString() throws -> String {
        let start = index
        try require(34)
        while let c = current {
            index += 1
            if c == 34 {
                let raw = Data(bytes[start..<index])
                guard let string = try JSONSerialization.jsonObject(with: raw, options: [.fragmentsAllowed]) as? String else { throw JSONDocumentError.invalidJSON }
                return String(decoding: try JSONSerialization.data(withJSONObject: string, options: [.fragmentsAllowed, .withoutEscapingSlashes]), as: UTF8.self)
            }
            if c == 92 { index += 1 }
            guard index <= bytes.count else { throw JSONDocumentError.invalidJSON }
        }
        throw JSONDocumentError.invalidJSON
    }
    func number() throws -> String {
        let start = index
        consume(45)
        if consume(48) {
            if let c = current, (48...57).contains(c) { throw JSONDocumentError.invalidJSON }
        } else {
            guard let c = current, (49...57).contains(c) else { throw JSONDocumentError.invalidJSON }
            digits()
        }
        if consume(46) {
            guard let c = current, (48...57).contains(c) else { throw JSONDocumentError.invalidJSON }
            digits()
        }
        if consume(101) || consume(69) {
            if !consume(43) { consume(45) }
            guard let c = current, (48...57).contains(c) else { throw JSONDocumentError.invalidJSON }
            digits()
        }
        return String(decoding: bytes[start..<index], as: UTF8.self)
    }
    func digits() { while let c = current, (48...57).contains(c) { index += 1 } }
    func literal(_ text: String) throws -> String {
        for byte in text.utf8 { try require(byte) }
        return text
    }
    func value(key: String?, parent: Int?, depth: Int) throws -> JSONNode {
        try Task.checkCancellation()
        guard depth <= 512 else { throw JSONDocumentError.invalidJSON }
        whitespace()
        let id = nextID; nextID += 1
        let kind: JSONNode.Kind, text: String
        var children: [JSONNode] = []
        switch current {
        case 123, 91:
            let object = current == 123
            let closing: UInt8 = object ? 125 : 93
            index += 1; whitespace()
            kind = object ? .object : .array
            if !consume(closing) {
                while true {
                    whitespace()
                    let childKey: String?
                    if object {
                        childKey = try quotedString(); whitespace(); try require(58)
                    } else { childKey = nil }
                    children.append(try value(key: childKey, parent: id, depth: depth + 1))
                    whitespace()
                    if consume(closing) { break }
                    try require(44)
                }
            }
            text = children.isEmpty ? (object ? "{}" : "[]") : (object ? "{" : "[")
            whitespace()
            if !children.isEmpty {
                let close = JSONNode(id: nextID, parentID: id, kind: .closing, prefix: "", value: object ? "}" : "]", comma: current == 44)
                nextID += 1; children.append(close); nodes.append(close)
            }
        case 34: kind = .string; text = try quotedString()
        case 116: kind = .boolean; text = try literal("true")
        case 102: kind = .boolean; text = try literal("false")
        case 110: kind = .null; text = try literal("null")
        default: kind = .number; text = try number()
        }
        whitespace()
        let node = JSONNode(id: id, parentID: parent, kind: kind, prefix: key.map { $0 + ": " } ?? "", value: text, comma: current == 44, children: children)
        nodes.append(node)
        return node
    }
}
