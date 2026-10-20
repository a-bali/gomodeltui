import AppKit

@MainActor
final class UsageView: NSView {
    var refreshRequested: (() -> Void)?
    var sizeChanged: (() -> Void)?
    private let root = NSStackView()
    private let rows = NSStackView()
    private let quotaFlow = QuotaFlowView()
    private let refreshButton = NSButton()
    private var lastPresentation: UsagePresentation?
    private var quotaCells: [QuotaCell] = []

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        refreshButton.image = NSImage(systemSymbolName: "arrow.clockwise", accessibilityDescription: "Refresh provider usage")
        refreshButton.title = ""
        refreshButton.isBordered = false
        refreshButton.controlSize = .mini
        refreshButton.target = self
        refreshButton.action = #selector(refresh)
        refreshButton.toolTip = "Refresh provider usage"
        refreshButton.setAccessibilityLabel("Refresh provider usage")
        let heading = NSStackView(views: [NSView(), refreshButton])
        heading.orientation = .horizontal
        heading.alignment = .centerY
        heading.spacing = 4
        rows.orientation = .vertical
        rows.alignment = .leading
        rows.spacing = 3
        quotaFlow.sizeChanged = { [weak self] in self?.sizeChanged?() }
        root.orientation = .vertical
        root.alignment = .leading
        root.spacing = 3
        root.addArrangedSubview(heading)
        root.addArrangedSubview(rows)
        root.translatesAutoresizingMaskIntoConstraints = false
        addSubview(root)
        NSLayoutConstraint.activate([
            root.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 16),
            root.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -16),
            root.topAnchor.constraint(equalTo: topAnchor, constant: 2),
            root.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -2),
            heading.widthAnchor.constraint(equalTo: root.widthAnchor),
            rows.widthAnchor.constraint(equalTo: root.widthAnchor),
            refreshButton.widthAnchor.constraint(equalToConstant: 18),
            refreshButton.heightAnchor.constraint(equalToConstant: 16),
        ])
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    func update(_ presentation: UsagePresentation) {
        guard lastPresentation != presentation else { return }
        lastPresentation = presentation
        quotaCells = []
        rows.arrangedSubviews.forEach { rows.removeArrangedSubview($0); $0.removeFromSuperview() }
        refreshButton.isEnabled = !presentation.isRefreshing
        if presentation.isRefreshing { refreshButton.image = NSImage(systemSymbolName: "arrow.clockwise", accessibilityDescription: "Refreshing provider usage") }
        else { refreshButton.image = NSImage(systemSymbolName: "arrow.clockwise", accessibilityDescription: "Refresh provider usage") }

        if !presentation.quotas.isEmpty {
            quotaFlow.setProviders(presentation.quotas)
            quotaCells = quotaFlow.cells
            rows.addArrangedSubview(quotaFlow)
        }

        if !presentation.balances.isEmpty {
            let balanceItems = presentation.balances.map(BalanceItem.init)
            var separatedItems: [NSView] = []
            for (index, item) in balanceItems.enumerated() {
                if index > 0 {
                    let dot = NSView(frame: NSRect(x: 0, y: 0, width: 3, height: 3))
                    dot.wantsLayer = true
                    dot.layer?.backgroundColor = NSColor.tertiaryLabelColor.cgColor
                    dot.layer?.cornerRadius = 1.5
                    dot.widthAnchor.constraint(equalToConstant: 3).isActive = true
                    dot.heightAnchor.constraint(equalToConstant: 3).isActive = true
                    separatedItems.append(dot)
                }
                separatedItems.append(item)
            }
            let balanceRow = NSStackView(views: separatedItems)
            balanceRow.orientation = .horizontal
            balanceRow.alignment = .centerY
            balanceRow.spacing = 9
            balanceRow.distribution = .fill
            balanceRow.setContentHuggingPriority(.required, for: .horizontal)
            let balanceContainer = NSView()
            balanceRow.translatesAutoresizingMaskIntoConstraints = false
            balanceContainer.addSubview(balanceRow)
            NSLayoutConstraint.activate([
                balanceRow.leadingAnchor.constraint(equalTo: balanceContainer.leadingAnchor),
                balanceRow.topAnchor.constraint(equalTo: balanceContainer.topAnchor),
                balanceRow.bottomAnchor.constraint(equalTo: balanceContainer.bottomAnchor),
                balanceRow.heightAnchor.constraint(equalToConstant: 18),
            ])
            rows.addArrangedSubview(balanceContainer)
        }
        isHidden = presentation.quotas.isEmpty && presentation.balances.isEmpty
        sizeChanged?()
    }

    func tick() { quotaCells.forEach { $0.tick() } }

    var requiredHeight: CGFloat {
        guard !isHidden else { return 0 }
        let hasQuotas = !quotaCells.isEmpty
        let hasBalances = rows.arrangedSubviews.contains { $0 !== quotaFlow }
        if hasQuotas { return 23 + quotaFlow.height + (hasBalances ? 21 : 0) }
        return hasBalances ? 41 : 0
    }

    @objc private func refresh() { refreshRequested?() }
}

@MainActor
private final class QuotaFlowView: NSView {
    var sizeChanged: (() -> Void)?
    private var providerRows: [QuotaProviderRow] = []
    private var heightConstraint: NSLayoutConstraint!
    var cells: [QuotaCell] { providerRows.flatMap(\.cells) }
    var height: CGFloat { heightConstraint.constant }
    override var isFlipped: Bool { true }

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        heightConstraint = heightAnchor.constraint(equalToConstant: 0)
        heightConstraint.isActive = true
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    func setProviders(_ providers: [QuotaProvider]) {
        providerRows.forEach { $0.removeFromSuperview() }
        providerRows = providers.map(QuotaProviderRow.init)
        let nameFont = NSFont.systemFont(ofSize: 10, weight: .medium)
        let nameWidth = providers.map { ceil(($0.name as NSString).size(withAttributes: [.font: nameFont]).width) + 1 }.max() ?? 0
        providerRows.forEach { $0.setNameColumnWidth(nameWidth) }
        let quotaCount = providerRows.map { $0.cells.count }.max() ?? 0
        let labelWidths = (0..<quotaCount).map { index in
            providerRows.compactMap { $0.cells.indices.contains(index) ? $0.cells[index].naturalLabelWidth : nil }.max() ?? 0
        }
        providerRows.forEach { $0.setQuotaLabelWidths(labelWidths) }
        providerRows.forEach(addSubview)
        needsLayout = true
        layoutSubtreeIfNeeded()
    }

    override func layout() {
        super.layout()
        let lineHeight: CGFloat = 17
        let lineGap: CGFloat = 4
        for (index, row) in providerRows.enumerated() {
            row.layoutSubtreeIfNeeded()
            row.frame = NSRect(x: 0, y: CGFloat(index) * (lineHeight + lineGap), width: bounds.width, height: lineHeight)
        }
        let height = providerRows.isEmpty ? 0 : CGFloat(providerRows.count) * lineHeight + CGFloat(providerRows.count - 1) * lineGap
        if heightConstraint.constant != height {
            heightConstraint.constant = height
            sizeChanged?()
        }
    }
}

@MainActor
private final class QuotaProviderRow: NSView {
    let cells: [QuotaCell]
    private let providerName: ProviderNameLink
    private var providerNameWidth: NSLayoutConstraint?
    init(_ provider: QuotaProvider) {
        cells = provider.quotas.map(QuotaCell.init)
        let providerURL: URL?
        switch provider.id {
        case "opencode": providerURL = URL(string: "https://opencode.ai/console")
        case "codex": providerURL = URL(string: "https://chatgpt.com/settings/usage?tab=overview")
        case "commandcode": providerURL = URL(string: "https://commandcode.ai/usage")
        case "openrouter": providerURL = URL(string: "https://openrouter.ai/credits")
        case "mimo": providerURL = URL(string: "https://platform.xiaomimimo.com/#/console/balance")
        default: providerURL = nil
        }
        providerName = ProviderNameLink(title: provider.name, url: providerURL)
        super.init(frame: .zero)
        let quotaStack = NSStackView(views: cells)
        quotaStack.orientation = .horizontal
        quotaStack.alignment = .centerY
        quotaStack.spacing = 15
        quotaStack.setContentHuggingPriority(.required, for: .horizontal)
        quotaStack.setContentCompressionResistancePriority(.required, for: .horizontal)
        let row = NSStackView(views: [providerName, quotaStack])
        row.orientation = .horizontal
        row.alignment = .centerY
        row.distribution = .fill
        row.spacing = 4
        row.translatesAutoresizingMaskIntoConstraints = false
        addSubview(row)
        providerNameWidth = providerName.widthAnchor.constraint(equalToConstant: 0)
        NSLayoutConstraint.activate([
            row.leadingAnchor.constraint(equalTo: leadingAnchor), row.trailingAnchor.constraint(equalTo: trailingAnchor),
            row.topAnchor.constraint(equalTo: topAnchor), row.bottomAnchor.constraint(equalTo: bottomAnchor),
            providerNameWidth!,
            heightAnchor.constraint(equalToConstant: 17),
        ])
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    func setNameColumnWidth(_ width: CGFloat) { providerNameWidth?.constant = width }
    func setQuotaLabelWidths(_ widths: [CGFloat]) {
        for (index, cell) in cells.enumerated() where widths.indices.contains(index) {
            cell.setColumnLabelWidth(widths[index])
        }
    }
}

@MainActor
private final class ProviderNameLink: NSView {
    private let url: URL?
    init(title: String, url: URL?) {
        self.url = url
        super.init(frame: .zero)
        let label = NSTextField(labelWithString: title)
        label.font = .systemFont(ofSize: 10, weight: .medium)
        label.textColor = .labelColor
        label.toolTip = "Open \(title)"
        label.translatesAutoresizingMaskIntoConstraints = false
        addSubview(label)
        NSLayoutConstraint.activate([
            label.leadingAnchor.constraint(equalTo: leadingAnchor), label.trailingAnchor.constraint(equalTo: trailingAnchor),
            label.centerYAnchor.constraint(equalTo: centerYAnchor),
        ])
        setAccessibilityElement(true)
        setAccessibilityRole(.button)
        setAccessibilityLabel(title)
        toolTip = "Open \(title)"
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    override func hitTest(_ point: NSPoint) -> NSView? {
        guard bounds.contains(convert(point, from: superview)) else { return nil }
        return self
    }
    override func resetCursorRects() {
        super.resetCursorRects()
        addCursorRect(bounds, cursor: .pointingHand)
    }
    override func mouseDown(with event: NSEvent) {
        if let url { NSWorkspace.shared.open(url) }
    }
}

@MainActor
private final class QuotaCell: NSView {
    private let eta: NSTextField
    private let resetDate: Date?
    private var labelWidthConstraint: NSLayoutConstraint?
    private var cellWidthConstraint: NSLayoutConstraint?
    var naturalLabelWidth: CGFloat { labelWidthConstraint?.constant ?? 0 }
    init(_ quota: UsageQuota) {
        eta = NSTextField(labelWithString: quota.resetsAt.map(Self.relativeETA) ?? "—")
        resetDate = quota.resetsAt
        super.init(frame: .zero)
        let visibleLabel = quota.label == "w" ? "weekly" : quota.label == "m" ? "monthly" : quota.label
        let label = NSTextField(labelWithString: visibleLabel)
        label.font = .monospacedSystemFont(ofSize: 9, weight: .medium)
        label.textColor = .secondaryLabelColor
        label.alignment = .left
        let labelWidth = ceil((visibleLabel as NSString).size(withAttributes: [.font: label.font!]).width)
        let tint: NSColor = quota.availablePercent <= 25 ? .systemRed : quota.availablePercent <= 50 ? .systemOrange : .systemGreen
        let bar = QuotaBar(percent: quota.availablePercent, tint: tint)
        let percent = NSTextField(labelWithString: "\(Int(quota.availablePercent.rounded()))%")
        percent.font = .monospacedDigitSystemFont(ofSize: 9, weight: .medium)
        percent.textColor = tint
        percent.alignment = .left
        eta.font = .monospacedDigitSystemFont(ofSize: 9, weight: .regular)
        eta.textColor = .tertiaryLabelColor
        eta.alignment = .left
        if let date = quota.resetsAt {
            let formatter = DateFormatter()
            formatter.dateStyle = .medium; formatter.timeStyle = .short
            eta.toolTip = "Resets \(formatter.string(from: date))"
        } else { eta.toolTip = "Reset time unavailable" }
        let result = NSStackView(views: [percent, eta])
        result.orientation = .horizontal
        result.alignment = .centerY
        result.spacing = 3
        let stack = NSStackView(views: [label, bar, result])
        stack.orientation = .horizontal
        stack.alignment = .centerY
        stack.spacing = 1
        stack.translatesAutoresizingMaskIntoConstraints = false
        addSubview(stack)
        labelWidthConstraint = label.widthAnchor.constraint(equalToConstant: labelWidth)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: leadingAnchor), stack.trailingAnchor.constraint(equalTo: trailingAnchor),
            stack.topAnchor.constraint(equalTo: topAnchor), stack.bottomAnchor.constraint(equalTo: bottomAnchor),
            labelWidthConstraint!, bar.widthAnchor.constraint(equalToConstant: 34), bar.heightAnchor.constraint(equalToConstant: 5),
            percent.widthAnchor.constraint(equalToConstant: 25), eta.widthAnchor.constraint(equalToConstant: 42),
        ])
        cellWidthConstraint = widthAnchor.constraint(equalToConstant: labelWidth + 106)
        cellWidthConstraint?.isActive = true
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    func setColumnLabelWidth(_ width: CGFloat) {
        labelWidthConstraint?.constant = width
        cellWidthConstraint?.constant = width + 106
    }
    func tick() { if let resetDate { eta.stringValue = Self.relativeETA(resetDate) } }

    private static func relativeETA(_ date: Date) -> String {
        let seconds = max(0, Int(date.timeIntervalSinceNow))
        let days = seconds / 86_400, hours = seconds % 86_400 / 3_600, minutes = seconds % 3_600 / 60
        if days > 0 { return "\(days)d \(hours)h" }
        if hours > 0 { return "\(hours)h \(minutes)m" }
        return "\(max(1, minutes))m"
    }
}

@MainActor
private final class QuotaBar: NSView {
    private let fill = NSView()
    private var fillWidth: NSLayoutConstraint!
    init(percent: Double, tint: NSColor) {
        super.init(frame: .zero)
        wantsLayer = true
        layer?.backgroundColor = NSColor.quaternaryLabelColor.cgColor
        layer?.cornerRadius = 2.5
        fill.wantsLayer = true
        fill.layer?.backgroundColor = tint.cgColor
        fill.layer?.cornerRadius = 2.5
        fill.translatesAutoresizingMaskIntoConstraints = false
        addSubview(fill)
        fillWidth = fill.widthAnchor.constraint(equalTo: widthAnchor, multiplier: min(1, max(0, percent / 100)))
        NSLayoutConstraint.activate([
            fill.leadingAnchor.constraint(equalTo: leadingAnchor), fill.topAnchor.constraint(equalTo: topAnchor),
            fill.bottomAnchor.constraint(equalTo: bottomAnchor), fillWidth,
        ])
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
}

@MainActor
private final class BalanceItem: NSView {
    private var providerButton: NSButton?

    init(_ provider: BalanceProvider) {
        super.init(frame: .zero)
        let button = NSButton(title: provider.name, target: nil, action: nil)
        button.isBordered = false
        button.font = .systemFont(ofSize: 10, weight: .medium)
        button.contentTintColor = .labelColor
        button.alignment = .left
        button.toolTip = "Open \(provider.name)"
        button.target = self
        button.action = #selector(openProvider)
        providerButton = button
        let text = provider.balance ?? "—"
        let amount = NSTextField(labelWithString: text)
        amount.font = .monospacedDigitSystemFont(ofSize: 10, weight: .regular)
        if provider.error != nil {
            amount.textColor = .systemRed
        } else if let usd = provider.balanceUSD {
            amount.textColor = usd < 1 ? .systemRed : usd <= 5 ? .systemYellow : .systemGreen
        } else {
            amount.textColor = .secondaryLabelColor
        }
        let stack = NSStackView()
        stack.orientation = .horizontal; stack.alignment = .centerY; stack.spacing = 4
        if provider.error != nil {
            let warning = NSImageView(image: NSImage(systemSymbolName: "exclamationmark.triangle.fill", accessibilityDescription: "Provider usage error") ?? NSImage())
            warning.contentTintColor = .systemRed
            warning.toolTip = provider.error
            warning.setContentHuggingPriority(.required, for: .horizontal)
            stack.addArrangedSubview(warning)
        }
        stack.addArrangedSubview(button); stack.addArrangedSubview(amount)
        button.toolTip = provider.error.map { "Open \(provider.name) · \($0)" } ?? "Open \(provider.name)"
        stack.translatesAutoresizingMaskIntoConstraints = false
        addSubview(stack)
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: leadingAnchor), stack.trailingAnchor.constraint(equalTo: trailingAnchor),
            stack.topAnchor.constraint(equalTo: topAnchor), stack.bottomAnchor.constraint(equalTo: bottomAnchor),
            button.widthAnchor.constraint(greaterThanOrEqualToConstant: 44),
        ])
        self.providerURL = provider.url
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    override func resetCursorRects() {
        super.resetCursorRects()
        guard let providerButton else { return }
        addCursorRect(providerButton.convert(providerButton.bounds, to: self), cursor: .pointingHand)
    }
    private var providerURL: URL?
    @objc private func openProvider() { if let providerURL { NSWorkspace.shared.open(providerURL) } }
}
