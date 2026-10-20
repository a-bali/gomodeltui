import AppKit
import Foundation
import Testing
import GoModelCore
@testable import GoModelBar

private actor HistoryCalls {
    var ranges: [RequestTimeRange] = []
    func record(_ range: RequestTimeRange) { ranges.append(range) }
}

@MainActor
struct HistoryTests {
    let date = Date(timeIntervalSince1970: 1_800_000_000)
    func range(_ start: Double, _ end: Double) -> RequestTimeRange {
        RequestTimeRange(start: date.addingTimeInterval(start), end: date.addingTimeInterval(end))
    }
    func fixture(_ offsets: [Double]) throws -> [(Date, AuditEvent)] {
        try offsets.map { offset in
            let timestamp = date.addingTimeInterval(offset)
            return (timestamp, try AuditEvent.history(["request_id": String(offset), "timestamp": ISO8601DateFormatter().string(from: timestamp), "status_code": 200, "input_tokens": 1234]))
        }
    }

    @Test func hourIsDefaultAndOlderDetailsAreLoadedOnlyOnDemand() async throws {
        let calls = HistoryCalls(), sample = try fixture([-10, -1800, -5400, -9900])
        let date = self.date
        let monitor = Monitor(now: { date }, historyLoader: { range, receive in
            await calls.record(range)
            await receive(sample.filter { range.contains($0.0) }.map(\.1))
        })
        monitor.ensureHistory()
        await monitor.historyTask?.value
        #expect(await calls.ranges == [range(-3600, 0)])
        #expect(monitor.store.records.count == 2 && monitor.logRecords().count == 2)

        monitor.selectWindow(10800)
        await monitor.historyTask?.value
        #expect(await calls.ranges == [range(-3600, 0), range(-10800, -3600)])
        #expect(monitor.logRecords().count == 4)

        monitor.selectRange(range(-10800, -7200), chartEnd: date)
        await monitor.historyTask?.value
        #expect(monitor.logRecords().map(\.id) == ["-9900.0"])
        #expect(monitor.store.records.count == 3) // selected details plus the live hour
        #expect(await calls.ranges.count == 2) // already loaded, including gaps
        #expect(monitor.activity.buckets(now: date, window: 10800).reduce(0) { $0 + $1.total } == 4)

        monitor.resumeLive()
        await monitor.historyTask?.value
        #expect(monitor.logWindow.isLive && monitor.store.records.count == 2)
        monitor.selectRange(range(-7200, -3600), chartEnd: date)
        await monitor.historyTask?.value
        #expect(await calls.ranges.last == range(-7200, -3600))
        #expect(monitor.logRecords().map(\.id) == ["-5400.0"])
        #expect(monitor.logRecords().first?.inputTokens == 1234)
    }

    @Test func scrollBackfillsOneHourIncludingEmptyPeriods() async throws {
        let calls = HistoryCalls(), sample = try fixture([-10, -9900]), date = self.date
        let monitor = Monitor(now: { date }, historyLoader: { range, receive in
            await calls.record(range)
            await receive(sample.filter { range.contains($0.0) }.map(\.1))
        })
        monitor.ensureHistory(); await monitor.historyTask?.value
        monitor.loadEarlier(); await monitor.historyTask?.value
        #expect(await calls.ranges.last == range(-7200, -3600))
        #expect(monitor.logRecords().count == 1)
        monitor.loadEarlier(); await monitor.historyTask?.value
        #expect(await calls.ranges.last == range(-10800, -7200))
        #expect(monitor.logRecords().count == 2)
        monitor.selectRange(range(-9000, -8500), chartEnd: date)
        await monitor.historyTask?.value
        #expect(monitor.logRecords().isEmpty)
        let count = await calls.ranges.count
        monitor.loadEarlier(); await monitor.historyTask?.value
        #expect(await calls.ranges.count == count)
    }

    @Test func chartGestureKeepsItsAxisStableDuringIncomingUpdates() {
        _ = NSApplication.shared
        let chart = ChartView()
        chart.buckets = (0..<60).map { ChartBucket(start: date.addingTimeInterval(-3600 + Double($0) * 60)) }
        var result: RequestTimeRange?, end: Date?
        chart.selected = { result = $0; end = $1 }
        chart.select(startFraction: 0.201, endFraction: 0.201, ended: false)
        chart.buckets = (0..<60).map { ChartBucket(start: date.addingTimeInterval(-3500 + Double($0) * 60)) }
        chart.select(startFraction: 0.201, endFraction: 0.501, ended: true)
        #expect(result == range(-2880, -1740))
        #expect(end == date && chart.selection == result)
    }

    @Test func prependingOlderRowsPreservesAnchorAndDoesNotCountAsNew() {
        _ = NSApplication.shared
        let log = RequestLog(frame: NSRect(x: 0, y: 0, width: 720, height: 180))
        log.canLoadEarlier = false
        log.layoutSubtreeIfNeeded()
        let rows = (0..<40).map { RequestRecord(id: "r-\($0)", timestamp: date.addingTimeInterval(Double($0))) }
        log.update(rows)
        log.scroll.contentView.scroll(to: NSPoint(x: 0, y: 36))
        NotificationCenter.default.post(name: NSView.boundsDidChangeNotification, object: log.scroll.contentView)
        #expect(!log.following)
        let older = (-20..<0).map { RequestRecord(id: "r-\($0)", timestamp: date.addingTimeInterval(Double($0))) }
        log.update(older + rows)
        #expect(log.scroll.contentView.bounds.minY == 396)
        #expect(log.unseenCount == 0)
    }

    @Test func panelSelectionFiltersRowsAndLiveButtonRestoresHour() async throws {
        _ = NSApplication.shared
        let date = self.date
        let monitor = Monitor(now: { date }); monitor.loadDemo()
        let panel = PanelController(monitor: monitor, usage: UsageManager())
        panel.loadViewIfNeeded()
        panel.view.setFrameSize(NSSize(width: 1024, height: 680))
        monitor.changed = { [weak panel] in panel?.refresh() }
        panel.refresh()
        let log = try #require(panel.view.subviews.compactMap { $0 as? RequestLog }.first)
        let chart = try #require(panel.view.subviews.compactMap { $0 as? ChartView }.first)
        chart.select(startFraction: 0.201, endFraction: 0.501, ended: true)
        await monitor.historyTask?.value
        let selected = try #require(monitor.logWindow.selection)
        #expect(!log.document.rows.isEmpty)
        #expect(log.document.rows.allSatisfy { selected.contains($0.timestamp) })
        let live = try #require(panel.view.subviews.compactMap { $0 as? NSButton }.first { $0.title == "Live" })
        #expect(!live.isHidden)
        panel.view.appearance = NSAppearance(named: .darkAqua)
        panel.view.wantsLayer = true
        panel.view.effectiveAppearance.performAsCurrentDrawingAppearance {
            panel.view.layer?.backgroundColor = NSColor.windowBackgroundColor.cgColor
        }
        panel.view.layoutSubtreeIfNeeded()
        let bitmap = try #require(panel.view.bitmapImageRepForCachingDisplay(in: panel.view.bounds))
        panel.view.cacheDisplay(in: panel.view.bounds, to: bitmap)
        let directory = URL(fileURLWithPath: ProcessInfo.processInfo.environment["GOMODELBAR_RENDER_DIR"] ?? "/private/tmp/gomodelbar-preview")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try bitmap.representation(using: .png, properties: [:])?.write(to: directory.appendingPathComponent("panel-selected.png"))
        live.performClick(nil)
        await monitor.historyTask?.value
        #expect(monitor.logWindow.isLive && monitor.chartEnd == nil)
        #expect(log.following && log.document.rows.count == 720)
        #expect(live.isHidden && chart.selection == nil)
    }
}
