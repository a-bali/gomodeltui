import AppKit
import Testing
import GoModelCore
@testable import GoModelBar

@MainActor
struct NativeViewTests {
    init() { _ = NSApplication.shared }

    @Test func panelFitsLaptopAndDesktopSizes() throws {
        let date = Date(timeIntervalSince1970: 1_800_000_000)
        let monitor = Monitor(now: { date })
        monitor.loadDemo()
        let panel = PanelController(monitor: monitor, usage: UsageManager())
        panel.loadViewIfNeeded()
        for size in [NSSize(width: 720, height: 540), NSSize(width: 1024, height: 680)] {
            panel.view.setFrameSize(size)
            panel.refresh()
            panel.view.layoutSubtreeIfNeeded()
            let chart = try #require(panel.view.subviews.compactMap { $0 as? ChartView }.first)
            let log = try #require(panel.view.subviews.compactMap { $0 as? RequestLog }.first)
            #expect(!panel.view.hasAmbiguousLayout)
            #expect(chart.frame.height == 148)
            #expect(chart.frame.width == size.width - 32)
            #expect(log.frame.height > 180)
            #expect(log.document.rows.count == 720)
            #expect(log.scroll.frame == log.bounds)
            #expect(log.document.frame.width >= log.scroll.contentSize.width)
            #expect(log.following)
            let header = try #require(panel.view.subviews.compactMap { $0 as? NSStackView }.first)
            let checkbox = try #require(header.arrangedSubviews.compactMap { $0 as? NSButton }.first { $0.title == "MCP" })
            let picker = try #require(header.arrangedSubviews.compactMap { $0 as? NSPopUpButton }.first)
            #expect(checkbox.state == .off)
            #expect(checkbox.frame.maxX <= picker.frame.minX)

            // Offscreen artifacts review native drawing without opening desktop windows.
            do {
                let directory = ProcessInfo.processInfo.environment["GOMODELBAR_RENDER_DIR"] ?? "/private/tmp/gomodelbar-preview"
                let destination = URL(fileURLWithPath: directory)
                try FileManager.default.createDirectory(at: destination, withIntermediateDirectories: true)
                for (name, appearance) in [("light", NSAppearance.Name.aqua), ("dark", .darkAqua)] {
                    panel.view.appearance = NSAppearance(named: appearance)
                    panel.view.wantsLayer = true
                    panel.view.effectiveAppearance.performAsCurrentDrawingAppearance {
                        panel.view.layer?.backgroundColor = NSColor.windowBackgroundColor.cgColor
                    }
                    panel.view.layoutSubtreeIfNeeded()
                    let bitmap = try #require(panel.view.bitmapImageRepForCachingDisplay(in: panel.view.bounds))
                    panel.view.cacheDisplay(in: panel.view.bounds, to: bitmap)
                    let data = try #require(bitmap.representation(using: .png, properties: [:]))
                    try data.write(to: destination.appendingPathComponent("panel-\(Int(size.width))-\(name).png"))
                }
            }
        }
    }

    @Test func statusImageRendersAnHourAndDisconnectedState() throws {
        let monitor = Monitor()
        monitor.loadDemo()
        let buckets = monitor.store.buckets(now: Date(), window: 3600)
        for connected in [true, false] {
            let image = ChartDrawing.statusImage(buckets: buckets, connected: connected)
            #expect(image.size == NSSize(width: 18, height: 18))
            #expect(!image.isTemplate)
            #expect(image.tiffRepresentation != nil)
        }
    }

    @Test func iconCompressionRetainsEvenOneFailedRequest() {
        var buckets = (0..<60).map { GoModelCore.ChartBucket(start: Date().addingTimeInterval(Double($0)), success: 20) }
        buckets[29].failed = 1
        let columns = ChartDrawing.iconColumns(buckets)
        #expect(columns.count == 18)
        #expect(columns.reduce(0) { $0 + $1.failed } == 1)
        #expect(columns.reduce(0) { $0 + $1.success } == 1200)
    }

    @Test func isolatedErrorIsVisibleInRetinaIcon() throws {
        var buckets = (0..<60).map { ChartBucket(start: Date().addingTimeInterval(Double($0)), success: 1000) }
        buckets[29].failed = 1
        let image = ChartDrawing.statusImage(buckets: buckets, connected: true)
        let bitmap = try #require(NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: 36, pixelsHigh: 36,
            bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0))
        let context = try #require(NSGraphicsContext(bitmapImageRep: bitmap))
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = context
        context.cgContext.scaleBy(x: 2, y: 2)
        image.draw(in: NSRect(x: 0, y: 0, width: 18, height: 18))
        NSGraphicsContext.restoreGraphicsState()
        let redPixels = (0..<36).flatMap { y in (0..<36).compactMap { x -> NSColor? in
            guard let color = bitmap.colorAt(x: x, y: y)?.usingColorSpace(.deviceRGB),
                  color.alphaComponent > 0.9, color.redComponent > color.greenComponent + 0.2 else { return nil }
            return color
        } }
        #expect(redPixels.count >= 4)
        let destination = URL(fileURLWithPath: "/private/tmp/gomodelbar-preview/icon-retina.png")
        try FileManager.default.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true)
        try bitmap.representation(using: .png, properties: [:])?.write(to: destination)
    }

    @Test func textLogKeepsUsageInlineAndTracksNewRequestsWhilePaused() throws {
        var first = GoModelCore.RequestRecord(id: "first", timestamp: Date())
        first.inputTokens = 1234; first.outputTokens = 56; first.cacheRatio = 0.9
        let text = LogText.line(first).string
        #expect(text.contains("1,234/56/90%"))
        let log = RequestLog(frame: NSRect(x: 0, y: 0, width: 720, height: 180))
        log.layoutSubtreeIfNeeded()
        let rows = (0..<40).map { GoModelCore.RequestRecord(id: "r-\($0)", timestamp: first.timestamp.addingTimeInterval(Double($0))) }
        log.update(rows)
        log.scroll.contentView.scroll(to: NSPoint(x: 0, y: 0))
        NotificationCenter.default.post(name: NSView.boundsDidChangeNotification, object: log.scroll.contentView)
        #expect(!log.following)
        let next = rows + (40..<47).map { GoModelCore.RequestRecord(id: "r-\($0)", timestamp: first.timestamp.addingTimeInterval(Double($0))) }
        log.update(next)
        #expect(log.unseenCount == 7)
        #expect(log.scroll.contentView.bounds.minY == 0)
        log.resumeFollowing()
        #expect(log.following && log.unseenCount == 0)
    }

    @Test func mcpLogFilterDefaultsOffAndCanBeToggled() {
        let model = RequestRecord(id: "model", timestamp: Date())
        var mcp = RequestRecord(id: "mcp", timestamp: Date())
        mcp.provider = "mcp"; mcp.endpoint = "/mcp"
        let log = RequestLog(frame: NSRect(x: 0, y: 0, width: 720, height: 180))
        log.layoutSubtreeIfNeeded()
        log.update([model, mcp])
        #expect(log.document.rows.map(\.id) == ["model"])
        log.update([model, mcp], includeMCP: true)
        #expect(log.document.rows.map(\.id) == ["model", "mcp"])
        log.update([model, mcp], includeMCP: false)
        #expect(log.document.rows.map(\.id) == ["model"])
        #expect(log.unseenCount == 0)

        let history = (0..<40).map { RequestRecord(id: "old-\($0)", timestamp: model.timestamp.addingTimeInterval(Double($0))) }
        log.update(history, reset: true)
        log.scroll.contentView.scroll(to: NSPoint(x: 0, y: 0))
        NotificationCenter.default.post(name: NSView.boundsDidChangeNotification, object: log.scroll.contentView)
        mcp.timestamp = model.timestamp.addingTimeInterval(41)
        let fresh = RequestRecord(id: "new-model", timestamp: model.timestamp.addingTimeInterval(42))
        log.update(history + [mcp, fresh])
        #expect(log.unseenCount == 1)
        log.update(history + [mcp, fresh], includeMCP: true)
        #expect(log.unseenCount == 2)
        log.update(history + [mcp, fresh], includeMCP: false)
        #expect(log.unseenCount == 1)
        #expect(!log.following)
    }

    @Test func fullNamesAndLocalizedTokenCountsRemainAvailableInText() throws {
        var record = RequestRecord(id: "long", timestamp: Date())
        record.appPath = "/applications/a-very-long-application-name"
        record.sourceModel = "a-long-requested-model-name"
        record.destinationModel = "provider/a-very-long-destination-model-name"
        record.inputTokens = 1_234_567; record.outputTokens = 89_012
        let line = LogText.line(record, includePreview: false)
        #expect(line.string.contains(record.appPath))
        #expect(line.string.contains(record.sourceModel))
        #expect(line.string.contains(record.destinationModel))
        #expect(!line.string.contains("…"))
        let narrow = ("i" as NSString).size(withAttributes: [.font: LogText.font]).width
        let wide = ("W" as NSString).size(withAttributes: [.font: LogText.font]).width
        #expect(narrow == wide)
        #expect(LogText.tokenCount(1_234_567, formatted: true, locale: Locale(identifier: "en_US")) == "1,234,567")
        #expect(LogText.tokenCount(0, formatted: true) == "0")
        #expect(LogText.tokenCount(nil, formatted: true) == "—")
        let log = RequestLog(frame: NSRect(x: 0, y: 0, width: 720, height: 180))
        log.layoutSubtreeIfNeeded(); log.update([record])
        #expect(!log.scroll.hasHorizontalScroller)
        #expect(log.document.bounds.width == log.scroll.contentSize.width)
        let details = LogText.line(record, formattedTokens: true).string
        #expect(details.contains("1,234,567/89,012/—"))
        #expect(details.contains(record.destinationModel))
    }
}
