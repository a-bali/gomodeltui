import AppKit

/// Resizes the public NSPopover content size without changing its window style
/// or its transient dismissal behavior.
@MainActor
final class ResizeGrip: NSView {
    let left: Bool
    var resize: ((NSSize, Bool) -> Void)?
    private var dragOrigin: NSPoint?
    private var originalSize = NSSize.zero

    init(left: Bool) {
        self.left = left
        super.init(frame: .zero)
        toolTip = "Drag to resize the dropdown"
        setAccessibilityElement(true)
        setAccessibilityRole(.growArea)
        setAccessibilityLabel("Resize dropdown")
        setAccessibilityHelp("Drag to resize, or use the arrow keys when focused.")
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    override var acceptsFirstResponder: Bool { true }

    override func draw(_ dirtyRect: NSRect) {
        NSColor.tertiaryLabelColor.setStroke()
        let path = NSBezierPath()
        path.lineWidth = 1
        for inset in [4.0, 8, 12] {
            path.move(to: NSPoint(x: left ? 3 : bounds.width - 3, y: inset))
            path.line(to: NSPoint(x: left ? inset : bounds.width - inset, y: 3))
        }
        path.stroke()
    }

    override func resetCursorRects() {
        super.resetCursorRects()
        if #available(macOS 15, *) {
            addCursorRect(bounds, cursor: .frameResize(position: left ? .bottomLeft : .bottomRight, directions: .all))
        } else { addCursorRect(bounds, cursor: .crosshair) }
    }
    override func mouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        dragOrigin = NSEvent.mouseLocation
        originalSize = superview?.bounds.size ?? .zero
    }
    override func mouseDragged(with event: NSEvent) { resizeFromPointer(finished: false) }
    override func mouseUp(with event: NSEvent) {
        resizeFromPointer(finished: true)
        dragOrigin = nil
    }
    private func resizeFromPointer(finished: Bool) {
        guard let origin = dragOrigin else { return }
        let point = NSEvent.mouseLocation
        resize?(NSSize(width: originalSize.width + (point.x - origin.x) * (left ? -1 : 1),
                       height: originalSize.height + origin.y - point.y), finished)
    }
    override func keyDown(with event: NSEvent) {
        let step: CGFloat = event.modifierFlags.contains(.shift) ? 40 : 10
        switch event.keyCode {
        case 123: adjust(width: -step, height: 0)
        case 124: adjust(width: step, height: 0)
        case 125: adjust(width: 0, height: step)
        case 126: adjust(width: 0, height: -step)
        default: super.keyDown(with: event)
        }
    }
    private func adjust(width: CGFloat, height: CGFloat) {
        guard let size = superview?.bounds.size else { return }
        resize?(NSSize(width: size.width + width, height: size.height + height), true)
    }
    override func accessibilityPerformIncrement() -> Bool { adjust(width: 20, height: 20); return true }
    override func accessibilityPerformDecrement() -> Bool { adjust(width: -20, height: -20); return true }
}
