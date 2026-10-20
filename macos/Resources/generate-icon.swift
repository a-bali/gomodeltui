import AppKit
import Foundation

func color(_ red: CGFloat, _ green: CGFloat, _ blue: CGFloat, alpha: CGFloat = 1) -> NSColor {
    NSColor(calibratedRed: red / 255, green: green / 255, blue: blue / 255, alpha: alpha)
}

func drawIcon(size: Int) throws -> Data {
    guard let bitmap = NSBitmapImageRep(
        bitmapDataPlanes: nil,
        pixelsWide: size,
        pixelsHigh: size,
        bitsPerSample: 8,
        samplesPerPixel: 4,
        hasAlpha: true,
        isPlanar: false,
        colorSpaceName: .deviceRGB,
        bytesPerRow: 0,
        bitsPerPixel: 0
    ), let context = NSGraphicsContext(bitmapImageRep: bitmap) else {
        throw NSError(domain: "GoModelIcon", code: 1, userInfo: [NSLocalizedDescriptionKey: "Could not create icon bitmap"])
    }

    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = context
    context.imageInterpolation = .high
    context.cgContext.scaleBy(x: CGFloat(size) / 1024, y: CGFloat(size) / 1024)
    NSColor.clear.setFill()
    NSRect(x: 0, y: 0, width: 1024, height: 1024).fill()

    let tile = NSRect(x: 40, y: 40, width: 944, height: 944)
    let tilePath = NSBezierPath(roundedRect: tile, xRadius: 220, yRadius: 220)
    tilePath.addClip()
    NSGradient(colors: [color(48, 73, 143), color(28, 39, 76)])!.draw(in: tile, angle: 45)

    let baseline = CGFloat(220)
    let barWidth = CGFloat(104)
    let bars: [(CGFloat, CGFloat)] = [(208, 300), (374, 515), (540, 405), (706, 625)]
    for (index, bar) in bars.enumerated() {
        let top = baseline + bar.1
        let greenTop = index == 2 ? top - 76 : top
        let greenRect = NSRect(x: bar.0, y: baseline, width: barWidth, height: greenTop - baseline)
        color(70, 222, 162).setFill()
        NSBezierPath(roundedRect: greenRect, xRadius: 48, yRadius: 48).fill()
        if index == 2 {
            let redRect = NSRect(x: bar.0, y: top - 128, width: barWidth, height: 138)
            color(255, 101, 110).setFill()
            NSBezierPath(roundedRect: redRect, xRadius: 48, yRadius: 48).fill()
        }
    }

    let baselinePath = NSBezierPath()
    baselinePath.lineWidth = 12
    baselinePath.lineCapStyle = .round
    color(230, 238, 255, alpha: 0.62).setStroke()
    baselinePath.move(to: NSPoint(x: 168, y: baseline - 42))
    baselinePath.line(to: NSPoint(x: 852, y: baseline - 42))
    baselinePath.stroke()

    NSGraphicsContext.restoreGraphicsState()
    guard let data = bitmap.representation(using: .png, properties: [:]) else {
        throw NSError(domain: "GoModelIcon", code: 2, userInfo: [NSLocalizedDescriptionKey: "Could not encode icon PNG"])
    }
    return data
}

let iconset = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
try FileManager.default.createDirectory(at: iconset, withIntermediateDirectories: true)

let representations: [(String, Int)] = [
    ("icon_16x16.png", 16), ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32), ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128), ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256), ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512), ("icon_512x512@2x.png", 1024)
]

for (name, size) in representations {
    try drawIcon(size: size).write(to: iconset.appendingPathComponent(name), options: .atomic)
}
