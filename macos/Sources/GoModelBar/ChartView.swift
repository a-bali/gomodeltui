import AppKit
import SwiftUI
import Charts
import GoModelCore

@MainActor
enum ChartDrawing {
    static func scale(_ maximum: Int) -> Double {
        guard maximum > 1 else { return 1 }
        let power = pow(10, floor(log10(Double(maximum))))
        return [1.0, 2, 5, 10].map { $0 * power }.first { $0 >= Double(maximum) } ?? Double(maximum)
    }

    static func iconColumns(_ buckets: [ChartBucket], count: Int = 18) -> [ChartBucket] {
        guard !buckets.isEmpty, count > 0 else { return [] }
        return (0..<count).map { column in
            let first = column * buckets.count / count
            let last = max(first + 1, (column + 1) * buckets.count / count)
            let source = buckets[first..<min(last, buckets.count)]
            return ChartBucket(start: buckets[first].start, success: source.reduce(0) { $0 + $1.success }, failed: source.reduce(0) { $0 + $1.failed })
        }
    }

    static func statusImage(buckets: [ChartBucket], connected: Bool) -> NSImage {
        let columns = iconColumns(buckets)
        let maximum = scale(columns.map(\.total).max() ?? 1)
        let image = NSImage(size: NSSize(width: 18, height: 18), flipped: false) { _ in
            let pixelScale = max(1, abs(NSGraphicsContext.current?.cgContext.ctm.a ?? 1))
            let strip = 2 / pixelScale
            let plot = NSRect(x: 0, y: strip + 1, width: 18, height: 15 - strip)
            if !columns.isEmpty {
                let width = plot.width / CGFloat(columns.count)
                for (index, bucket) in columns.enumerated() {
                    let success = CGFloat(Double(bucket.success) / maximum) * plot.height
                    let failed = CGFloat(Double(bucket.failed) / maximum) * plot.height
                    let x = CGFloat(index) * width
                    NSColor.systemGreen.withAlphaComponent(connected ? 0.9 : 0.35).setFill()
                    NSRect(x: x, y: plot.minY, width: width, height: success).fill()
                    NSColor.systemRed.withAlphaComponent(connected ? 0.95 : 0.4).setFill()
                    NSRect(x: x, y: plot.minY + success, width: width, height: failed).fill()
                }
                for (index, bucket) in columns.enumerated() {
                    let color: NSColor = bucket.failed > 0 ? .systemRed : bucket.total > 0 ? .systemGreen : .tertiaryLabelColor
                    color.setFill()
                    NSRect(x: CGFloat(index) * width, y: 0, width: width, height: strip).fill()
                }
            }
            if !connected {
                NSColor.secondaryLabelColor.setFill()
                NSBezierPath(ovalIn: NSRect(x: 14, y: 13, width: 4, height: 4)).fill()
            }
            return true
        }
        image.isTemplate = false
        return image
    }
}

@MainActor
private struct ActivityChart: View {
    let buckets: [ChartBucket]
    let timeWindow: TimeInterval
    let hovered: Int?
    let hover: (Int?, String?) -> Void
    let selection: RequestTimeRange?
    let drag: (Double, Double, Bool) -> Void
    private var start: Date { buckets.first?.start ?? Date().addingTimeInterval(-timeWindow) }
    private var end: Date { start.addingTimeInterval(timeWindow) }
    private var interval: TimeInterval { timeWindow / Double(max(1, buckets.count)) }

    var body: some View {
        Chart {
            ForEach(buckets, id: \.start) { bucket in
                // Explicit edges keep bar widths and stacking tied to the actual
                // bucket interval rather than Charts' inferred Date spacing.
                let left = bucket.start.addingTimeInterval(interval * 0.08)
                let right = bucket.start.addingTimeInterval(interval * 0.92)
                RectangleMark(xStart: .value("Bucket start", left), xEnd: .value("Bucket end", right),
                              yStart: .value("Requests", 0), yEnd: .value("Requests", bucket.success))
                    .foregroundStyle(by: .value("Outcome", "Successful"))
                RectangleMark(xStart: .value("Bucket start", left), xEnd: .value("Bucket end", right),
                              yStart: .value("Requests", bucket.success), yEnd: .value("Requests", bucket.total))
                    .foregroundStyle(by: .value("Outcome", "Failed"))
            }
            if let selection {
                RectangleMark(xStart: .value("Selected start", selection.start), xEnd: .value("Selected end", selection.end))
                    .foregroundStyle(Color.accentColor.opacity(0.16))
            }
            if let hovered, buckets.indices.contains(hovered) {
                RuleMark(x: .value("Time", buckets[hovered].start.addingTimeInterval(interval / 2)))
                    .foregroundStyle(Color.secondary.opacity(0.45))
                    .lineStyle(StrokeStyle(lineWidth: 1, dash: [3, 3]))
            }
        }
        .chartForegroundStyleScale(domain: ["Successful", "Failed"], range: [Color.green.opacity(0.8), Color.red.opacity(0.9)])
        .chartLegend(.hidden)
        .chartXScale(domain: start...end)
        .chartYScale(domain: 0...ChartDrawing.scale(buckets.map(\.total).max() ?? 1))
        .chartXAxis {
            AxisMarks(values: [start, start.addingTimeInterval(timeWindow / 2), end]) { value in
                AxisValueLabel(anchor: value.index == 0 ? .topLeading : value.index == 2 ? .topTrailing : .top) {
                    if let date = value.as(Date.self) { Text(date, format: .dateTime.hour().minute()) }
                }.font(.system(size: 10))
            }
        }
        .chartYAxis {
            AxisMarks(position: .trailing, values: .automatic(desiredCount: 3)) {
                AxisGridLine(stroke: StrokeStyle(lineWidth: 0.5, dash: [2, 4]))
                    .foregroundStyle(Color.secondary.opacity(0.18))
                AxisValueLabel(anchor: .leading).font(.system(size: 10))
            }
        }
        .chartOverlay { proxy in
            GeometryReader { geometry in
                Rectangle().fill(.clear).contentShape(Rectangle())
                    .gesture(DragGesture(minimumDistance: 0)
                        .onChanged { value in
                            guard let frame = proxy.plotFrame else { return }
                            let plot = geometry[frame]
                            guard plot.width > 0, plot.contains(value.startLocation) else { return }
                            drag(Double((value.startLocation.x - plot.minX) / plot.width),
                                 Double((value.location.x - plot.minX) / plot.width), false)
                        }
                        .onEnded { value in
                            guard let frame = proxy.plotFrame else { return }
                            let plot = geometry[frame]
                            guard plot.width > 0, plot.contains(value.startLocation) else { return }
                            drag(Double((value.startLocation.x - plot.minX) / plot.width),
                                 Double((value.location.x - plot.minX) / plot.width), true)
                        })
                    .onContinuousHover { phase in
                        switch phase {
                        case .active(let point):
                            guard let plotFrame = proxy.plotFrame, !buckets.isEmpty else { return }
                            let x = point.x - geometry[plotFrame].minX
                            guard let date: Date = proxy.value(atX: x) else { return }
                            let index = min(buckets.count - 1, max(0, Int(date.timeIntervalSince(start) / timeWindow * Double(buckets.count))))
                            let bucket = buckets[index]
                            let formatter = DateFormatter(); formatter.dateFormat = interval < 60 ? "HH:mm:ss" : "HH:mm"
                            hover(index, "\(formatter.string(from: bucket.start)) · \(bucket.success) successful · \(bucket.failed) failed")
                        case .ended: hover(nil, nil)
                        }
                    }
            }
        }
        .padding(.top, 6)
        .transaction { $0.animation = nil; $0.disablesAnimations = true }
        .accessibilityLabel("Request activity")
    }
}

@MainActor
final class ChartView: NSView {
    var buckets: [ChartBucket] = [] { didSet { refreshChart() } }
    var timeWindow: TimeInterval = 3600 { didSet { refreshChart() } }
    var hover: ((String?) -> Void)?
    var selected: ((RequestTimeRange, Date) -> Void)?
    var selection: RequestTimeRange? { didSet { refreshChart() } }
    private(set) var hoverDescription: String?
    private var hoveredIndex: Int?
    private var frozenBuckets: [ChartBucket]?
    private var frozenWindow: TimeInterval?
    private var dragSelection: RequestTimeRange?
    private var host: NSHostingView<ActivityChart>!
    private struct RenderState: Equatable {
        let buckets: [ChartBucket]
        let window: TimeInterval
        let hovered: Int?
        let selection: RequestTimeRange?
    }
    private var rendered: RenderState?
    private var updating = false
    override init(frame: NSRect) {
        super.init(frame: frame)
        host = NSHostingView(rootView: content)
        host.sizingOptions = []
        addSubview(host)
        rendered = renderState
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    func update(buckets: [ChartBucket], timeWindow: TimeInterval, selection: RequestTimeRange?) {
        updating = true
        self.buckets = buckets; self.timeWindow = timeWindow; self.selection = selection
        updating = false
        refreshChart()
    }
    private var renderState: RenderState {
        RenderState(buckets: frozenBuckets ?? buckets, window: frozenWindow ?? timeWindow,
                    hovered: hoveredIndex, selection: dragSelection ?? selection)
    }
    private var content: ActivityChart {
        ActivityChart(buckets: frozenBuckets ?? buckets, timeWindow: frozenWindow ?? timeWindow, hovered: hoveredIndex, hover: { [weak self] index, text in
            guard let self else { return }
            guard self.frozenBuckets == nil else { return }
            self.hoverDescription = text
            self.hover?(text)
            if self.hoveredIndex != index {
                self.hoveredIndex = index
                self.refreshChart()
            }
        }, selection: dragSelection ?? selection, drag: { [weak self] start, end, ended in
            self?.select(startFraction: start, endFraction: end, ended: ended)
        })
    }
    /// Keeps the time axis fixed for the full gesture, even when live data arrives.
    func select(startFraction: Double, endFraction: Double, ended: Bool) {
        if frozenBuckets == nil { frozenBuckets = buckets; frozenWindow = timeWindow }
        guard let start = frozenBuckets?.first?.start, let count = frozenBuckets?.count, count > 0 else {
            frozenBuckets = nil; frozenWindow = nil; return
        }
        let chart = RequestTimeRange(start: start, end: start.addingTimeInterval(frozenWindow ?? timeWindow))
        guard let range = RequestTimeRange.chartSelection(startFraction: startFraction, endFraction: endFraction, chart: chart, buckets: count) else { return }
        dragSelection = range
        if ended {
            hoverDescription = nil; hoveredIndex = nil; hover?(nil)
            selection = range
            dragSelection = nil; frozenBuckets = nil; frozenWindow = nil
            selected?(range, chart.end)
        }
        refreshChart()
    }
    private func refreshChart() {
        guard !updating, host != nil else { return }
        let state = renderState
        guard rendered != state else { return }
        rendered = state
        host.rootView = content
    }
    override func layout() { super.layout(); host.frame = bounds }
}
