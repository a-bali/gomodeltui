import AppKit

@MainActor
final class ChartDivider: NSView {
    var chartHeight: (() -> CGFloat)?
    var move: ((CGFloat, Bool) -> Void)?
    private var dragOrigin: CGFloat?
    private var originalHeight: CGFloat = 148

    override init(frame: NSRect) {
        super.init(frame: frame)
        toolTip = "Drag to resize the chart and logs. Double-click to reset."
        setAccessibilityElement(true)
        setAccessibilityRole(.splitter)
        setAccessibilityLabel("Chart and request log divider")
        setAccessibilityHelp("Drag up or down, or use the arrow keys when focused. Double-click to reset.")
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    override var acceptsFirstResponder: Bool { true }

    override func draw(_ dirtyRect: NSRect) {
        NSColor.separatorColor.setFill()
        NSRect(x: 0, y: floor(bounds.midY), width: bounds.width, height: 1).fill()
        NSColor.tertiaryLabelColor.setFill()
        NSBezierPath(roundedRect: NSRect(x: bounds.midX - 14, y: floor(bounds.midY) - 1,
                                       width: 28, height: 3), xRadius: 1.5, yRadius: 1.5).fill()
    }
    override func resetCursorRects() {
        super.resetCursorRects()
        addCursorRect(bounds, cursor: .resizeUpDown)
    }
    override func mouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        if event.clickCount == 2 { move?(148, true); dragOrigin = nil; return }
        dragOrigin = NSEvent.mouseLocation.y
        originalHeight = chartHeight?() ?? 148
    }
    override func mouseDragged(with event: NSEvent) { moveFromPointer(finished: false) }
    override func mouseUp(with event: NSEvent) {
        moveFromPointer(finished: true)
        dragOrigin = nil
    }
    private func moveFromPointer(finished: Bool) {
        guard let origin = dragOrigin else { return }
        move?(originalHeight + origin - NSEvent.mouseLocation.y, finished)
    }
    override func keyDown(with event: NSEvent) {
        let step: CGFloat = event.modifierFlags.contains(.shift) ? 40 : 10
        switch event.keyCode {
        case 125: adjust(step)
        case 126: adjust(-step)
        default: super.keyDown(with: event)
        }
    }
    private func adjust(_ delta: CGFloat) { move?((chartHeight?() ?? 148) + delta, true) }
    override func accessibilityPerformIncrement() -> Bool { adjust(20); return true }
    override func accessibilityPerformDecrement() -> Bool { adjust(-20); return true }
}
