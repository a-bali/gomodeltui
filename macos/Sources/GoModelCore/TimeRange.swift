import Foundation

public struct RequestTimeRange: Equatable, Sendable {
    public let start: Date
    public let end: Date
    public var duration: TimeInterval { end.timeIntervalSince(start) }
    public init(start: Date, end: Date) {
        self.start = min(start, end); self.end = max(start, end)
    }
    public func contains(_ date: Date) -> Bool { date >= start && date < end }
    public func intersection(_ other: Self) -> Self? {
        let lower = max(start, other.start), upper = min(end, other.end)
        return lower < upper ? Self(start: lower, end: upper) : nil
    }

    /// A click selects one whole bucket; a drag selects all buckets it crosses.
    public static func chartSelection(startFraction: Double, endFraction: Double,
                                      chart: Self, buckets: Int) -> Self? {
        guard buckets > 0, chart.duration > 0, startFraction.isFinite, endFraction.isFinite else { return nil }
        func index(_ value: Double) -> Int { min(buckets - 1, max(0, Int(floor(min(1, max(0, value)) * Double(buckets))))) }
        let a = index(startFraction), b = index(endFraction)
        let width = chart.duration / Double(buckets)
        return Self(start: chart.start.addingTimeInterval(Double(min(a, b)) * width),
                    end: chart.start.addingTimeInterval(Double(max(a, b) + 1) * width))
    }
}

/// Tracks exact coverage, including empty intervals, rather than assuming all
/// history before the earliest loaded request is available.
public struct HistoryCoverage: Sendable {
    public private(set) var ranges: [RequestTimeRange] = []
    public init() {}
    public mutating func insert(_ range: RequestTimeRange) {
        guard range.duration > 0 else { return }
        var merged: [RequestTimeRange] = []
        for item in (ranges + [range]).sorted(by: { $0.start < $1.start }) {
            if let last = merged.last, item.start <= last.end {
                merged[merged.count - 1] = RequestTimeRange(start: last.start, end: max(last.end, item.end))
            } else { merged.append(item) }
        }
        ranges = merged
    }
    public func missing(in range: RequestTimeRange) -> [RequestTimeRange] {
        var result: [RequestTimeRange] = []
        var cursor = range.start
        for covered in ranges where covered.end > cursor && covered.start < range.end {
            if covered.start > cursor { result.append(RequestTimeRange(start: cursor, end: min(covered.start, range.end))) }
            cursor = max(cursor, covered.end)
        }
        if cursor < range.end { result.append(RequestTimeRange(start: cursor, end: range.end)) }
        return result
    }
    public mutating func retain(in retained: [RequestTimeRange]) {
        let intersections = ranges.flatMap { range in retained.compactMap { range.intersection($0) } }
        ranges = []
        for range in intersections { insert(range) }
    }
}

public struct LogTimeWindow: Equatable, Sendable {
    public private(set) var duration: TimeInterval = 3600
    public private(set) var selection: RequestTimeRange?
    public private(set) var browsingStart: Date?
    public init() {}
    public var isLive: Bool { selection == nil && browsingStart == nil && duration == 3600 }
    public func range(now: Date) -> RequestTimeRange {
        selection ?? RequestTimeRange(start: browsingStart ?? now.addingTimeInterval(-duration), end: now)
    }
    public mutating func select(_ range: RequestTimeRange) { selection = range; browsingStart = nil }
    public mutating func selectWindow(_ seconds: TimeInterval) {
        duration = seconds; selection = nil; browsingStart = nil
    }
    public mutating func pause(now: Date) {
        if selection == nil && browsingStart == nil { browsingStart = now.addingTimeInterval(-duration) }
    }
    public mutating func resumeFollowing() { browsingStart = nil }
    @discardableResult
    public mutating func loadEarlier(now: Date, earliest: Date) -> RequestTimeRange? {
        guard selection == nil else { return nil }
        let previous = range(now: now).start
        let start = max(earliest, previous.addingTimeInterval(-3600))
        guard start < previous else { return nil }
        browsingStart = start
        return RequestTimeRange(start: start, end: previous)
    }
    public mutating func resumeLive() { self = Self() }
}
