import AppKit
import GoModelCore

@MainActor
enum LogText {
    static let font = NSFont.monospacedSystemFont(ofSize: 10, weight: .regular)
    private static let timestamp: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "HH:mm:ss"
        return formatter
    }()
    private static let paragraph: NSParagraphStyle = {
        let paragraph = NSMutableParagraphStyle()
        let cell = ("0" as NSString).size(withAttributes: [.font: font, .ligature: 0, .kern: 0]).width
        // Fixed positions for the outcome, eight timestamp characters, and a
        // right-hand gap. Fallback symbol glyphs cannot move the timestamp.
        paragraph.tabStops = [NSTextTab(textAlignment: .left, location: ceil(cell * 2)),
                              NSTextTab(textAlignment: .left, location: ceil(cell * 11))]
        paragraph.lineBreakMode = .byTruncatingTail
        paragraph.tighteningFactorForTruncation = 0
        paragraph.allowsDefaultTighteningForTruncation = false
        return paragraph
    }()
    static func tokenCount(_ value: Int?, formatted: Bool, locale: Locale = .current) -> String {
        guard let value else { return "—" }
        guard formatted else { return String(value) }
        let formatter = NumberFormatter()
        formatter.locale = locale
        formatter.numberStyle = .decimal
        formatter.groupingSeparator = ","
        formatter.groupingSize = 3
        formatter.secondaryGroupingSize = 3
        formatter.maximumFractionDigits = 0
        return formatter.string(from: NSNumber(value: value)) ?? String(value)
    }
    static func color(_ key: String) -> NSColor {
        let palette: [NSColor] = [.systemBlue, .systemCyan, .systemIndigo, .systemPurple, .systemPink,
            .systemYellow, .systemOrange, .systemGreen, .systemTeal, .systemBrown]
        let hash = key.utf8.reduce(UInt32(2_166_136_261)) { ($0 ^ UInt32($1)) &* 16_777_619 }
        return palette[Int(hash % UInt32(palette.count))]
    }

    static func line(_ record: RequestRecord, formattedTokens: Bool = true, includePreview: Bool = true,
                     now: Date = Date()) -> NSAttributedString {
        let result = NSMutableAttributedString(string: "")
        let pending = !record.terminal
        let pendingAlpha = 0.62 + 0.18 * sin(now.timeIntervalSinceReferenceDate * (2 * Double.pi / 1.2))
        func add(_ text: String, _ color: NSColor = .labelColor) {
            let foreground = pending ? color.withAlphaComponent(pendingAlpha) : color
            result.append(NSAttributedString(string: text, attributes: [.font: font, .foregroundColor: foreground, .ligature: 0, .kern: 0]))
        }
        let outcome: NSColor = !record.terminal ? .secondaryLabelColor : !record.success ? .systemRed : record.failover ? .systemYellow : .systemGreen
        add(record.terminal ? (record.success ? "✓\t" : "✗\t") : "·\t", outcome)
        add(timestamp.string(from: record.timestamp) + "\t", .secondaryLabelColor)
        if !record.appPath.isEmpty { add(record.appPath) }
        if !record.sessionID.isEmpty {
            add(" (\(record.sessionID.suffix(3)))", color("session:" + record.sessionID))
        }
        if !record.appPath.isEmpty || !record.sessionID.isEmpty { add(" ") }
        if !record.sourceModel.isEmpty { add(record.sourceModel, color("model:" + record.sourceModel)) }
        if !record.destinationModel.isEmpty {
            add("→", .tertiaryLabelColor)
            add(record.destinationModel, color("model:" + record.destinationModel))
        }
        if !record.isMCP {
            add(" ")
            add(tokenCount(record.inputTokens, formatted: formattedTokens))
            add("/", .tertiaryLabelColor)
            add(tokenCount(record.outputTokens, formatted: formattedTokens))
            add("/", .tertiaryLabelColor)
            add(record.cacheRatio.map { String(format: "%.0f%%", $0 * 100) } ?? "—")
        }
        add(" ")
        let duration = record.terminal ? record.duration : max(0, now.timeIntervalSince(record.startedAt))
        if let duration {
            add(duration < 1 ? String(format: "%.0fms", duration * 1000) : String(format: "%.1fs", duration),
                duration < 5 ? .systemGreen : duration <= 30 ? .systemYellow : .systemRed)
        } else { add("—", .secondaryLabelColor) }
        let preview = record.error.isEmpty ? record.preview : record.error
        if includePreview, !preview.isEmpty { add("  " + preview, record.error.isEmpty ? .secondaryLabelColor : .systemRed) }
        result.addAttribute(.paragraphStyle, value: paragraph, range: NSRange(location: 0, length: result.length))
        return result
    }

}

@MainActor
final class LogDocument: NSView {
    static let rowHeight: CGFloat = 18
    var rows: [RequestRecord] = [] {
        didSet { window?.invalidateCursorRects(for: self); refreshHover() }
    }
    var openRequest: ((RequestRecord) -> Void)?
    private(set) var hoveredRow: Int?
    private var hoverTracking: NSTrackingArea?
    override var isFlipped: Bool { true }
    override init(frame: NSRect) {
        super.init(frame: frame)
        setAccessibilityElement(true)
        setAccessibilityRole(.textArea)
        setAccessibilityLabel("Recent GoModel requests")
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    override func draw(_ dirtyRect: NSRect) {
        let first = max(0, Int(dirtyRect.minY / Self.rowHeight))
        let last = min(rows.count, Int(ceil(dirtyRect.maxY / Self.rowHeight)))
        guard first < last else { return }
        for index in first..<last {
            if index == hoveredRow {
                NSColor.labelColor.withAlphaComponent(0.06).setFill()
                NSRect(x: 0, y: CGFloat(index) * Self.rowHeight, width: bounds.width, height: Self.rowHeight).fill()
            }
            drawPathPill(for: rows[index], rowY: CGFloat(index) * Self.rowHeight)
            let context = NSStringDrawingContext()
            context.minimumScaleFactor = 1
            LogText.line(rows[index]).draw(with: NSRect(x: 2, y: CGFloat(index) * Self.rowHeight + 2,
                width: max(0, bounds.width - 4), height: Self.rowHeight),
                options: [.usesLineFragmentOrigin, .truncatesLastVisibleLine], context: context)
        }
    }

    private func drawPathPill(for record: RequestRecord, rowY: CGFloat) {
        guard !record.appPath.isEmpty else { return }
        let attributes: [NSAttributedString.Key: Any] = [.font: LogText.font, .ligature: 0, .kern: 0]
        let cellWidth = ("0" as NSString).size(withAttributes: attributes).width
        let textX = 2 + ceil(cellWidth * 11)
        let x = max(0, textX - 3)
        let measuredWidth = (record.appPath as NSString).size(withAttributes: attributes).width + 6
        let width = min(measuredWidth, max(0, bounds.width - x - 2))
        guard width > 0 else { return }
        let rect = NSRect(x: x, y: rowY + 3, width: width, height: 12)
        let tint = LogText.color(record.appPath).withAlphaComponent(record.terminal ? 0.28 : 0.18)
        tint.setFill()
        NSBezierPath(roundedRect: rect, xRadius: 5, yRadius: 5).fill()
    }

    override func viewDidChangeEffectiveAppearance() {
        super.viewDidChangeEffectiveAppearance()
        needsDisplay = true
    }

    override func resetCursorRects() {
        super.resetCursorRects()
        let occupied = NSRect(x: 0, y: 0, width: bounds.width,
            height: min(bounds.height, CGFloat(rows.count) * Self.rowHeight)).intersection(visibleRect)
        if !occupied.isEmpty { addCursorRect(occupied, cursor: .pointingHand) }
    }

    override func updateTrackingAreas() {
        if let hoverTracking { removeTrackingArea(hoverTracking) }
        let tracking = NSTrackingArea(rect: .zero,
            options: [.inVisibleRect, .mouseMoved, .mouseEnteredAndExited, .activeAlways], owner: self)
        addTrackingArea(tracking); hoverTracking = tracking
        super.updateTrackingAreas()
        refreshHover()
    }

    override func mouseEntered(with event: NSEvent) { updateHover(at: convert(event.locationInWindow, from: nil)) }
    override func mouseMoved(with event: NSEvent) { updateHover(at: convert(event.locationInWindow, from: nil)) }
    override func mouseExited(with event: NSEvent) { setHoveredRow(nil) }

    func refreshHover() {
        guard let window, window.isVisible, !isHiddenOrHasHiddenAncestor else { setHoveredRow(nil); return }
        updateHover(at: convert(window.mouseLocationOutsideOfEventStream, from: nil))
    }

    private func updateHover(at point: NSPoint) {
        guard visibleRect.contains(point) else { setHoveredRow(nil); return }
        let index = Int(floor(point.y / Self.rowHeight))
        setHoveredRow(rows.indices.contains(index) ? index : nil)
    }

    private func setHoveredRow(_ index: Int?) {
        guard index != hoveredRow else { return }
        let previous = hoveredRow
        hoveredRow = index
        for row in [previous, index].compactMap({ $0 }) {
            setNeedsDisplay(NSRect(x: 0, y: CGFloat(row) * Self.rowHeight, width: bounds.width, height: Self.rowHeight))
        }
    }
    override func mouseDown(with event: NSEvent) {
        let index = Int(convert(event.locationInWindow, from: nil).y / Self.rowHeight)
        if rows.indices.contains(index) { openRequest?(rows[index]) }
    }
}

@MainActor
final class HistoryScrollView: NSScrollView {
    var scrollEarlier: (() -> Void)?
    override func scrollWheel(with event: NSEvent) {
        if event.scrollingDeltaY > 0, contentView.bounds.minY <= LogDocument.rowHeight * 3 { scrollEarlier?() }
        super.scrollWheel(with: event)
    }
}

@MainActor
final class RequestLog: NSView {
    let scroll = HistoryScrollView()
    let document = LogDocument()
    private(set) var following = true
    private(set) var unseenCount = 0
    var followChanged: (() -> Void)?
    var loadEarlier: (() -> Void)?
    var visibleChanged: (([RequestRecord]) -> Void)?
    var canLoadEarlier = true {
        didSet { if canLoadEarlier && !oldValue { requestedEarlier = false } }
    }
    private var requestedEarlier = false
    private var lastEarlierRequest = Date.distantPast
    private var updating = false
    private var observer: NSObjectProtocol?
    private var latencyTimer: Timer?
    private var knownIDs = Set<String>()
    private var unseenIDs = Set<String>()
    private var newestTimestamp: Date?

    override init(frame: NSRect) {
        super.init(frame: frame)
        scroll.documentView = document
        scroll.hasVerticalScroller = true
        scroll.hasHorizontalScroller = false
        scroll.horizontalScrollElasticity = .none
        scroll.autohidesScrollers = true
        scroll.drawsBackground = false
        scroll.scrollEarlier = { [weak self] in self?.requestEarlier(userInitiated: true) }
        scroll.contentView.postsBoundsChangedNotifications = true
        observer = NotificationCenter.default.addObserver(forName: NSView.boundsDidChangeNotification,
            object: scroll.contentView, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.didScroll() }
            }
        addSubview(scroll)
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    override func layout() {
        super.layout()
        updating = true
        scroll.frame = bounds
        sizeDocument()
        if following { scrollToEnd() }
        updating = false
        document.window?.invalidateCursorRects(for: document)
        document.refreshHover()
        notifyVisibleRows()
    }

    private func sizeDocument() {
        document.setFrameSize(NSSize(width: scroll.contentSize.width,
            height: max(scroll.contentSize.height, CGFloat(document.rows.count) * LogDocument.rowHeight)))
        if scroll.contentView.bounds.minX != 0 {
            scroll.contentView.scroll(to: NSPoint(x: 0, y: scroll.contentView.bounds.minY))
            scroll.reflectScrolledClipView(scroll.contentView)
        }
        document.needsDisplay = true
    }

    private func scrollToEnd() {
        scroll.contentView.scroll(to: NSPoint(x: scroll.contentView.bounds.minX, y: max(0, document.bounds.height - scroll.contentSize.height)))
        scroll.reflectScrolledClipView(scroll.contentView)
    }

    private func didScroll() {
        guard !updating else { return }
        document.window?.invalidateCursorRects(for: document)
        document.refreshHover()
        if scroll.contentView.bounds.minY > LogDocument.rowHeight * 5 { requestedEarlier = false }
        guard !document.rows.isEmpty else { return }
        let nearBottom = scroll.contentView.bounds.maxY >= document.bounds.height - 4
        if following != nearBottom {
            following = nearBottom
            if following { unseenCount = 0; unseenIDs.removeAll() }
            followChanged?()
        }
        if !following, scroll.contentView.bounds.minY <= LogDocument.rowHeight * 3 { requestEarlier() }
        let first = max(0, Int(scroll.contentView.bounds.minY / LogDocument.rowHeight))
        let last = min(document.rows.count, first + Int(bounds.height / LogDocument.rowHeight) + 1)
        if first < last {
            document.setAccessibilityValue(document.rows[first..<last].map { LogText.line($0, formattedTokens: true).string }.joined(separator: "\n"))
        }
        notifyVisibleRows()
    }

    private func notifyVisibleRows() {
        let first = max(0, Int(scroll.contentView.bounds.minY / LogDocument.rowHeight))
        let last = min(document.rows.count, Int(ceil(scroll.contentView.bounds.maxY / LogDocument.rowHeight)))
        visibleChanged?(first < last ? Array(document.rows[first..<last]) : [])
    }

    func requestEarlier(userInitiated: Bool = false) {
        guard !updating, canLoadEarlier, !requestedEarlier || userInitiated,
              Date().timeIntervalSince(lastEarlierRequest) >= 0.3 else { return }
        requestedEarlier = true; lastEarlierRequest = Date()
        following = false
        followChanged?()
        // Defer until scroll notifications finish so prepending preserves the visible row.
        Task { @MainActor [weak self] in
            guard let self, !self.following, self.canLoadEarlier else { return }
            self.loadEarlier?()
        }
    }

    func update(_ records: [RequestRecord], reset: Bool = false, includeMCP: Bool = false) {
        updating = true
        let next = (includeMCP ? records : records.filter { !$0.isMCP }).flatMap { $0.logRows() }
        let old = document.rows
        let first = max(0, Int(scroll.contentView.bounds.minY / LogDocument.rowHeight))
        let anchor = old.indices.contains(first) ? old[first].id : nil
        let offset = scroll.contentView.bounds.minY - CGFloat(first) * LogDocument.rowHeight
        if reset { following = true; requestedEarlier = false }
        if following { unseenIDs.removeAll() }
        else {
            unseenIDs.formUnion(records.filter {
                !knownIDs.contains($0.id) && $0.timestamp >= (newestTimestamp ?? .distantPast)
            }.map(\.id))
        }
        knownIDs = Set(records.map(\.id))
        newestTimestamp = records.map(\.timestamp).max()
        unseenIDs.formIntersection(knownIDs)
        document.rows = next
        updateLatencyTimer()
        let active = Set(next.map(\.logicalID))
        unseenCount = unseenIDs.intersection(active).count
        sizeDocument()
        if following { scrollToEnd() }
        else if let anchor, let index = next.firstIndex(where: { $0.id == anchor }) {
            scroll.contentView.scroll(to: NSPoint(x: scroll.contentView.bounds.minX, y: CGFloat(index) * LogDocument.rowHeight + offset))
            scroll.reflectScrolledClipView(scroll.contentView)
        }
        updating = false
        followChanged?()
        document.refreshHover()
        notifyVisibleRows()
    }

    func resumeFollowing() {
        updating = true
        following = true; unseenCount = 0; unseenIDs.removeAll()
        requestedEarlier = false
        scrollToEnd()
        updating = false
        followChanged?()
        document.refreshHover()
    }

    func refreshLiveLatency() {
        document.setNeedsDisplay(document.visibleRect)
        updateLatencyTimer()
    }

    private func updateLatencyTimer() {
        guard document.rows.contains(where: { !$0.terminal }) else {
            latencyTimer?.invalidate()
            latencyTimer = nil
            return
        }
        guard latencyTimer == nil else { return }
        latencyTimer = Timer.scheduledTimer(withTimeInterval: 0.1, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self else { return }
                guard self.window?.isVisible == true,
                      self.document.rows.contains(where: { !$0.terminal }) else {
                    self.latencyTimer?.invalidate()
                    self.latencyTimer = nil
                    return
                }
                self.document.setNeedsDisplay(self.document.visibleRect)
            }
        }
        if let latencyTimer { RunLoop.main.add(latencyTimer, forMode: .common) }
    }
}
