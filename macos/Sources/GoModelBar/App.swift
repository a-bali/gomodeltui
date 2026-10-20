import AppKit
import GoModelCore

private struct UsageStatusEntry {
    let id: String
    var name: String
    var quota: QuotaProvider?
    var balance: BalanceProvider?
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate, NSPopoverDelegate {
    private let monitor = Monitor()
    private let usage = UsageManager()
    private var statusItem: NSStatusItem!
    private var panel: PanelController!
    private let popover = NSPopover()
    private var timer: Timer?
    private var wakeObserver: NSObjectProtocol?
    private var screenObserver: NSObjectProtocol?
    private var configurationPath = ""
    private var iconBuckets: [ChartBucket]?
    private var iconConnected: Bool?
    private var usageStatusItems: [String: NSStatusItem] = [:]
    private let visibleUsageProvidersKey = "visibleUsageProviders"

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        guard let button = statusItem.button else { return }
        button.target = self
        button.action = #selector(statusClicked)
        button.sendAction(on: [.leftMouseUp, .rightMouseUp])
        button.setAccessibilityLabel("GoModel request activity")
        panel = PanelController(monitor: monitor, usage: usage)
        panel.resizeRequested = { [weak self] size, finished in self?.resizePanel(to: size, save: finished) }
        popover.contentViewController = panel
        popover.behavior = .semitransient
        popover.animates = false
        popover.delegate = self
        monitor.changed = { [weak self] in self?.refresh() }
        usage.changed = { [weak self] in self?.refresh() }
        let arguments = ProcessInfo.processInfo.arguments
        let environment = ProcessInfo.processInfo.environment
        var path = environment["GOMODELTUI_CONFIG"].flatMap { $0.isEmpty ? nil : $0 }
            ?? UserDefaults.standard.string(forKey: "configurationPath") ?? Configuration.defaultPath()
        if let index = arguments.firstIndex(of: "--config"), arguments.indices.contains(index + 1) { path = arguments[index + 1] }
        configurationPath = NSString(string: path).expandingTildeInPath
        if arguments.contains("--demo") { monitor.loadDemo() }
        else {
            monitor.configure(path: path)
        }
        if let index = arguments.firstIndex(of: "--render-preview"), arguments.indices.contains(index + 1) {
            renderPreview(to: arguments[index + 1])
            NSApp.terminate(nil)
            return
        }
        usage.configure(monitor.configuration)
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.monitor.tick()
                self?.refresh()
            }
        }
        if let timer { RunLoop.main.add(timer, forMode: .common) }
        wakeObserver = NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.didWakeNotification,
            object: nil, queue: .main) { [weak self] _ in MainActor.assumeIsolated { self?.monitor.reconnect() } }
        screenObserver = NotificationCenter.default.addObserver(forName: NSApplication.didChangeScreenParametersNotification,
            object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated {
                    guard let self, self.popover.isShown else { return }
                    self.popover.performClose(nil)
                }
            }
        refresh()
        if arguments.contains("--show") || arguments.contains("--demo") { togglePanel() }
    }

    private func renderPreview(to path: String) {
        panel.loadViewIfNeeded()
        panel.view.setFrameSize(NSSize(width: 1024, height: 680))
        panel.refresh()
        panel.view.layoutSubtreeIfNeeded()
        panel.view.appearance = NSAppearance(named: .darkAqua)
        panel.view.wantsLayer = true
        panel.view.effectiveAppearance.performAsCurrentDrawingAppearance {
            panel.view.layer?.backgroundColor = NSColor.windowBackgroundColor.cgColor
        }
        guard let bitmap = panel.view.bitmapImageRepForCachingDisplay(in: panel.view.bounds) else {
            fputs("Could not create preview bitmap.\n", stderr)
            return
        }
        panel.view.cacheDisplay(in: panel.view.bounds, to: bitmap)
        guard let data = bitmap.representation(using: .png, properties: [:]) else {
            fputs("Could not encode preview PNG.\n", stderr)
            return
        }
        do { try data.write(to: URL(fileURLWithPath: path)) }
        catch { fputs("Could not write preview: \(error.localizedDescription)\n", stderr) }
    }

    func applicationWillTerminate(_ notification: Notification) {
        timer?.invalidate()
        monitor.stop()
        if let wakeObserver { NSWorkspace.shared.notificationCenter.removeObserver(wakeObserver) }
        if let screenObserver { NotificationCenter.default.removeObserver(screenObserver) }
    }

    private func refresh() {
        let now = monitor.now
        let end = ActivityStore.alignedEnd(now: now, window: 3600, count: 18)
        let buckets = monitor.activity.buckets(now: end, window: 3600, count: 18, through: now, includeMCP: monitor.includeMCP)
        let success = buckets.reduce(0) { $0 + $1.success }
        let failed = buckets.reduce(0) { $0 + $1.failed }
        if iconBuckets != buckets || iconConnected != monitor.connected {
            statusItem?.button?.image = ChartDrawing.statusImage(buckets: buckets, connected: monitor.connected)
            iconBuckets = buckets; iconConnected = monitor.connected
        }
        let description = "Last hour: \(success + failed) requests · \(success) successful · \(failed) failed"
        statusItem?.button?.toolTip = description + "\n" + [monitor.connectionStatus, monitor.historyStatus].filter { !$0.isEmpty }.joined(separator: " · ")
        statusItem?.button?.setAccessibilityValue(description)
        refreshUsageStatusItems()
        if popover.isShown { panel.refresh() }
    }

    @objc private func togglePanel() {
        if popover.isShown { popover.performClose(nil); return }
        guard let button = statusItem.button, let screen = button.window?.screen ?? NSScreen.main else { return }
        let usable = screen.visibleFrame
        let preferences = UserDefaults.standard
        let width = preferences.double(forKey: "dropdownWidth")
        let height = preferences.double(forKey: "dropdownHeight")
        popover.contentSize = constrainedSize(NSSize(width: width > 0 ? width : usable.width * 0.5,
            height: height > 0 ? height : max(390, usable.height * 0.65)), on: screen)
        panel.refresh()
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        popover.contentViewController?.view.window?.makeKey()
    }

    func popoverDidShow(_ notification: Notification) {
        panel.refresh()
    }

    private func constrainedSize(_ size: NSSize, on screen: NSScreen) -> NSSize {
        let area = screen.visibleFrame
        let maximum = NSSize(width: max(1, area.width - 24), height: max(1, area.height - 32))
        return NSSize(width: min(maximum.width, max(min(640, maximum.width), size.width)),
                      height: min(maximum.height, max(min(390, maximum.height), size.height)))
    }

    private func resizePanel(to size: NSSize, save: Bool) {
        guard popover.isShown, size.width.isFinite, size.height.isFinite,
              let screen = statusItem.button?.window?.screen ?? NSScreen.main else { return }
        let clamped = constrainedSize(size, on: screen)
        popover.contentSize = clamped
        panel.view.layoutSubtreeIfNeeded()
        if save {
            UserDefaults.standard.set(clamped.width, forKey: "dropdownWidth")
            UserDefaults.standard.set(clamped.height, forKey: "dropdownHeight")
        }
    }

    @objc private func statusClicked() {
        if NSApp.currentEvent?.type == .rightMouseUp || NSApp.currentEvent?.modifierFlags.contains(.control) == true {
            popover.performClose(nil)
            guard let button = statusItem.button else { return }
            let menu = NSMenu()
            let configuration = NSMenuItem(title: "Configuration", action: #selector(editConfiguration), keyEquivalent: "")
            configuration.target = self
            menu.addItem(configuration)
            let usageOptions = NSMenu()
            let entries = usageStatusEntries()
            let selected = Set(UserDefaults.standard.stringArray(forKey: visibleUsageProvidersKey) ?? [])
            if entries.isEmpty {
                let empty = NSMenuItem(title: "No providers configured", action: nil, keyEquivalent: "")
                empty.isEnabled = false
                usageOptions.addItem(empty)
            } else {
                for entry in entries {
                    let item = NSMenuItem(title: entry.name, action: #selector(toggleUsageProvider(_:)), keyEquivalent: "")
                    item.target = self
                    item.representedObject = entry.id
                    item.state = selected.contains(entry.id) ? .on : .off
                    usageOptions.addItem(item)
                }
            }
            let usageItem = NSMenuItem(title: "Usage Icons", action: nil, keyEquivalent: "")
            usageItem.submenu = usageOptions
            menu.addItem(usageItem)
            menu.addItem(.separator())
            for (title, action) in [("Reconnect", #selector(reconnect)), ("Quit GoModel", #selector(quit))] {
                let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
                item.target = self; menu.addItem(item)
            }
            menu.popUp(positioning: nil, at: NSPoint(x: 0, y: button.bounds.minY), in: button)
        } else { togglePanel() }
    }

    @objc private func reconnect() { monitor.reconnect() }
    @objc private func quit() { NSApp.terminate(nil) }

    @objc private func toggleUsageProvider(_ sender: NSMenuItem) {
        guard let id = sender.representedObject as? String else { return }
        var selected = Set(UserDefaults.standard.stringArray(forKey: visibleUsageProvidersKey) ?? [])
        if selected.contains(id) { selected.remove(id) } else { selected.insert(id) }
        UserDefaults.standard.set(Array(selected).sorted(), forKey: visibleUsageProvidersKey)
        refreshUsageStatusItems()
    }

    private func usageStatusEntries() -> [UsageStatusEntry] {
        var entries: [String: UsageStatusEntry] = [:]
        for quota in usage.presentation.quotas {
            var entry = entries[quota.id] ?? UsageStatusEntry(id: quota.id, name: quota.name, quota: nil, balance: nil)
            entry.name = quota.name
            entry.quota = quota
            entries[quota.id] = entry
        }
        for balance in usage.presentation.balances {
            var entry = entries[balance.id] ?? UsageStatusEntry(id: balance.id, name: balance.name, quota: nil, balance: nil)
            entry.name = balance.name
            entry.balance = balance
            entries[balance.id] = entry
        }
        return entries.values.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
    }

    private func refreshUsageStatusItems() {
        let entries = usageStatusEntries()
        let selected = Set(UserDefaults.standard.stringArray(forKey: visibleUsageProvidersKey) ?? [])
        let visibleIDs = Set(entries.map(\.id)).intersection(selected)
        for id in Array(usageStatusItems.keys) where !visibleIDs.contains(id) {
            if let item = usageStatusItems.removeValue(forKey: id) { NSStatusBar.system.removeStatusItem(item) }
        }
        for entry in entries where visibleIDs.contains(entry.id) {
            let item = usageStatusItems[entry.id] ?? NSStatusBar.system.statusItem(withLength: 32)
            usageStatusItems[entry.id] = item
            guard let button = item.button else { continue }
            button.image = UsageStatusIcon.image(for: entry)
            button.imagePosition = .imageOnly
            button.target = self
            button.action = #selector(togglePanel)
            button.sendAction(on: [.leftMouseUp])
            button.toolTip = UsageStatusIcon.tooltip(for: entry)
            button.setAccessibilityLabel("\(entry.name) provider usage")
            button.setAccessibilityValue(UsageStatusIcon.tooltip(for: entry))
        }
    }

    @objc private func editConfiguration() {
        popover.performClose(nil)
        let url = URL(fileURLWithPath: configurationPath).standardizedFileURL
        guard FileManager.default.fileExists(atPath: url.path) else {
            let alert = NSAlert()
            alert.messageText = "Configuration file not found"
            alert.informativeText = url.path
            alert.alertStyle = .warning
            alert.runModal()
            return
        }
        NSWorkspace.shared.open(url)
    }
}

@MainActor
private enum UsageStatusIcon {
    private static let size = NSSize(width: 32, height: 18)

    static func image(for entry: UsageStatusEntry) -> NSImage {
        let image = NSImage(size: size)
        image.lockFocus()
        drawName(entry.name)
        if let quota = entry.quota {
            let barsArea = entry.balance == nil
                ? NSRect(x: 2, y: 1, width: size.width - 4, height: 9)
                : NSRect(x: 2, y: 1, width: size.width - 4, height: 5)
            drawBars(quota.quotas, in: barsArea)
        }
        if let balance = entry.balance {
            drawBalance(balance, in: entry.quota == nil
                ? NSRect(x: 1, y: 1, width: size.width - 2, height: 11)
                : NSRect(x: 1, y: 6, width: size.width - 2, height: 6))
        }
        image.unlockFocus()
        image.isTemplate = false
        return image
    }

    static func tooltip(for entry: UsageStatusEntry) -> String {
        var details = [entry.name]
        if let balance = entry.balance {
            details.append(balance.balance ?? balance.error ?? "Balance unavailable")
        }
        if let quota = entry.quota {
            details += quota.quotas.map { "\($0.label): \(Int($0.availablePercent.rounded()))% available" }
        }
        return details.joined(separator: " · ")
    }

    private static func drawBars(_ quotas: [UsageQuota], in area: NSRect) {
        let items = Array(quotas.sorted {
            let lhs = quotaOrder($0)
            let rhs = quotaOrder($1)
            if lhs != rhs { return lhs < rhs }
            switch ($0.resetsAt, $1.resetsAt) {
            case let (lhs?, rhs?): return lhs < rhs
            case (_?, nil): return true
            case (nil, _?): return false
            case (nil, nil): return $0.label.localizedCaseInsensitiveCompare($1.label) == .orderedAscending
            }
        }.prefix(3))
        guard !items.isEmpty else { return }
        let barHeight: CGFloat = items.count == 3 ? (area.height < 7 ? 1 : 1.9) : 2.3
        let spacing: CGFloat = items.count == 3 ? (area.height < 7 ? 0.5 : 1.35) : 1.8
        let totalHeight = CGFloat(items.count) * barHeight + CGFloat(items.count - 1) * spacing
        let firstY = area.midY - totalHeight / 2
        for (index, quota) in items.enumerated() {
            let y = firstY + CGFloat(items.count - index - 1) * (barHeight + spacing)
            let background = NSRect(x: area.minX, y: y, width: area.width, height: barHeight)
            NSColor.quaternaryLabelColor.setFill()
            NSBezierPath(roundedRect: background, xRadius: 1.1, yRadius: 1.1).fill()
            let fraction = min(1, max(0, quota.availablePercent / 100))
            if fraction > 0 {
                let fill = NSRect(x: area.minX, y: y, width: area.width * fraction, height: barHeight)
                tint(for: quota.availablePercent).setFill()
                NSBezierPath(roundedRect: fill, xRadius: 1.1, yRadius: 1.1).fill()
            }
        }
    }

    private static func drawBalance(_ balance: BalanceProvider, in area: NSRect) {
        let pillColor: NSColor
        if balance.error != nil { pillColor = NSColor(srgbRed: 0.62, green: 0.12, blue: 0.12, alpha: 1) }
        else if let usd = balance.balanceUSD {
            pillColor = usd < 1 ? NSColor(srgbRed: 0.62, green: 0.12, blue: 0.12, alpha: 1)
                : usd <= 5 ? NSColor(srgbRed: 0.55, green: 0.34, blue: 0.02, alpha: 1)
                : NSColor(srgbRed: 0.08, green: 0.39, blue: 0.21, alpha: 1)
        }
        else { pillColor = .tertiaryLabelColor }
        let text: String
        if balance.error != nil { text = "!" }
        else if let usd = balance.balanceUSD { text = String(format: "%.2f", usd) }
        else { text = (balance.balance ?? "—").replacingOccurrences(of: "$", with: "").replacingOccurrences(of: "US", with: "").trimmingCharacters(in: .whitespaces) }
        var fontSize: CGFloat = area.height >= 10 ? 10 : 6
        var attributes: [NSAttributedString.Key: Any] = [:]
        var measured = NSSize.zero
        repeat {
            attributes = [.font: NSFont.monospacedSystemFont(ofSize: fontSize, weight: .semibold), .foregroundColor: NSColor.white]
            measured = (text as NSString).size(withAttributes: attributes)
            if measured.width <= area.width - 6 || fontSize <= 5 { break }
            fontSize -= 0.5
        } while true
        let textSize = NSSize(width: measured.width, height: min(measured.height, area.height - 2))
        let rect = NSRect(x: area.midX - textSize.width / 2, y: area.midY - textSize.height / 2,
                          width: textSize.width, height: textSize.height)
        let pill = rect.insetBy(dx: -2, dy: -1)
        pillColor.setFill()
        NSBezierPath(roundedRect: pill, xRadius: 2.5, yRadius: 2.5).fill()
        (text as NSString).draw(in: rect, withAttributes: attributes)
    }

    private static func drawName(_ name: String) {
        let shortName: String
        switch name.lowercased() {
        case "opencode go": shortName = "OC Go"
        case "command code": shortName = "Command"
        case "chatgpt": shortName = "ChatGPT"
        case "openrouter": shortName = "OpenRtr"
        case "deepseek": shortName = "DeepSeek"
        case "xiaomi mimo": shortName = "MiMo"
        default: shortName = name
        }
        let font = NSFont.systemFont(ofSize: 5.5, weight: .medium)
        let attributes: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: NSColor.labelColor]
        let width = (shortName as NSString).size(withAttributes: attributes).width
        (shortName as NSString).draw(at: NSPoint(x: (size.width - width) / 2, y: size.height - 7), withAttributes: attributes)
    }

    private static func tint(for available: Double) -> NSColor {
        available <= 25 ? .systemRed : available <= 50 ? .systemOrange : .systemGreen
    }

    private static func quotaOrder(_ quota: UsageQuota) -> Int {
        switch quota.label.lowercased() {
        case "5h", "5 h", "5-hour", "5 hour": return 0
        case "w", "week", "weekly": return 1
        case "m", "month", "monthly": return 2
        default: return 3
        }
    }
}

@main
enum GoModelBarApp {
    @MainActor
    static func main() {
        if ProcessInfo.processInfo.arguments.contains("--help") {
            print("GoModelBar [--config PATH] [--demo] [--show] [--render-preview PATH]\nRead-only GoModel menu bar monitor. Configuration: ~/.config/gomodeltui/config.yaml")
            return
        }
        let application = NSApplication.shared
        let delegate = AppDelegate()
        application.delegate = delegate
        withExtendedLifetime(delegate) { application.run() }
    }
}
