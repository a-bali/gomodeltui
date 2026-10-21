import AppKit

/// Custom hover tooltip for menu bar status items.
///
/// `NSStatusBarButton.toolTip` is unreliable for accessory apps that are rarely
/// active, and tracking areas on status bar buttons do not reliably receive
/// mouse-entered events either. This polls the cursor location instead and
/// shows a small panel beneath the hovered status item.
@MainActor
final class StatusTooltip {
    private static let maxWidth: CGFloat = 340
    private static let padding = NSSize(width: 22, height: 14)
    private static let dwell: TimeInterval = 0.25
    private static let pollInterval: TimeInterval = 0.12

    private let panel: NSPanel
    private let backdrop = NSVisualEffectView()
    private let label = NSTextField(wrappingLabelWithString: "")
    /// Optional external gate, e.g. while the dropdown is open.
    var isBlocked: (() -> Bool)?
    private var buttons: [ObjectIdentifier: NSStatusBarButton] = [:]
    private var texts: [ObjectIdentifier: String] = [:]
    private var timer: Timer?
    private var pendingShow: DispatchWorkItem?
    private var hoveredButton: NSStatusBarButton?
    private var suppressedButton: NSStatusBarButton?
    private weak var shownButton: NSStatusBarButton?

    init() {
        panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 220, height: 44),
                        styleMask: [.borderless, .nonactivatingPanel],
                        backing: .buffered, defer: false)
        panel.isFloatingPanel = true
        panel.level = .popUpMenu
        panel.backgroundColor = .clear
        panel.isOpaque = false
        panel.hasShadow = true
        panel.ignoresMouseEvents = true
        panel.hidesOnDeactivate = false
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .ignoresCycle, .stationary]
        backdrop.material = .popover
        backdrop.blendingMode = .behindWindow
        backdrop.state = .active
        backdrop.wantsLayer = true
        backdrop.layer?.cornerRadius = 8
        backdrop.layer?.masksToBounds = true
        label.font = .systemFont(ofSize: 12)
        label.textColor = .labelColor
        label.maximumNumberOfLines = 0
        label.lineBreakMode = .byWordWrapping
        label.preferredMaxLayoutWidth = StatusTooltip.maxWidth
        backdrop.addSubview(label)
        panel.contentView = backdrop
    }

    /// Register (or update) the tooltip text for a status item button.
    func setText(_ text: String, for button: NSStatusBarButton) {
        let key = ObjectIdentifier(button)
        texts[key] = text
        buttons[key] = button
        // The custom tooltip replaces the unreliable standard one.
        if button.toolTip != nil { button.toolTip = nil }
        startPolling()
        if shownButton === button { show(for: button) }
    }

    /// Stop tracking a status item button that is about to be removed.
    func untrack(_ button: NSStatusBarButton?) {
        guard let button else { return }
        let key = ObjectIdentifier(button)
        buttons.removeValue(forKey: key)
        texts.removeValue(forKey: key)
        if hoveredButton === button { hoveredButton = nil }
        if suppressedButton === button { suppressedButton = nil }
        if shownButton === button { hide() }
    }

    /// Hide the tooltip and suppress it for the icon under the cursor until the
    /// pointer leaves that icon, so clicking a status item does not re-show it.
    func suppressHover() {
        suppressedButton = hoveredButton ?? button(at: NSEvent.mouseLocation)
        hide()
    }

    func hide() {
        pendingShow?.cancel()
        pendingShow = nil
        shownButton = nil
        panel.orderOut(nil)
    }

    private func startPolling() {
        guard timer == nil else { return }
        let timer = Timer(timeInterval: Self.pollInterval, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.poll() }
        }
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer
    }

    private func poll() {
        if isBlocked?() == true {
            hide()
            return
        }
        let hovered = button(at: NSEvent.mouseLocation)
        guard hovered !== hoveredButton else { return }
        hoveredButton = hovered
        pendingShow?.cancel()
        pendingShow = nil
        if hovered == nil {
            suppressedButton = nil
            hide()
            return
        }
        if hovered !== suppressedButton {
            suppressedButton = nil
            if let hovered { scheduleShow(for: hovered) }
        }
    }

    private func button(at point: NSPoint) -> NSStatusBarButton? {
        for button in buttons.values {
            guard let window = button.window, window.isVisible else { continue }
            let inWindow = window.convertPoint(fromScreen: point)
            if button.bounds.contains(button.convert(inWindow, from: nil)) { return button }
        }
        return nil
    }

    private func scheduleShow(for button: NSStatusBarButton) {
        guard texts[ObjectIdentifier(button)] != nil else { return }
        let work = DispatchWorkItem { [weak self, weak button] in
            guard let self, let button else { return }
            self.show(for: button)
        }
        pendingShow = work
        DispatchQueue.main.asyncAfter(deadline: .now() + Self.dwell, execute: work)
    }

    private func show(for button: NSStatusBarButton) {
        guard let text = texts[ObjectIdentifier(button)], !text.isEmpty,
              let window = button.window, window.isVisible else { return }
        pendingShow?.cancel()
        pendingShow = nil
        let textSize = layout(text)
        let size = NSSize(width: textSize.width + Self.padding.width,
                          height: textSize.height + Self.padding.height)
        let anchor = window.convertToScreen(button.convert(button.bounds, to: nil))
        let visible = (window.screen ?? NSScreen.main)?.visibleFrame ?? .zero
        var x = anchor.midX - size.width / 2
        x = min(max(x, visible.minX + 4), visible.maxX - size.width - 4)
        var y = anchor.minY - size.height - 6
        y = min(max(y, visible.minY + 4), visible.maxY - size.height - 4)

        label.frame = NSRect(x: Self.padding.width / 2,
                             y: Self.padding.height / 2,
                             width: textSize.width,
                             height: textSize.height)
        backdrop.frame = NSRect(origin: .zero, size: size)
        panel.setFrame(NSRect(x: x, y: y, width: size.width, height: size.height), display: false)
        panel.orderFrontRegardless()
        shownButton = button
    }

    /// Lays out the label and returns the size its cell needs. Measuring with
    /// `boundingRect` alone underestimates the height because the text field
    /// cell reserves horizontal padding, which silently clipped trailing lines.
    private func layout(_ text: String) -> NSSize {
        let attributes: [NSAttributedString.Key: Any] = [.font: label.font ?? .systemFont(ofSize: 12)]
        let wide = (text as NSString).boundingRect(with: NSSize(width: Self.maxWidth, height: .greatestFiniteMagnitude),
                                                   options: [.usesLineFragmentOrigin, .usesFontLeading],
                                                   attributes: attributes)
        let width = min(Self.maxWidth, ceil(wide.width) + 12)
        label.stringValue = text
        label.preferredMaxLayoutWidth = width
        let cell = label.cell?.cellSize(forBounds: NSRect(x: 0, y: 0, width: width,
                                                          height: .greatestFiniteMagnitude))
        let height = ceil(max(wide.height, cell?.height ?? 0)) + 2
        return NSSize(width: width, height: height)
    }
}
