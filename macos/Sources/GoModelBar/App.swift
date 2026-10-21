import AppKit
import GoModelCore
import ServiceManagement

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
    private var dismissMonitors: [Any] = []
    private var usageTimer: Timer?
    private var lastPopoverClose: Date?
    private var aboutWindow: NSWindow?
    private let statusTooltip = StatusTooltip()
    private let visibleUsageProvidersKey = "visibleUsageProviders"

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        guard let button = statusItem.button else { return }
        button.target = self
        button.action = #selector(statusClicked)
        button.sendAction(on: [.leftMouseUp, .rightMouseUp])
        button.setAccessibilityLabel("GoModel request activity")
        statusTooltip.isBlocked = { [weak self] in self?.popover.isShown == true }
        panel = PanelController(monitor: monitor, usage: usage)
        panel.resizeRequested = { [weak self] size, finished in self?.resizePanel(to: size, save: finished) }
        panel.dismissRequested = { [weak self] in self?.popover.performClose(nil) }
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
        // Provider quota/balance data is only fetched on launch and on manual
        // refresh otherwise; keep the menu bar icons fresh in the background.
        usageTimer = Timer.scheduledTimer(withTimeInterval: 300, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.usage.refresh() }
        }
        if let usageTimer { RunLoop.main.add(usageTimer, forMode: .common) }
        wakeObserver = NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.didWakeNotification,
            object: nil, queue: .main) { [weak self] _ in MainActor.assumeIsolated {
                self?.monitor.reconnect()
                self?.usage.refresh()
            } }
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
        removeDismissMonitors()
        timer?.invalidate()
        usageTimer?.invalidate()
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
        if let button = statusItem?.button {
            statusTooltip.setText(description + "\n" + [monitor.connectionStatus, monitor.historyStatus].filter { !$0.isEmpty }.joined(separator: " · "), for: button)
        }
        statusItem?.button?.setAccessibilityValue(description)
        refreshUsageStatusItems()
        if popover.isShown { panel.refresh() }
    }

    @objc private func togglePanel() {
        statusTooltip.suppressHover()
        if popover.isShown { popover.performClose(nil); return }
        // A click on a status item can close the popover before the button
        // action runs on mouse-up; don't immediately reopen it in that case.
        if let closedAt = lastPopoverClose, Date().timeIntervalSince(closedAt) < 0.3 { return }
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
        installDismissMonitors()
    }

    func popoverDidClose(_ notification: Notification) {
        lastPopoverClose = Date()
        removeDismissMonitors()
    }

    /// `NSPopover`'s transient behaviors do not reliably close an accessory
    /// app's popover when it never became the active app, so dismiss explicitly
    /// on any mouse-down outside the popover. The status item's own window is
    /// excluded so its action can keep toggling the panel.
    private func installDismissMonitors() {
        removeDismissMonitors()
        let mask: NSEvent.EventTypeMask = [.leftMouseDown, .rightMouseDown, .otherMouseDown]
        if let local = NSEvent.addLocalMonitorForEvents(matching: mask, handler: { [weak self] event in
            MainActor.assumeIsolated {
                guard let self, self.popover.isShown else { return }
                let popoverWindow = self.popover.contentViewController?.view.window
                guard event.window !== popoverWindow, !self.clickedStatusItem() else { return }
                self.popover.performClose(nil)
            }
            return event
        }) {
            dismissMonitors.append(local)
        }
        if let global = NSEvent.addGlobalMonitorForEvents(matching: mask, handler: { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self, self.popover.isShown else { return }
                self.popover.performClose(nil)
            }
        }) {
            dismissMonitors.append(global)
        }
    }

    /// Status item clicks must reach their own action so it can toggle the
    /// popover. Their events may have no window or a private status-bar window,
    /// so test the click location against every status item's button instead.
    private func clickedStatusItem() -> Bool {
        let point = NSEvent.mouseLocation
        var buttons: [NSStatusBarButton] = []
        if let button = statusItem.button { buttons.append(button) }
        buttons.append(contentsOf: usageStatusItems.values.compactMap(\.button))
        return buttons.contains { button in
            guard let window = button.window, window.isVisible else { return false }
            let inWindow = window.convertPoint(fromScreen: point)
            return button.bounds.contains(button.convert(inWindow, from: nil))
        }
    }

    private func removeDismissMonitors() {
        for monitor in dismissMonitors { NSEvent.removeMonitor(monitor) }
        dismissMonitors.removeAll()
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
        statusTooltip.suppressHover()
        if NSApp.currentEvent?.type == .rightMouseUp || NSApp.currentEvent?.modifierFlags.contains(.control) == true {
            guard let button = statusItem.button else { return }
            showContextMenu(for: button)
        } else { togglePanel() }
    }

    @objc private func providerClicked(_ sender: NSStatusBarButton) {
        statusTooltip.suppressHover()
        if NSApp.currentEvent?.type == .rightMouseUp || NSApp.currentEvent?.modifierFlags.contains(.control) == true {
            showContextMenu(for: sender)
        } else { togglePanel() }
    }

    private func showContextMenu(for button: NSStatusBarButton) {
        popover.performClose(nil)
        let menu = NSMenu()
        let about = NSMenuItem(title: "About GoModelBar", action: #selector(showAbout), keyEquivalent: "")
        about.target = self
        menu.addItem(about)
        menu.addItem(.separator())
        let configuration = NSMenuItem(title: "Configuration", action: #selector(editConfiguration), keyEquivalent: "")
        configuration.target = self
        menu.addItem(configuration)
        let loginItem = NSMenuItem(title: "Start at login", action: #selector(toggleStartAtLogin(_:)), keyEquivalent: "")
        loginItem.target = self
        switch SMAppService.mainApp.status {
        case .enabled:
            loginItem.state = .on
        case .requiresApproval:
            loginItem.state = .mixed
        case .notRegistered, .notFound:
            loginItem.state = .off
        @unknown default:
            loginItem.state = .off
        }
        menu.addItem(loginItem)
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
        let usageItem = NSMenuItem(title: "Usage icons", action: nil, keyEquivalent: "")
        usageItem.submenu = usageOptions
        menu.addItem(usageItem)
        menu.addItem(.separator())
        for (title, action) in [("Reconnect", #selector(reconnect)), ("Quit GoModel", #selector(quit))] {
            let item = NSMenuItem(title: title, action: action, keyEquivalent: "")
            item.target = self; menu.addItem(item)
        }
        guard let event = NSApp.currentEvent else { return }
        NSMenu.popUpContextMenu(menu, with: event, for: button)
    }

    @objc private func reconnect() { monitor.reconnect() }
    @objc private func quit() { NSApp.terminate(nil) }

    @objc private func toggleStartAtLogin(_ sender: NSMenuItem) {
        let service = SMAppService.mainApp
        do {
            switch service.status {
            case .enabled:
                try service.unregister()
            case .requiresApproval:
                SMAppService.openSystemSettingsLoginItems()
            case .notRegistered, .notFound:
                try service.register()
                if service.status == .requiresApproval {
                    SMAppService.openSystemSettingsLoginItems()
                }
            @unknown default:
                try service.register()
            }
        } catch {
            NSApp.presentError(error)
        }
        switch service.status {
        case .enabled: sender.state = .on
        case .requiresApproval: sender.state = .mixed
        default: sender.state = .off
        }
    }

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
            guard let item = usageStatusItems.removeValue(forKey: id) else { continue }
            statusTooltip.untrack(item.button)
            NSStatusBar.system.removeStatusItem(item)
        }
        for entry in entries where visibleIDs.contains(entry.id) {
            let item = usageStatusItems[entry.id] ?? NSStatusBar.system.statusItem(withLength: 32)
            usageStatusItems[entry.id] = item
            guard let button = item.button else { continue }
            button.image = UsageStatusIcon.image(for: entry)
            button.imagePosition = .imageOnly
            button.target = self
            button.action = #selector(providerClicked(_:))
            button.sendAction(on: [.leftMouseUp, .rightMouseUp])
            statusTooltip.setText(UsageStatusIcon.tooltip(for: entry,
                refreshedAt: usage.presentation.updatedAt,
                refreshing: usage.presentation.isRefreshing), for: button)
            button.setAccessibilityLabel("\(entry.name) provider usage")
            button.setAccessibilityValue(UsageStatusIcon.tooltip(for: entry,
                refreshedAt: usage.presentation.updatedAt,
                refreshing: usage.presentation.isRefreshing))
        }
    }

    @objc private func showAbout() {
        popover.performClose(nil)
        let repository = URL(string: "https://github.com/a-bali/gomodeltui")!
        let sourceVersion = (Bundle.main.object(forInfoDictionaryKey: "GoModelSourceVersion") as? String)
            .flatMap { $0.isEmpty ? nil : $0 }
        let bundleVersion = (Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String)
            .flatMap { $0.isEmpty || $0 == "0.0.0" ? nil : $0 }
        let version = (sourceVersion ?? bundleVersion)
            .map { $0.hasPrefix("v") ? String($0.dropFirst()) : $0 } ?? "development"
        NSApp.activate(ignoringOtherApps: true)
        if let aboutWindow {
            aboutWindow.makeKeyAndOrderFront(nil)
            return
        }
        let window = makeAboutWindow(version: version, repository: repository)
        aboutWindow = window
        window.makeKeyAndOrderFront(nil)
    }

    private func makeAboutWindow(version: String, repository: URL) -> NSWindow {
        let window = AboutWindow(contentRect: NSRect(x: 0, y: 0, width: 340, height: 300),
                                 styleMask: [.titled, .closable], backing: .buffered, defer: false)
        window.title = "About GoModelBar"
        window.isReleasedWhenClosed = false

        let icon = NSImageView(image: NSApp.applicationIconImage)
        icon.imageScaling = .scaleProportionallyUpOrDown
        NSLayoutConstraint.activate([
            icon.widthAnchor.constraint(equalToConstant: 96),
            icon.heightAnchor.constraint(equalToConstant: 96),
        ])

        let name = NSTextField(labelWithString: "GoModelBar")
        name.font = .systemFont(ofSize: 20, weight: .semibold)
        name.alignment = .center

        let stack = NSStackView(views: [
            icon,
            name,
            aboutLabel("Version \(version)"),
            aboutLabel("Built \(Self.buildDateDescription())"),
            aboutLink(to: repository),
        ])
        stack.orientation = .vertical
        stack.alignment = .centerX
        stack.spacing = 6
        stack.setCustomSpacing(12, after: icon)
        stack.setCustomSpacing(12, after: name)
        stack.translatesAutoresizingMaskIntoConstraints = false

        let content = NSView()
        content.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: content.centerXAnchor),
            stack.topAnchor.constraint(equalTo: content.topAnchor, constant: 22),
            stack.bottomAnchor.constraint(lessThanOrEqualTo: content.bottomAnchor, constant: -22),
        ])
        window.contentView = content
        content.layoutSubtreeIfNeeded()
        window.setContentSize(NSSize(width: 340, height: stack.fittingSize.height + 44))
        window.center()
        return window
    }

    private func aboutLabel(_ text: String) -> NSTextField {
        let label = NSTextField(labelWithString: text)
        label.font = .systemFont(ofSize: NSFont.smallSystemFontSize)
        label.textColor = .secondaryLabelColor
        label.lineBreakMode = .byClipping
        label.usesSingleLineMode = true
        label.maximumNumberOfLines = 1
        return label
    }

    private func aboutLink(to url: URL) -> NSTextField {
        let label = NSTextField(labelWithString: "")
        label.isSelectable = true
        label.allowsEditingTextAttributes = true
        label.lineBreakMode = .byClipping
        label.usesSingleLineMode = true
        label.maximumNumberOfLines = 1
        label.attributedStringValue = NSAttributedString(string: url.absoluteString, attributes: [
            .link: url,
            .font: NSFont.systemFont(ofSize: NSFont.smallSystemFontSize),
            .foregroundColor: NSColor.linkColor,
            .underlineStyle: NSUnderlineStyle.single.rawValue,
        ])
        return label
    }

    private static func buildDateDescription() -> String {
        let date: Date?
        if let raw = Bundle.main.object(forInfoDictionaryKey: "GoModelBuildDate") as? String {
            date = ISO8601DateFormatter().date(from: raw)
        } else if let executable = Bundle.main.executableURL {
            date = try? executable.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate
        } else {
            date = nil
        }
        guard let date else { return "unknown" }
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        return formatter.string(from: date)
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

    static func tooltip(for entry: UsageStatusEntry, refreshedAt: Date? = nil,
                        refreshing: Bool = false) -> String {
        var lines = [entry.name]
        if let quota = entry.quota {
            lines += quota.quotas.map(quotaLine)
        }
        if let balance = entry.balance {
            if let value = balance.balance, !value.isEmpty {
                lines.append("Balance: \(value)")
            } else if let error = balance.error, !error.isEmpty {
                lines.append("Balance unavailable: \(error)")
            } else {
                lines.append("Balance unavailable")
            }
        }
        lines.append(refreshing ? "Refreshing…" : refreshedAgo(refreshedAt))
        return lines.joined(separator: "\n")
    }

    private static func refreshedAgo(_ date: Date?) -> String {
        guard let date else { return "Data not refreshed yet" }
        let seconds = max(0, Int(Date().timeIntervalSince(date)))
        if seconds < 60 { return "Data refreshed just now" }
        let minutes = seconds / 60
        if minutes < 60 { return "Data refreshed \(minutes)m ago" }
        let hours = minutes / 60
        if hours < 24 { return "Data refreshed \(hours)h \(minutes % 60)m ago" }
        return "Data refreshed \(hours / 24)d ago"
    }

    private static func quotaLine(_ quota: UsageQuota) -> String {
        let label = quota.label == "w" ? "weekly" : quota.label == "m" ? "monthly" : quota.label
        let available = min(100, max(0, quota.availablePercent))
        let percentage = String(format: "%.1f%% available", available)
        guard let reset = quota.resetsAt else {
            return "\(label): \(percentage) · reset time unavailable"
        }
        return "\(label): \(percentage) · resets in \(resetETA(reset))"
    }

    private static func resetETA(_ date: Date) -> String {
        let seconds = max(0, Int(date.timeIntervalSinceNow))
        let days = seconds / 86_400, hours = seconds % 86_400 / 3_600, minutes = seconds % 3_600 / 60
        if days > 0 { return "\(days)d \(hours)h" }
        if hours > 0 { return "\(hours)h \(minutes)m" }
        return "\(max(1, minutes))m"
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

@MainActor
private final class AboutWindow: NSWindow {
    override func cancelOperation(_ sender: Any?) { performClose(nil) }
}
