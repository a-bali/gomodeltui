import Foundation

/// Older chart activity retains only identity, time, outcome and traffic kind.
public struct ActivityStore: Sendable {
    private struct Activity: Sendable {
        let timestamp: Date
        let success: Bool
        let live: Bool
        let isMCP: Bool
    }
    private var requests: [String: Activity] = [:]
    public init() {}
    public mutating func apply(_ record: RequestRecord) {
        guard record.terminal else { return }
        if requests[record.id]?.live == true && !record.liveTerminal { return }
        requests[record.id] = Activity(timestamp: record.timestamp, success: record.success, live: record.liveTerminal, isMCP: record.isMCP)
    }
    public mutating func prune(before: Date) { requests = requests.filter { $0.value.timestamp >= before } }
    /// Includes the current partial bucket on a fixed clock grid. The axis moves
    /// only when that bucket finishes, never on each refresh of the live clock.
    public static func alignedEnd(now: Date, window: TimeInterval, count: Int = 60) -> Date {
        guard count > 0, window > 0 else { return now }
        let interval = window / Double(count)
        return Date(timeIntervalSince1970: (floor(now.timeIntervalSince1970 / interval) + 1) * interval)
    }
    /// With `through`, `now` is the exclusive display edge and `through` caps
    /// observed activity, allowing the current bucket to extend beyond the clock.
    public func buckets(now: Date, window: TimeInterval, count: Int = 60, through: Date? = nil, includeMCP: Bool = false) -> [ChartBucket] {
        guard count > 0, window > 0 else { return [] }
        let start = now.addingTimeInterval(-window), interval = window / Double(count)
        var buckets = (0..<count).map { ChartBucket(start: start.addingTimeInterval(Double($0) * interval)) }
        let cutoff = min(now, through ?? now)
        for record in requests.values where record.timestamp >= start && record.timestamp <= cutoff && (record.timestamp < now || through == nil) {
            guard includeMCP || !record.isMCP else { continue }
            let index = min(count - 1, max(0, Int(record.timestamp.timeIntervalSince(start) / interval)))
            if record.success { buckets[index].success += 1 } else { buckets[index].failed += 1 }
        }
        return buckets
    }
}
