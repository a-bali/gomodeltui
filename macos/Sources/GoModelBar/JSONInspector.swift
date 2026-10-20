import AppKit
import GoModelCore

private func inBackground<T: Sendable>(_ work: @escaping @Sendable () throws -> T) async throws -> T {
    let task = Task.detached(priority: .userInitiated, operation: work)
    return try await withTaskCancellationHandler(operation: { try await task.value }, onCancel: { task.cancel() })
}

@MainActor
enum JSONSyntax {
    static let font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
    static func line(_ node: JSONNode, expanded: Bool, matches: [NSRange] = [], active: NSRange? = nil, wrapping: Bool = false) -> NSAttributedString {
        let text = node.text(expanded: expanded)
        let paragraph = NSMutableParagraphStyle(); paragraph.lineBreakMode = wrapping ? .byWordWrapping : .byClipping
        let result = NSMutableAttributedString(string: text, attributes: [.font: font, .foregroundColor: NSColor.secondaryLabelColor,
            .ligature: 0, .kern: 0, .paragraphStyle: paragraph])
        let prefixLength = (node.prefix as NSString).length
        if prefixLength > 2 { result.addAttribute(.foregroundColor, value: NSColor.systemBlue, range: NSRange(location: 0, length: prefixLength - 2)) }
        let color: NSColor = switch node.kind {
        case .string: .systemGreen
        case .number: .systemOrange
        case .boolean, .null: .systemPurple
        default: .labelColor
        }
        result.addAttribute(.foregroundColor, value: color, range: NSRange(location: prefixLength, length: (node.value as NSString).length))
        for range in matches where NSMaxRange(range) <= result.length {
            result.addAttributes([.backgroundColor: NSColor.systemYellow.withAlphaComponent(0.5), .foregroundColor: NSColor.black], range: range)
        }
        if let active, NSMaxRange(active) <= result.length {
            result.addAttributes([.backgroundColor: NSColor.systemOrange, .foregroundColor: NSColor.black], range: active)
        }
        return result
    }
}

@MainActor
private struct JSONTextLayout {
    let storage: NSTextStorage
    let manager: NSLayoutManager
    let container: NSTextContainer
    init(_ text: NSAttributedString, width: CGFloat, wrapping: Bool) {
        storage = NSTextStorage(attributedString: text)
        manager = NSLayoutManager()
        container = NSTextContainer(size: NSSize(width: max(1, width), height: .greatestFiniteMagnitude))
        container.lineFragmentPadding = 0
        container.maximumNumberOfLines = wrapping ? 0 : 1
        container.lineBreakMode = wrapping ? .byWordWrapping : .byClipping
        manager.addTextContainer(container); storage.addLayoutManager(manager)
        manager.ensureLayout(for: container)
    }
    var height: CGFloat { max(21, ceil(manager.usedRect(for: container).height) + 6) }
    func rect(for range: NSRange, firstGlyphOnly: Bool) -> NSRect {
        var glyphs = manager.glyphRange(forCharacterRange: range, actualCharacterRange: nil)
        if firstGlyphOnly { glyphs.length = min(1, glyphs.length) }
        return manager.boundingRect(forGlyphRange: glyphs, in: container)
    }
}

@MainActor
private final class JSONCell: NSTableCellView {
    private let text = NSTextView(frame: .zero)
    private var leading: NSLayoutConstraint!
    override init(frame: NSRect) {
        super.init(frame: frame)
        text.isEditable = false; text.isSelectable = true; text.drawsBackground = false
        text.isVerticallyResizable = false; text.isHorizontallyResizable = false
        text.textContainerInset = .zero
        text.textContainer?.lineFragmentPadding = 0
        text.textContainer?.widthTracksTextView = true
        text.textContainer?.heightTracksTextView = false
        text.textContainer?.containerSize = NSSize(width: 1, height: CGFloat.greatestFiniteMagnitude)
        text.translatesAutoresizingMaskIntoConstraints = false
        addSubview(text)
        leading = text.leadingAnchor.constraint(equalTo: leadingAnchor)
        NSLayoutConstraint.activate([leading, text.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -4),
            text.topAnchor.constraint(equalTo: topAnchor, constant: 3), text.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -3)])
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    func configure(_ content: NSAttributedString, closing: Bool, indent: CGFloat, wrapping: Bool) {
        leading.constant = closing ? -indent : 0
        text.textContainer?.maximumNumberOfLines = wrapping ? 0 : 1
        text.textContainer?.lineBreakMode = wrapping ? .byWordWrapping : .byClipping
        text.textStorage?.setAttributedString(content)
        text.setAccessibilityLabel(content.string)
    }
}

@MainActor
final class JSONOutlineView: NSOutlineView {
    var toggleContainer: ((JSONNode) -> Void)?

    override func hitTest(_ point: NSPoint) -> NSView? {
        guard let hit = super.hitTest(point) else { return nil }
        let location = convert(point, from: superview)
        let row = row(at: location)
        // Route container text, disclosure arrows, and trailing whitespace to
        // the same action. Scalar values keep native text selection behavior.
        if row >= 0, let node = item(atRow: row) as? JSONNode, node.expandable { return self }
        return hit
    }

    override func mouseDown(with event: NSEvent) {
        let row = row(at: convert(event.locationInWindow, from: nil))
        if row >= 0, let node = item(atRow: row) as? JSONNode, node.expandable {
            guard event.clickCount == 1 else { return }
            toggleContainer?(node)
            let currentRow = self.row(forItem: node)
            if currentRow >= 0 { selectRowIndexes(IndexSet(integer: currentRow), byExtendingSelection: false) }
            window?.makeFirstResponder(self)
        } else { super.mouseDown(with: event) }
    }
}

@MainActor
final class JSONInspectorViewController: NSViewController, NSOutlineViewDataSource, NSOutlineViewDelegate, NSSearchFieldDelegate {
    let search = NSSearchField()
    let previous = NSButton()
    let next = NSButton()
    let resultCount = NSTextField(labelWithString: "")
    let wordWrap = NSButton(checkboxWithTitle: "Word wrap", target: nil, action: nil)
    let expandCollapse = NSButton(title: "Expand All", target: nil, action: nil)
    let outline = JSONOutlineView()
    let scroll = NSScrollView()
    let status = NSTextField(labelWithString: "")
    private(set) var document: JSONDocument?
    private(set) var matches: [JSONMatch] = []
    private(set) var matchIndex = -1
    private var matchesByNode: [Int: [NSRange]] = [:]
    private(set) var searchTask: Task<Void, Never>?
    private var searchGeneration = UUID()
    private var column: NSTableColumn!
    private var naturalWidth: CGFloat = 200
    private var cellInset: CGFloat = 26
    private var rowHeights: [Int: CGFloat] = [:]
    private var updatingLayout = false
    private var pendingRowLayout = false
    private var bulkExpansion = false
    private var revealingSearch = false
    private var searchRevealSuppressed = false
    private struct ScrollAnchor {
        let node: JSONNode
        let offset: CGFloat
        let x: CGFloat
    }
    private var pendingScrollAnchor: ScrollAnchor?
    private var wrapping: Bool { wordWrap.state == .on }

    override func loadView() {
        view = NSView(frame: NSRect(x: 0, y: 0, width: 900, height: 620))
        search.placeholderString = "Search JSON"
        search.sendsSearchStringImmediately = true; search.sendsWholeSearchString = false
        search.delegate = self
        search.setAccessibilityLabel("Search request JSON")
        for (button, symbol, title, action) in [(previous, "chevron.up", "Previous result", #selector(previousMatch)), (next, "chevron.down", "Next result", #selector(nextMatch))] {
            button.image = NSImage(systemSymbolName: symbol, accessibilityDescription: title)
            button.bezelStyle = .rounded; button.controlSize = .small
            button.target = self; button.action = action; button.toolTip = title
            button.setAccessibilityLabel(title)
            button.widthAnchor.constraint(equalToConstant: 30).isActive = true
        }
        resultCount.font = .monospacedDigitSystemFont(ofSize: 11, weight: .regular)
        resultCount.textColor = .secondaryLabelColor; resultCount.alignment = .right
        resultCount.setContentHuggingPriority(.required, for: .horizontal)
        wordWrap.controlSize = .small; wordWrap.font = .systemFont(ofSize: 11)
        wordWrap.state = .off; wordWrap.target = self; wordWrap.action = #selector(changeWrapping)
        wordWrap.toolTip = "Wrap JSON lines while preserving each item's indentation"
        wordWrap.setContentHuggingPriority(.required, for: .horizontal)
        expandCollapse.controlSize = .small; expandCollapse.bezelStyle = .rounded
        expandCollapse.font = .systemFont(ofSize: 11)
        expandCollapse.target = self; expandCollapse.action = #selector(toggleAll)
        expandCollapse.widthAnchor.constraint(equalToConstant: 92).isActive = true
        let header = NSStackView(views: [search, wordWrap, expandCollapse, resultCount, previous, next]); header.spacing = 8
        column = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("json"))
        column.minWidth = 200; column.maxWidth = .greatestFiniteMagnitude
        outline.addTableColumn(column); outline.outlineTableColumn = column
        outline.headerView = nil; outline.dataSource = self; outline.delegate = self
        outline.style = .plain
        outline.rowHeight = 21; outline.intercellSpacing = NSSize(width: 0, height: 0)
        outline.indentationPerLevel = 14
        outline.columnAutoresizingStyle = .noColumnAutoresizing
        outline.autoresizesOutlineColumn = false
        outline.selectionHighlightStyle = .none
        outline.toggleContainer = { [weak self] node in self?.toggleContainer(node) }
        outline.setAccessibilityLabel("Request JSON tree")
        scroll.documentView = outline
        scroll.hasVerticalScroller = true; scroll.hasHorizontalScroller = true
        scroll.autohidesScrollers = true
        status.textColor = .secondaryLabelColor; status.alignment = .center
        status.maximumNumberOfLines = 3
        for element in [header, scroll, status] {
            element.translatesAutoresizingMaskIntoConstraints = false; view.addSubview(element)
        }
        NSLayoutConstraint.activate([
            header.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 14),
            header.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -14),
            header.topAnchor.constraint(equalTo: view.topAnchor, constant: 12),
            header.heightAnchor.constraint(equalToConstant: 26),
            scroll.topAnchor.constraint(equalTo: header.bottomAnchor, constant: 10),
            scroll.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 8),
            scroll.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -8),
            scroll.bottomAnchor.constraint(equalTo: view.bottomAnchor, constant: -8),
            status.centerXAnchor.constraint(equalTo: scroll.centerXAnchor),
            status.centerYAnchor.constraint(equalTo: scroll.centerYAnchor),
            status.widthAnchor.constraint(lessThanOrEqualTo: scroll.widthAnchor, constant: -40),
        ])
        updateControls()
    }

    private func updateExpansionControl() {
        guard let document else { expandCollapse.title = "Expand All"; expandCollapse.isEnabled = false; return }
        expandCollapse.isEnabled = document.nodes.contains { $0 !== document.root && $0.expandable }
        let allExpanded = document.nodes.allSatisfy { !$0.expandable || outline.isItemExpanded($0) }
        expandCollapse.title = allExpanded ? "Collapse All" : "Expand All"
        expandCollapse.toolTip = allExpanded ? "Collapse all nested objects and arrays" : "Expand every object and array"
        expandCollapse.setAccessibilityLabel(expandCollapse.title)
    }

    @objc func toggleAll() {
        guard let document, document.root.expandable else { return }
        let collapse = document.nodes.allSatisfy { !$0.expandable || outline.isItemExpanded($0) }
        searchRevealSuppressed = true
        pendingScrollAnchor = visibleAnchor()
        bulkExpansion = true
        if collapse {
            for child in document.root.children where child.expandable {
                outline.collapseItem(child, collapseChildren: true)
            }
        } else {
            outline.expandItem(document.root, expandChildren: true)
        }
        bulkExpansion = false
        updateColumnWidth(force: true)
        updateExpansionControl()
        pendingScrollAnchor = nil
    }

    private func anchor(for node: JSONNode) -> ScrollAnchor? {
        let row = outline.row(forItem: node)
        guard row >= 0 else { return nil }
        return ScrollAnchor(node: node, offset: scroll.contentView.bounds.minY - outline.rect(ofRow: row).minY,
            x: scroll.contentView.bounds.minX)
    }

    private func visibleAnchor() -> ScrollAnchor? {
        let row = outline.row(at: NSPoint(x: 0, y: max(0, scroll.contentView.bounds.minY)))
        guard row >= 0, let node = outline.item(atRow: row) as? JSONNode else { return nil }
        return anchor(for: node)
    }

    private func restore(_ anchor: ScrollAnchor?) {
        guard let anchor else { return }
        var row = outline.row(forItem: anchor.node)
        if row < 0, let ancestor = document?.ancestors(of: anchor.node).reversed().first(where: { outline.row(forItem: $0) >= 0 }) {
            row = outline.row(forItem: ancestor)
        }
        guard row >= 0 else { return }
        let y = max(0, min(outline.rect(ofRow: row).minY + anchor.offset, outline.bounds.height - scroll.contentSize.height))
        let x = max(0, min(anchor.x, outline.bounds.width - scroll.contentSize.width))
        scroll.contentView.scroll(to: NSPoint(x: x, y: y))
        scroll.reflectScrolledClipView(scroll.contentView)
    }

    private func preservePosition(of node: JSONNode) {
        guard !bulkExpansion, !revealingSearch else { return }
        searchRevealSuppressed = true
        pendingScrollAnchor = anchor(for: node)
    }

    override func viewDidLayout() {
        super.viewDidLayout()
        updateColumnWidth()
    }

    private func updateColumnWidth(force: Bool = false) {
        guard !updatingLayout, column != nil else { return }
        let width = max(200, wrapping ? scroll.contentSize.width : max(naturalWidth, scroll.contentSize.width))
        guard force || abs(column.width - width) > 0.5 else { return }
        let anchor = pendingScrollAnchor ?? visibleAnchor()
        updatingLayout = true
        column.width = width
        if outline.numberOfRows > 0, let first = outline.item(atRow: 0) as? JSONNode {
            let depth = document?.ancestors(of: first).count ?? 0
            cellInset = max(0, width - outline.frameOfCell(atColumn: 0, row: 0).width - CGFloat(depth) * outline.indentationPerLevel)
        }
        rowHeights = [:]
        if outline.numberOfRows > 0 { outline.noteHeightOfRows(withIndexesChanged: IndexSet(integersIn: 0..<outline.numberOfRows)) }
        redrawVisibleRows()
        updatingLayout = false
        outline.layoutSubtreeIfNeeded()
        restore(anchor)
    }

    @objc func changeWrapping() {
        pendingScrollAnchor = nil
        let visibleRow = outline.row(at: NSPoint(x: 0, y: scroll.contentView.bounds.minY))
        let anchor = visibleRow >= 0 ? outline.item(atRow: visibleRow) : nil
        scroll.hasHorizontalScroller = !wrapping
        if wrapping { scroll.contentView.scroll(to: NSPoint(x: 0, y: scroll.contentView.bounds.minY)) }
        updateColumnWidth(force: true)
        if matches.indices.contains(matchIndex), !searchRevealSuppressed { revealMatch() }
        else if let anchor {
            let row = outline.row(forItem: anchor)
            if row >= 0 { outline.scrollRowToVisible(row) }
        }
    }

    func reset(message: String = "Loading full request JSON…") {
        loadViewIfNeeded()
        searchTask?.cancel(); searchTask = nil; searchGeneration = UUID()
        document = nil; matches = []; matchesByNode = [:]; matchIndex = -1
        rowHeights = [:]; naturalWidth = 200
        pendingScrollAnchor = nil
        searchRevealSuppressed = false
        search.stringValue = ""; search.isEnabled = false
        resultCount.stringValue = ""; status.stringValue = message; status.isHidden = false
        outline.reloadData(); updateControls()
    }

    func display(_ document: JSONDocument) {
        loadViewIfNeeded()
        searchTask?.cancel(); searchGeneration = UUID()
        self.document = document
        pendingScrollAnchor = nil
        searchRevealSuppressed = false
        matches = []; matchesByNode = [:]; matchIndex = -1
        status.isHidden = true; search.isEnabled = true
        outline.reloadData()
        outline.expandItem(document.root, expandChildren: false)
        // Size every line once so even hidden long strings are never truncated.
        naturalWidth = document.nodes.reduce(CGFloat(0)) { width, node in
            let depth = document.ancestors(of: node).count - (node.kind == .closing ? 1 : 0)
            let size = (node.text(expanded: false) as NSString).size(withAttributes: [.font: JSONSyntax.font])
            return max(width, size.width + CGFloat(max(0, depth)) * outline.indentationPerLevel + 54)
        }
        updateColumnWidth(force: true)
        scroll.contentView.scroll(to: .zero)
        updateControls()
        if !search.stringValue.isEmpty { find(search.stringValue) }
    }

    func outlineView(_ outlineView: NSOutlineView, numberOfChildrenOfItem item: Any?) -> Int {
        (item as? JSONNode)?.children.count ?? (document == nil ? 0 : 1)
    }
    func outlineView(_ outlineView: NSOutlineView, child index: Int, ofItem item: Any?) -> Any {
        if let node = item as? JSONNode { return node.children[index] }
        return document!.root
    }
    func outlineView(_ outlineView: NSOutlineView, isItemExpandable item: Any) -> Bool { (item as? JSONNode)?.expandable ?? false }
    func outlineView(_ outlineView: NSOutlineView, viewFor tableColumn: NSTableColumn?, item: Any) -> NSView? {
        guard let node = item as? JSONNode else { return nil }
        let identifier = NSUserInterfaceItemIdentifier("json-cell")
        let cell = outlineView.makeView(withIdentifier: identifier, owner: self) as? JSONCell ?? JSONCell()
        cell.identifier = identifier
        let active = matches.indices.contains(matchIndex) && matches[matchIndex].nodeID == node.id ? matches[matchIndex].range : nil
        cell.configure(JSONSyntax.line(node, expanded: outlineView.isItemExpanded(node), matches: matchesByNode[node.id] ?? [], active: active, wrapping: wrapping),
            closing: node.kind == .closing, indent: outlineView.indentationPerLevel, wrapping: wrapping)
        return cell
    }
    private func textWidth(for node: JSONNode) -> CGFloat {
        let depth = document?.ancestors(of: node).count ?? 0
        let width = column.width - CGFloat(depth) * outline.indentationPerLevel - cellInset
        return max(1, width + (node.kind == .closing ? outline.indentationPerLevel : 0) - 4)
    }
    func outlineView(_ outlineView: NSOutlineView, heightOfRowByItem item: Any) -> CGFloat {
        guard wrapping, let node = item as? JSONNode else { return 21 }
        if let height = rowHeights[node.id] { return height }
        let layout = JSONTextLayout(JSONSyntax.line(node, expanded: outline.isItemExpanded(node), wrapping: true), width: textWidth(for: node), wrapping: true)
        rowHeights[node.id] = layout.height
        return layout.height
    }
    func outlineViewItemDidExpand(_ notification: Notification) { scheduleRowLayout() }
    func outlineViewItemDidCollapse(_ notification: Notification) { scheduleRowLayout() }
    func outlineView(_ outlineView: NSOutlineView, shouldExpandItem item: Any) -> Bool {
        if let node = item as? JSONNode { preservePosition(of: node) }
        return true
    }
    func outlineView(_ outlineView: NSOutlineView, shouldCollapseItem item: Any) -> Bool {
        if let node = item as? JSONNode { preservePosition(of: node) }
        return true
    }
    private func scheduleRowLayout() {
        guard !bulkExpansion, !pendingRowLayout else { return }
        pendingRowLayout = true
        Task { @MainActor [weak self] in
            guard let self else { return }
            self.pendingRowLayout = false
            self.updateColumnWidth(force: true)
            self.updateExpansionControl()
            self.pendingScrollAnchor = nil
        }
    }
    private func redrawVisibleRows() {
        let range = outline.rows(in: outline.visibleRect)
        if range.length > 0 { outline.reloadData(forRowIndexes: IndexSet(integersIn: range.location..<NSMaxRange(range)), columnIndexes: IndexSet(integer: 0)) }
    }

    func controlTextDidChange(_ notification: Notification) { find(search.stringValue) }
    func control(_ control: NSControl, textView: NSTextView, doCommandBy commandSelector: Selector) -> Bool {
        if commandSelector == #selector(NSResponder.insertNewline(_:)) {
            if NSApp.currentEvent?.modifierFlags.contains(.shift) == true { previousMatch() } else { nextMatch() }
            return true
        }
        return false
    }
    func find(_ query: String) {
        searchTask?.cancel()
        searchRevealSuppressed = false; pendingScrollAnchor = nil
        let generation = UUID(); searchGeneration = generation
        matches = []; matchesByNode = [:]; matchIndex = -1
        redrawVisibleRows(); updateControls()
        guard let document, !query.isEmpty else { resultCount.stringValue = ""; return }
        resultCount.stringValue = "Searching…"
        searchTask = Task { [weak self] in
            do {
                let found = try await inBackground { try document.matches(for: query) }
                guard let self, !Task.isCancelled, self.searchGeneration == generation else { return }
                self.matches = found
                self.matchesByNode = Dictionary(grouping: found, by: \.nodeID).mapValues { $0.map(\.range) }
                self.matchIndex = found.isEmpty ? -1 : 0
                if !self.searchRevealSuppressed { self.revealMatch() } else { self.redrawVisibleRows() }
                self.updateControls()
            } catch { /* A newer query or a closed inspector cancels the old search. */ }
        }
    }
    @objc func nextMatch() { moveMatch(1) }
    @objc func previousMatch() { moveMatch(-1) }
    private func moveMatch(_ delta: Int) {
        guard !matches.isEmpty else { return }
        searchRevealSuppressed = false
        matchIndex = (matchIndex + delta + matches.count) % matches.count
        revealMatch(); updateControls()
    }
    private func revealMatch() {
        guard matches.indices.contains(matchIndex), let document,
              let node = document.byID[matches[matchIndex].nodeID] else { redrawVisibleRows(); return }
        pendingScrollAnchor = nil
        revealingSearch = true
        defer { revealingSearch = false }
        for ancestor in document.ancestors(of: node) where !outline.isItemExpanded(ancestor) {
            outline.expandItem(ancestor, expandChildren: false)
        }
        updateColumnWidth(force: true)
        updateExpansionControl()
        // Container keys remain visible even when their own children are collapsed.
        let row = outline.row(forItem: node)
        guard row >= 0 else { return }
        let layout = JSONTextLayout(JSONSyntax.line(node, expanded: outline.isItemExpanded(node), wrapping: wrapping), width: textWidth(for: node), wrapping: wrapping)
        var target = layout.rect(for: matches[matchIndex].range, firstGlyphOnly: wrapping)
        let cell = outline.frameOfCell(atColumn: 0, row: row)
        target.origin.x += cell.minX; target.origin.y += outline.rect(ofRow: row).minY + 3
        if wrapping {
            target.origin.x = 0; target.size.width = min(column.width, scroll.contentSize.width)
        }
        outline.scrollToVisible(target.insetBy(dx: wrapping ? 0 : -12, dy: -8))
        redrawVisibleRows()
    }
    private func updateControls() {
        updateExpansionControl()
        previous.isEnabled = !matches.isEmpty; next.isEnabled = !matches.isEmpty
        if !search.stringValue.isEmpty { resultCount.stringValue = matches.isEmpty ? "0 results" : "\(matchIndex + 1) of \(matches.count)" }
        else { resultCount.stringValue = "" }
        resultCount.setAccessibilityValue(resultCount.stringValue)
    }
    private func toggleContainer(_ node: JSONNode) {
        preservePosition(of: node)
        if outline.isItemExpanded(node) { outline.collapseItem(node) } else { outline.expandItem(node) }
    }
}

@MainActor
private final class InspectorPanel: NSPanel {
    override func cancelOperation(_ sender: Any?) { close() }
}

@MainActor
final class JSONInspectorController: NSWindowController, NSWindowDelegate {
    let inspector = JSONInspectorViewController()
    private(set) var loadTask: Task<Void, Never>?
    private var generation = UUID()
    private weak var parentWindow: NSWindow?

    init() {
        let panel = InspectorPanel(contentRect: NSRect(x: 0, y: 0, width: 920, height: 640), styleMask: [.titled, .closable, .resizable], backing: .buffered, defer: false)
        // NSPopover windows sit at the pop-up menu level. Match that level so
        // the inspector remains visible above the dropdown when opened from a row.
        panel.isReleasedWhenClosed = false; panel.level = .popUpMenu
        panel.minSize = NSSize(width: 460, height: 320)
        super.init(window: panel)
        panel.contentViewController = inspector; panel.delegate = self
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    func show(record: RequestRecord, loader: @escaping @Sendable () async throws -> Data, relativeTo source: NSWindow?) {
        loadTask?.cancel()
        let token = UUID(); generation = token
        inspector.reset()
        if let source, let window, parentWindow !== source {
            parentWindow?.removeChildWindow(window)
            source.addChildWindow(window, ordered: .above)
            parentWindow = source
        }
        window?.title = "Request JSON · " + record.id
        if window?.isVisible != true, let screen = source?.screen ?? NSScreen.main {
            let area = screen.visibleFrame
            let size = NSSize(width: min(980, area.width - 40), height: min(700, area.height - 40))
            window?.setFrame(NSRect(x: area.midX - size.width / 2, y: area.midY - size.height / 2, width: size.width, height: size.height), display: false)
        }
        showWindow(nil); window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        loadTask = Task { [weak self] in
            do {
                let data = try await loader()
                try Task.checkCancellation()
                let document: JSONDocument
                do { document = try await inBackground { try JSONDocument(data: data) } }
                catch is CancellationError { return }
                catch {
                    guard let self, self.generation == token, !Task.isCancelled else { return }
                    self.inspector.reset(message: "GoModel returned invalid JSON.")
                    return
                }
                guard let self, self.generation == token, !Task.isCancelled else { return }
                self.inspector.display(document)
            } catch {
                guard let self, self.generation == token, !Task.isCancelled else { return }
                if case APIError.http(404) = error {
                    self.inspector.reset(message: record.terminal ? "Full request JSON is not available on GoModel." : "Full request JSON is not available yet.\nReopen this request after it completes.")
                } else {
                    self.inspector.reset(message: "Could not load full request JSON.\n" + Monitor.safeError(error))
                }
            }
        }
    }
    func windowWillClose(_ notification: Notification) {
        if let window, let parentWindow { parentWindow.removeChildWindow(window) }
        parentWindow = nil
        generation = UUID(); loadTask?.cancel(); loadTask = nil
        inspector.reset(message: "")
    }
}
