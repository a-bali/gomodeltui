import Foundation
import Testing
@testable import GoModelCore

struct TimeRangeTests {
    let origin = Date(timeIntervalSince1970: 1_800_000_000)
    func range(_ start: Double, _ end: Double) -> RequestTimeRange {
        RequestTimeRange(start: origin.addingTimeInterval(start), end: origin.addingTimeInterval(end))
    }

    @Test func clicksAndDragsSelectCompleteBuckets() {
        let chart = range(0, 3600)
        #expect(RequestTimeRange.chartSelection(startFraction: 0.501, endFraction: 0.501, chart: chart, buckets: 60) == range(1800, 1860))
        #expect(RequestTimeRange.chartSelection(startFraction: 0.501, endFraction: 0.201, chart: chart, buckets: 60) == range(720, 1860))
        #expect(RequestTimeRange.chartSelection(startFraction: 1, endFraction: 1, chart: chart, buckets: 60) == range(3540, 3600))
        #expect(RequestTimeRange.chartSelection(startFraction: -1, endFraction: 2, chart: chart, buckets: 60) == chart)
        #expect(RequestTimeRange.chartSelection(startFraction: .nan, endFraction: 0, chart: chart, buckets: 60) == nil)
        #expect(RequestTimeRange.chartSelection(startFraction: 0, endFraction: 0, chart: chart, buckets: 0) == nil)
        #expect(!chart.contains(chart.end))
    }

    @Test func coverageTracksEmptyIntervalsAndDisjointSelections() {
        var coverage = HistoryCoverage()
        coverage.insert(range(10, 20)); coverage.insert(range(30, 40))
        #expect(coverage.missing(in: range(0, 50)) == [range(0, 10), range(20, 30), range(40, 50)])
        coverage.insert(range(20, 30))
        #expect(coverage.ranges == [range(10, 40)])
        coverage.retain(in: [range(15, 25), range(35, 50)])
        #expect(coverage.ranges == [range(15, 25), range(35, 40)])
        #expect(coverage.missing(in: range(15, 25)).isEmpty)
    }

    @Test func liveHourLazyExpansionSelectionAndReset() {
        var window = LogTimeWindow()
        #expect(window.isLive)
        #expect(window.range(now: origin) == range(-3600, 0))
        #expect(window.range(now: origin.addingTimeInterval(60)) == range(-3540, 60))
        window.pause(now: origin)
        #expect(window.range(now: origin.addingTimeInterval(60)) == range(-3600, 60))
        #expect(window.loadEarlier(now: origin, earliest: origin.addingTimeInterval(-8000)) == range(-7200, -3600))
        #expect(window.loadEarlier(now: origin, earliest: origin.addingTimeInterval(-8000)) == range(-8000, -7200))
        #expect(window.loadEarlier(now: origin, earliest: origin.addingTimeInterval(-8000)) == nil)
        window.resumeFollowing()
        #expect(window.isLive && window.range(now: origin) == range(-3600, 0))
        window.select(range(-7000, -6000))
        #expect(window.range(now: origin.addingTimeInterval(60)) == range(-7000, -6000))
        #expect(window.loadEarlier(now: origin, earliest: origin.addingTimeInterval(-8000)) == nil)
        window.selectWindow(900)
        #expect(window.selection == nil && window.browsingStart == nil)
        #expect(window.range(now: origin) == range(-900, 0))
        window.resumeLive()
        #expect(window.isLive && window.range(now: origin) == range(-3600, 0))
    }

    @Test func activitySurvivesEvictionOfRequestDetailsAndSeparatesMCP() {
        var activity = ActivityStore()
        var store = RequestStore()
        for (id, offset, provider) in [("old", -7200.0, "model"), ("recent", -10.0, "model"), ("mcp", -10.0, "mcp")] {
            let event = try! AuditEvent.history(["request_id": id, "timestamp": ISO8601DateFormatter().string(from: origin.addingTimeInterval(offset)), "status_code": 200, "provider_name": provider])
            try! store.apply(event)
            activity.apply(store.records[id]!)
            activity.apply(store.records[id]!)
        }
        store.retain(in: [range(-3600, 1)])
        #expect(store.records.count == 2)
        #expect(activity.buckets(now: origin, window: 10800).reduce(0) { $0 + $1.total } == 2)
        activity.prune(before: origin.addingTimeInterval(-3600))
        #expect(activity.buckets(now: origin, window: 10800).reduce(0) { $0 + $1.total } == 1)
    }
}
