import AppKit
import GoModelCore

@MainActor
final class PanelController: NSViewController {
    private let monitor: Monitor
    private let usage: UsageManager
    private let chart = ChartView()
    private let usageView = UsageView()
    private let requests = RequestLog()
    private let connection = NSTextField(labelWithString: "●")
    private let totals = NSTextField(labelWithString: "")
    private let caption = NSTextField(labelWithString: "")
    private let empty = NSTextField(labelWithString: "Waiting for requests…")
    private let latest = NSButton(title: "Live", target: nil, action: nil)
    private let picker = NSPopUpButton()
    private let mcp = NSButton(checkboxWithTitle: "MCP", target: nil, action: nil)
    private var lastRevision = -1
    private var lastCount = 0
    private var lastFilter: LogTimeWindow?
    private var inspector: JSONInspectorController?
    private var chartHeightConstraint: NSLayoutConstraint!
    private var usageHeightConstraint: NSLayoutConstraint!
    private var preferredChartHeight: CGFloat = 148
    var resizeRequested: ((NSSize, Bool) -> Void)?
    var dismissRequested: (() -> Void)?

    init(monitor: Monitor, usage: UsageManager) { self.monitor = monitor; self.usage = usage; super.init(nibName: nil, bundle: nil) }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    override func loadView() {
        let content = DropdownContentView(frame: NSRect(x: 0, y: 0, width: 800, height: 600))
        view = content
        let title = NSTextField(labelWithString: "GoModel")
        title.font = .systemFont(ofSize: 13, weight: .semibold)
        connection.font = .systemFont(ofSize: 10)
        connection.setAccessibilityLabel("Connection status")
        picker.addItems(withTitles: Monitor.windows.map(\.0)); picker.selectItem(at: 2)
        picker.controlSize = .small; picker.font = .systemFont(ofSize: 11)
        picker.target = self; picker.action = #selector(changeWindow)
        picker.setAccessibilityLabel("Chart time window")
        mcp.state = .off
        mcp.controlSize = .small; mcp.font = .systemFont(ofSize: 11)
        mcp.target = self; mcp.action = #selector(changeMCP)
        mcp.toolTip = "Include MCP requests in the charts, totals, and log"
        let header = NSStackView(views: [connection, title, totals, NSView(), mcp, picker])
        header.spacing = 8
        totals.font = .monospacedDigitSystemFont(ofSize: 11, weight: .medium)
        totals.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        totals.lineBreakMode = .byTruncatingTail
        caption.font = .systemFont(ofSize: 10)
        caption.textColor = .secondaryLabelColor
        caption.lineBreakMode = .byTruncatingTail
        chart.hover = { [weak self] text in self?.caption.stringValue = text ?? self?.defaultCaption ?? "" }
        chart.selected = { [weak self] range, end in self?.monitor.selectRange(range, chartEnd: end) }
        usageView.refreshRequested = { [weak self] in self?.usage.refresh() }
        usageView.sizeChanged = { [weak self] in self?.fitUsage() }
        let divider = ChartDivider(frame: .zero)
        divider.chartHeight = { [weak self] in self?.chart.frame.height ?? 148 }
        divider.move = { [weak self] height, finished in self?.resizeChart(to: height, save: finished) }
        latest.controlSize = .small; latest.bezelStyle = .rounded
        latest.font = .systemFont(ofSize: 10, weight: .medium)
        latest.image = NSImage(systemSymbolName: "arrow.down.circle.fill", accessibilityDescription: "Return to live logs")
        latest.imagePosition = .imageLeading
        latest.toolTip = "Clear the time filter and follow the live last hour"
        latest.target = self; latest.action = #selector(followLatest)
        empty.font = .systemFont(ofSize: 12); empty.textColor = .secondaryLabelColor
        empty.alignment = .center; empty.maximumNumberOfLines = 3
        for element in [header, usageView, chart, caption, divider, requests, empty, latest] {
            element.translatesAutoresizingMaskIntoConstraints = false
            view.addSubview(element)
        }
        let savedHeight = UserDefaults.standard.double(forKey: "dropdownChartHeight")
        preferredChartHeight = savedHeight.isFinite && savedHeight >= 80 ? savedHeight : 148
        chartHeightConstraint = chart.heightAnchor.constraint(equalToConstant: min(preferredChartHeight, view.bounds.height - 209))
        usageHeightConstraint = usageView.heightAnchor.constraint(equalToConstant: 0)
        usageHeightConstraint.priority = .defaultHigh
        // Preserve the preferred chart height, while allowing a smaller outer
        // dropdown to keep both the chart and the logs usable.
        chartHeightConstraint.priority = .defaultHigh
        content.sizeChanged = { [weak self] size in self?.fitChart(to: size) }
        NSLayoutConstraint.activate([
            header.topAnchor.constraint(equalTo: view.topAnchor, constant: 12),
            header.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 16),
            header.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -16),
            header.heightAnchor.constraint(equalToConstant: 24),
            picker.widthAnchor.constraint(equalToConstant: 110),
            usageView.topAnchor.constraint(equalTo: header.bottomAnchor, constant: 3),
            usageView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            usageView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            usageHeightConstraint,
            chart.topAnchor.constraint(equalTo: usageView.bottomAnchor, constant: 4),
            chart.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 16),
            chart.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -16),
            chartHeightConstraint,
            chart.heightAnchor.constraint(greaterThanOrEqualToConstant: 80),
            caption.topAnchor.constraint(equalTo: chart.bottomAnchor, constant: 5),
            caption.leadingAnchor.constraint(equalTo: header.leadingAnchor),
            caption.trailingAnchor.constraint(equalTo: header.trailingAnchor),
            caption.heightAnchor.constraint(equalToConstant: 14),
            divider.topAnchor.constraint(equalTo: caption.bottomAnchor, constant: 5),
            divider.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            divider.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            divider.heightAnchor.constraint(equalToConstant: 10),
            requests.topAnchor.constraint(equalTo: divider.bottomAnchor, constant: 5),
            requests.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 10),
            requests.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -10),
            requests.bottomAnchor.constraint(equalTo: view.bottomAnchor, constant: -8),
            requests.heightAnchor.constraint(greaterThanOrEqualToConstant: 120),
            empty.centerXAnchor.constraint(equalTo: requests.centerXAnchor),
            empty.centerYAnchor.constraint(equalTo: requests.centerYAnchor),
            empty.widthAnchor.constraint(lessThanOrEqualTo: requests.widthAnchor, constant: -48),
            latest.centerXAnchor.constraint(equalTo: view.centerXAnchor),
            latest.bottomAnchor.constraint(equalTo: view.bottomAnchor, constant: -12),
            latest.heightAnchor.constraint(equalToConstant: 24),
        ])
        for left in [true, false] {
            let grip = ResizeGrip(left: left)
            grip.resize = { [weak self] size, finished in self?.resizeRequested?(size, finished) }
            grip.translatesAutoresizingMaskIntoConstraints = false
            view.addSubview(grip)
            NSLayoutConstraint.activate([
                grip.widthAnchor.constraint(equalToConstant: 20),
                grip.heightAnchor.constraint(equalToConstant: 20),
                grip.bottomAnchor.constraint(equalTo: view.bottomAnchor, constant: -2),
                left ? grip.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 2) :
                       grip.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -2),
            ])
        }
        requests.followChanged = { [weak self] in
            guard let self else { return }
            if !self.requests.following { self.monitor.pauseLog() }
            else { self.monitor.resumeLogFollowing() }
            self.updateFollow()
        }
        requests.loadEarlier = { [weak self] in self?.monitor.loadEarlier() }
        requests.visibleChanged = { [weak self] rows in self?.monitor.loadVisiblePreviews(rows) }
        requests.document.openRequest = { [weak self] record in
            guard let self else { return }
            if self.inspector == nil { self.inspector = JSONInspectorController() }
            self.inspector?.show(record: record, loader: self.monitor.detailLoader(for: record), relativeTo: self.view.window)
            // The inspector is a regular window; close the dropdown so it is
            // not covered by the popover's higher window level.
            self.dismissRequested?()
        }
    }

    private var defaultCaption: String {
        var warnings = [monitor.historyStatus, monitor.eventWarning].filter { !$0.isEmpty }
        if let selection = monitor.logWindow.selection {
            let formatter = DateFormatter(); formatter.dateFormat = "MMM d HH:mm:ss"
            warnings.insert("\(formatter.string(from: selection.start)) – \(formatter.string(from: selection.end)) · \(lastCount) requests", at: 0)
        }
        if !warnings.isEmpty { return warnings.joined(separator: " · ") }
        let seconds = monitor.availableWindow / 60
        return seconds >= 60 ? String(format: "Requests per %g min", seconds / 60) : String(format: "Requests per %g sec", seconds)
    }

    override func viewDidDisappear() {
        super.viewDidDisappear()
        monitor.stopPreviews()
    }

    func refresh(reset: Bool = false) {
        loadViewIfNeeded()
        if reset { inspector?.close() }
        usageView.update(usage.presentation)
        usageView.tick()
        fitUsage()
        connection.textColor = monitor.connected ? .systemGreen : .systemRed
        connection.toolTip = monitor.connectionStatus
        connection.setAccessibilityValue(monitor.connectionStatus)
        mcp.state = monitor.includeMCP ? .on : .off
        chart.update(buckets: monitor.activity.buckets(now: monitor.displayChartEnd, window: monitor.availableWindow,
                     through: monitor.now, includeMCP: monitor.includeMCP),
                     timeWindow: monitor.availableWindow, selection: monitor.logWindow.selection)
        requests.canLoadEarlier = monitor.logWindow.selection == nil && !monitor.loadingHistory &&
            monitor.logWindow.range(now: monitor.now).start > monitor.now.addingTimeInterval(-monitor.retention)
        let counted = chart.buckets.filter { bucket in
            guard let selection = monitor.logWindow.selection else { return true }
            return bucket.start < selection.end && bucket.start.addingTimeInterval(monitor.availableWindow / 60) > selection.start
        }
        let success = counted.reduce(0) { $0 + $1.success }
        let failed = counted.reduce(0) { $0 + $1.failed }
        let interval: String
        if let selection = monitor.logWindow.selection {
            interval = "in selected \(compactDuration(selection.duration))"
        } else { interval = "in last \(compactDuration(monitor.availableWindow))" }
        let text = NSMutableAttributedString(string: "\(success + failed) req \(interval) (\(success) ok, \(failed) failed)",
            attributes: [.font: totals.font!, .foregroundColor: NSColor.secondaryLabelColor])
        text.addAttribute(.foregroundColor, value: NSColor.labelColor, range: (text.string as NSString).range(of: "\(success + failed) req"))
        text.addAttribute(.foregroundColor, value: NSColor.systemGreen, range: (text.string as NSString).range(of: "\(success) ok"))
        text.addAttribute(.foregroundColor, value: NSColor.systemRed, range: (text.string as NSString).range(of: "\(failed) failed"))
        totals.attributedStringValue = text
        let filterChanged = lastFilter?.selection != monitor.logWindow.selection || lastFilter?.duration != monitor.logWindow.duration
        if lastRevision != monitor.revision || reset || monitor.logWindow.selection == nil {
            let records = monitor.logRecords()
            requests.update(records, reset: reset || filterChanged, includeMCP: mcp.state == .on)
            lastCount = requests.document.rows.count; lastRevision = monitor.revision
            lastFilter = monitor.logWindow
        }
        requests.refreshLiveLatency()
        caption.stringValue = chart.hoverDescription ?? defaultCaption
        caption.toolTip = defaultCaption
        empty.isHidden = lastCount > 0
        empty.stringValue = monitor.configuration == nil && !monitor.connected ? "Right-click the menu bar icon and choose Config… to connect." : monitor.loadingLogHistory ? "Loading requests…" : "No requests in this interval."
        updateFollow()
    }

    private func updateFollow() {
        latest.isHidden = requests.following && monitor.logWindow.isLive
        latest.setAccessibilityLabel("Return to live logs")
    }
    private func resizeChart(to height: CGFloat, save: Bool) {
        guard height.isFinite else { return }
        let maximum = max(80, chart.frame.height + requests.frame.height - 120)
        preferredChartHeight = min(maximum, max(80, height))
        fitChart(to: view.bounds.size)
        view.layoutSubtreeIfNeeded()
        if save { UserDefaults.standard.set(preferredChartHeight, forKey: "dropdownChartHeight") }
    }
    private func fitChart(to size: NSSize) {
        // Header, usage summary, caption, divider, margins, and at least 120 points of logs.
        let height = min(preferredChartHeight, max(80, size.height - 209 - usageHeightConstraint.constant))
        if chartHeightConstraint.constant != height { chartHeightConstraint.constant = height }
    }
    private func fitUsage() {
        let height = usageView.requiredHeight
        guard usageHeightConstraint.constant != height else { return }
        usageHeightConstraint.constant = height
        fitChart(to: view.bounds.size)
    }
    private func compactDuration(_ seconds: TimeInterval) -> String {
        let total = max(1, Int(seconds.rounded()))
        let components = [(total / 3600, "h"), (total % 3600 / 60, "m"), (total % 60, "s")]
        return components.filter { $0.0 > 0 }.prefix(2).map { "\($0.0)\($0.1)" }.joined(separator: " ")
    }
    @objc private func changeWindow() { monitor.selectWindow(Monitor.windows[picker.indexOfSelectedItem].1) }
    @objc private func changeMCP() { monitor.setIncludeMCP(mcp.state == .on); refresh() }
    @objc private func followLatest() {
        requests.resumeFollowing()
        monitor.resumeLive()
        picker.selectItem(at: 2)
    }
}

@MainActor
private final class DropdownContentView: NSView {
    var sizeChanged: ((NSSize) -> Void)?
    override func setFrameSize(_ newSize: NSSize) {
        sizeChanged?(newSize)
        super.setFrameSize(newSize)
    }
    override func resizeSubviews(withOldSize oldSize: NSSize) {
        sizeChanged?(bounds.size)
        super.resizeSubviews(withOldSize: oldSize)
    }
}
