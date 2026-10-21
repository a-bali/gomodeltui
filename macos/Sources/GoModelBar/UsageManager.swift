import Foundation
import GoModelCore

struct UsageQuota: Identifiable, Equatable, Sendable {
    let id: String
    let label: String
    let availablePercent: Double
    let resetsAt: Date?
}

struct QuotaProvider: Identifiable, Equatable, Sendable {
    let id: String
    let name: String
    let quotas: [UsageQuota]
}

struct BalanceProvider: Identifiable, Equatable, Sendable {
    let id: String
    let name: String
    let balance: String?
    let balanceUSD: Double?
    let error: String?
    let url: URL

    init(id: String, name: String, balance: String?, error: String?, url: URL, balanceUSD: Double? = nil) {
        self.id = id; self.name = name; self.balance = balance; self.error = error; self.url = url
        self.balanceUSD = balanceUSD
    }
}

struct UsagePresentation: Equatable, Sendable {
    var quotas: [QuotaProvider] = []
    var balances: [BalanceProvider] = []
    var updatedAt: Date?
    var isRefreshing = false
}

@MainActor
final class UsageManager {
    private(set) var presentation = UsagePresentation()
    var changed: (() -> Void)?
    private var configuration: Configuration?
    private var refreshTask: Task<Void, Never>?

    func configure(_ configuration: Configuration?) {
        self.configuration = configuration
        refresh()
    }

    func refresh() {
        refreshTask?.cancel()
        guard let configuration else {
            presentation = UsagePresentation()
            changed?()
            return
        }
        let hasProvider = !configuration.openCodeAPIKey.isEmpty || !configuration.commandCodeCookie.isEmpty ||
            !configuration.codexAuthPath.isEmpty || !configuration.openRouterAPIKey.isEmpty ||
            !configuration.deepSeekAPIKey.isEmpty || !configuration.miMoCookie.isEmpty || !configuration.miMoAccountCookie.isEmpty
        guard hasProvider else {
            presentation = UsagePresentation()
            changed?()
            return
        }
        presentation.isRefreshing = true
        changed?()
        refreshTask = Task { [weak self] in
            let result = await Self.fetch(configuration)
            guard let self, !Task.isCancelled else { return }
            self.presentation = UsagePresentation(quotas: result.quotas, balances: result.balances,
                updatedAt: Date(), isRefreshing: false)
            self.changed?()
        }
    }

    private static func fetch(_ configuration: Configuration) async -> UsagePresentation {
        var result = UsagePresentation()
        await withTaskGroup(of: (String, [QuotaProvider], [BalanceProvider]).self) { group in
            if !configuration.openCodeAPIKey.isEmpty {
                group.addTask { await fetchOpenCode(configuration.openCodeAPIKey) }
            }
            if !configuration.commandCodeCookie.isEmpty {
                group.addTask { await fetchCommandCode(configuration.commandCodeCookie) }
            }
            if !configuration.codexAuthPath.isEmpty {
                group.addTask { await fetchCodex(configuration.codexAuthPath) }
            }
            if !configuration.openRouterAPIKey.isEmpty {
                group.addTask { await fetchOpenRouter(configuration.openRouterAPIKey) }
            }
            if !configuration.deepSeekAPIKey.isEmpty {
                group.addTask { await fetchDeepSeek(configuration.deepSeekAPIKey) }
            }
            if !configuration.miMoCookie.isEmpty || !configuration.miMoAccountCookie.isEmpty {
                group.addTask { await fetchMiMo(configuration.miMoCookie, accountCookie: configuration.miMoAccountCookie) }
            }
            for await (_, quotas, balances) in group {
                result.quotas += quotas
                result.balances += balances
            }
        }
        result.quotas.sort { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
        result.balances.sort { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
        return result
    }

    private static func fetchOpenCode(_ key: String) async -> (String, [QuotaProvider], [BalanceProvider]) {
        let providerURL = URL(string: "https://opencode.ai/console")!
        let (data, error) = await requestJSON("https://opencode.ai/zen/go/v1/usage", bearer: key)
        guard let data, let root = object(data), let usage = root["usage"] as? [String: Any],
              let rolling = usage["rolling"] as? [String: Any] else {
            return ("OpenCode Go", [], [BalanceProvider(id: "opencode", name: "OpenCode Go", balance: nil,
                error: error ?? "Invalid usage response", url: providerURL)])
        }
        var windows: [UsageQuota] = []
        if let quota = quotaWindow(rolling, label: "5h", id: "opencode-5h") { windows.append(quota) }
        if let weekly = usage["weekly"] as? [String: Any], let quota = quotaWindow(weekly, label: "w", id: "opencode-weekly") { windows.append(quota) }
        if let monthly = usage["monthly"] as? [String: Any], let quota = quotaWindow(monthly, label: "m", id: "opencode-monthly") { windows.append(quota) }
        guard !windows.isEmpty else {
            return ("OpenCode Go", [], [BalanceProvider(id: "opencode", name: "OpenCode Go", balance: nil,
                error: "No quota windows returned", url: providerURL)])
        }
        return ("OpenCode Go", [QuotaProvider(id: "opencode", name: "OpenCode Go", quotas: windows)], [])
    }

    private static func fetchCodex(_ path: String) async -> (String, [QuotaProvider], [BalanceProvider]) {
        let providerURL = URL(string: "https://chatgpt.com/settings/usage?tab=overview")!
        guard let data = try? Data(contentsOf: URL(fileURLWithPath: path)), let auth = object(data),
              let tokens = auth["tokens"] as? [String: Any], let token = tokens["access_token"] as? String, !token.isEmpty else {
            return ("ChatGPT", [], [BalanceProvider(id: "codex", name: "ChatGPT", balance: nil,
                error: "Could not read the local Codex sign-in", url: providerURL)])
        }
        let (usageData, error) = await requestJSON("https://chatgpt.com/backend-api/wham/usage", bearer: token)
        guard let usageData, let root = object(usageData), let rate = root["rate_limit"] as? [String: Any] else {
            return ("ChatGPT", [], [BalanceProvider(id: "codex", name: "ChatGPT", balance: nil,
                error: error ?? "Invalid usage response", url: providerURL)])
        }
        var windows: [UsageQuota] = []
        if let primary = rate["primary_window"] as? [String: Any] {
            windows.append(UsageQuota(id: "codex-5h", label: "5h",
                availablePercent: min(100, max(0, 100 - number(primary["used_percent"]))),
                resetsAt: date(primary["reset_at"])))
        }
        if let secondary = rate["secondary_window"] as? [String: Any] {
            windows.append(UsageQuota(id: "codex-week", label: "w",
                availablePercent: min(100, max(0, 100 - number(secondary["used_percent"]))),
                resetsAt: date(secondary["reset_at"])))
        }
        return windows.isEmpty
            ? ("ChatGPT", [], [BalanceProvider(id: "codex", name: "ChatGPT", balance: nil, error: "No quota windows returned", url: providerURL)])
            : ("ChatGPT", [QuotaProvider(id: "codex", name: "ChatGPT", quotas: windows)], [])
    }

    private static func fetchCommandCode(_ cookie: String) async -> (String, [QuotaProvider], [BalanceProvider]) {
        let providerURL = URL(string: "https://commandcode.ai/usage")!
        let (data, error) = await requestJSON("https://api.commandcode.ai/internal/billing/credits", cookie: commandCodeCookieHeader(cookie),
            origin: "https://commandcode.ai", referer: "https://commandcode.ai/")
        guard let data, let root = object(data), let credits = root["credits"] as? [String: Any] else {
            return ("Command Code", [], [BalanceProvider(id: "commandcode", name: "Command Code", balance: nil,
                error: error ?? "Invalid usage response", url: providerURL)])
        }
        let limits = root["windowLimits"] as? [String: Any] ?? [:]
        var windows: [UsageQuota] = []
        if let item = limits["fiveHour"] as? [String: Any], let quota = cappedQuota(item, label: "5h", id: "command-5h") { windows.append(quota) }
        if let item = limits["weekly"] as? [String: Any], let quota = cappedQuota(item, label: "w", id: "command-week") { windows.append(quota) }
        let monthly = number(credits["monthlyCredits"]), granted = number(credits["monthlyCreditsGranted"])
        if granted > 0 {
            let used = min(100, max(0, (granted - monthly) / granted * 100))
            var reset: Date?
            let (subscriptionData, _) = await requestJSON("https://api.commandcode.ai/internal/billing/subscriptions",
                cookie: commandCodeCookieHeader(cookie), origin: "https://commandcode.ai", referer: "https://commandcode.ai/")
            if let subscriptionData, let subscription = object(subscriptionData), subscription["success"] as? Bool == true,
               let subData = subscription["data"] as? [String: Any] { reset = date(subData["currentPeriodEnd"]) }
            windows.append(UsageQuota(id: "command-month", label: "m", availablePercent: 100 - used, resetsAt: reset))
        }
        return windows.isEmpty
            ? ("Command Code", [], [BalanceProvider(id: "commandcode", name: "Command Code", balance: nil, error: "No quota windows returned", url: providerURL)])
            : ("Command Code", [QuotaProvider(id: "commandcode", name: "Command Code", quotas: windows)], [])
    }

    private static func quotaWindow(_ data: [String: Any], label: String, id: String) -> UsageQuota? {
        // Keep this in lockstep with internal/usage.openCodeWindow.
        guard let used = openCodeNumber(data["percent"]) else { return nil }
        var reset: Date?
        for key in ["resetInSec", "resetInSeconds", "resetSeconds", "reset_sec", "resetsInSec"] {
            if let seconds = openCodeNumber(data[key]), seconds > 0 {
                reset = Date().addingTimeInterval(seconds)
                break
            }
        }
        if reset == nil {
            for key in ["resetAt", "resetsAt", "reset_at", "resets_at", "nextReset", "renewAt"] {
                if let value = openCodeDate(data[key]) {
                    reset = value
                    break
                }
            }
        }
        return UsageQuota(id: id, label: label, availablePercent: min(100, max(0, 100 - used)), resetsAt: reset)
    }

    // These mirror the TUI's numberValue and dateValue helpers for OpenCode Go.
    private static func openCodeNumber(_ value: Any?) -> Double? {
        guard let value, !(value is NSNull) else { return nil }
        if let value = value as? NSNumber { return value.doubleValue }
        if let value = value as? String {
            let scanner = Scanner(string: value)
            scanner.locale = Locale(identifier: "en_US_POSIX")
            return scanner.scanDouble()
        }
        return nil
    }

    private static func openCodeDate(_ value: Any?) -> Date? {
        guard let value else { return nil }
        if let text = value as? String {
            for options: ISO8601DateFormatter.Options in [
                [.withInternetDateTime, .withFractionalSeconds], [.withInternetDateTime],
            ] {
                let formatter = ISO8601DateFormatter()
                formatter.formatOptions = options
                if let date = formatter.date(from: text) { return date }
            }
            return nil
        }
        guard let seconds = openCodeNumber(value), seconds > 0 else { return nil }
        return Date(timeIntervalSince1970: seconds > 1e11 ? seconds / 1000 : seconds)
    }

    private static func cappedQuota(_ data: [String: Any], label: String, id: String) -> UsageQuota? {
        let cap = number(data["cap"])
        guard cap > 0 else { return nil }
        let available = 100 - number(data["used"]) / cap * 100
        return UsageQuota(id: id, label: label, availablePercent: min(100, max(0, available)), resetsAt: date(data["resetAt"]))
    }

    private static func commandCodeCookieHeader(_ raw: String) -> String {
        let value = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        let stripped = value.range(of: "^Cookie:", options: [.regularExpression, .caseInsensitive])
            .map { String(value[$0.upperBound...]).trimmingCharacters(in: .whitespacesAndNewlines) } ?? value
        guard !stripped.contains("=") && !stripped.contains(";") else { return stripped }
        let names = ["__Secure-commandcode_prod_.session_token", "commandcode_prod_.session_token",
            "__Host-commandcode_prod_.session_token", "__Host-better-auth.session_token",
            "__Secure-better-auth.session_token", "better-auth.session_token"]
        return names.map { "\($0)=\(stripped)" }.joined(separator: "; ")
    }

    private static func fetchOpenRouter(_ key: String) async -> (String, [QuotaProvider], [BalanceProvider]) {
        async let keyResult = requestJSON("https://openrouter.ai/api/v1/key", bearer: key)
        async let creditResult = requestJSON("https://openrouter.ai/api/v1/credits", bearer: key)
        let (keyData, keyRequestError) = await keyResult
        let (creditData, _) = await creditResult // Credits are optional for ordinary API keys.
        var quotas: [UsageQuota] = []
        var balance: String?
        var balanceUSD: Double?
        let keyDataObject = keyData.flatMap(object)?["data"] as? [String: Any]
        let keyError = keyRequestError ?? (keyDataObject == nil ? "Invalid API key response" : nil)
        if let data = keyDataObject {
            let limit = number(data["limit"])
            if limit > 0 {
                let reset = (data["limit_reset"] as? String ?? "").lowercased()
                let label: String
                switch reset {
                case "daily": label = "d"
                case "weekly": label = "w"
                case "monthly": label = "m"
                case "5h", "five_hour", "five-hour": label = "5h"
                default: label = "key"
                }
                let percent = optionalNumber(data["limit_remaining"]).map { $0 / limit * 100 }
                    ?? optionalNumber(data["usage"]).map { 100 - $0 / limit * 100 }
                if let percent {
                    quotas.append(UsageQuota(id: "openrouter-\(label)", label: label,
                        availablePercent: min(100, max(0, percent)), resetsAt: resetDate(for: reset)))
                }
            }
        }
        if let creditData, let root = object(creditData), let data = root["data"] as? [String: Any] {
            if let total = optionalNumber(data["total_credits"]), let used = optionalNumber(data["total_usage"]), total >= used {
                balanceUSD = total - used
                balance = money(total - used, currency: "USD")
            }
        }
        let providerURL = URL(string: "https://openrouter.ai/credits")!
        let quotaRows = quotas.isEmpty ? [] : [QuotaProvider(id: "openrouter", name: "OpenRouter", quotas: quotas)]
        let balanceRows = (balance != nil || keyError != nil) ?
            [BalanceProvider(id: "openrouter", name: "OpenRouter", balance: balance, error: keyError, url: providerURL,
                balanceUSD: balanceUSD)] : []
        return ("OpenRouter", quotaRows, balanceRows)
    }

    private static func fetchDeepSeek(_ key: String) async -> (String, [QuotaProvider], [BalanceProvider]) {
        let url = URL(string: "https://api.deepseek.com/user/balance")!
        let (data, error) = await requestJSON(url.absoluteString, bearer: key)
        guard let data, let root = object(data), let balances = root["balance_infos"] as? [[String: Any]], !balances.isEmpty else {
            return ("DeepSeek", [], [BalanceProvider(id: "deepseek", name: "DeepSeek", balance: nil,
                error: error ?? "Invalid balance response", url: URL(string: "https://platform.deepseek.com/usage")!)])
        }
        let selected = balances.first(where: { ($0["currency"] as? String)?.uppercased() == "USD" }) ?? balances[0]
        guard let amount = optionalNumber(selected["total_balance"]) else {
            return ("DeepSeek", [], [BalanceProvider(id: "deepseek", name: "DeepSeek", balance: nil,
                error: "Invalid balance response", url: URL(string: "https://platform.deepseek.com/usage")!)])
        }
        let currency = selected["currency"] as? String ?? "USD"
        let value = money(amount, currency: currency)
        let available = root["is_available"] as? Bool ?? true
        return ("DeepSeek", [], [BalanceProvider(id: "deepseek", name: "DeepSeek", balance: value,
            error: available ? nil : "Balance unavailable for API calls", url: URL(string: "https://platform.deepseek.com/usage")!,
            balanceUSD: isUSDCurrency(currency) ? amount : nil)])
    }

    private static func requestJSON(_ address: String, bearer: String? = nil, cookie: String? = nil,
                                   origin: String? = nil, referer: String? = nil) async -> (Data?, String?) {
        guard let url = URL(string: address) else { return (nil, "Invalid provider URL") }
        var request = URLRequest(url: url)
        request.timeoutInterval = 15
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if let bearer { request.setValue("Bearer \(bearer)", forHTTPHeaderField: "Authorization") }
        if let cookie {
            request.setValue(cookie.hasPrefix("Cookie:") ? String(cookie.dropFirst(7)).trimmingCharacters(in: .whitespaces) : cookie,
                             forHTTPHeaderField: "Cookie")
            request.setValue(origin ?? "https://platform.xiaomimimo.com", forHTTPHeaderField: "Origin")
            request.setValue(referer ?? "https://platform.xiaomimimo.com/#/console/balance", forHTTPHeaderField: "Referer")
        }
        do {
            let session = URLSession(configuration: .ephemeral,
                delegate: ProviderRedirectDelegate(allowedHost: url.host ?? ""), delegateQueue: nil)
            defer { session.finishTasksAndInvalidate() }
            let (data, response) = try await session.data(for: request)
            guard let response = response as? HTTPURLResponse else { return (nil, "Invalid provider response") }
            guard (200..<300).contains(response.statusCode) else { return (nil, "HTTP \(response.statusCode)") }
            return (data, nil)
        } catch { return (nil, error.localizedDescription) }
    }

    static func object(_ data: Data) -> [String: Any]? { (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] }
    static func optionalNumber(_ value: Any?) -> Double? {
        guard let value, !(value is NSNull) else { return nil }
        if let value = value as? NSNumber { return value.doubleValue }
        if let value = value as? String { return Double(value.trimmingCharacters(in: .whitespacesAndNewlines)) }
        return nil
    }
    static func number(_ value: Any?) -> Double {
        optionalNumber(value) ?? 0
    }
    private static func date(_ value: Any?) -> Date? {
        guard let value else { return nil }
        if let number = optionalNumber(value), number > 0 {
            return Date(timeIntervalSince1970: number > 1e11 ? number / 1000 : number)
        }
        guard let string = value as? String else { return nil }
        let iso = ISO8601DateFormatter()
        if let date = iso.date(from: string) { return date }
        let formatter = DateFormatter(); formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(secondsFromGMT: 0); formatter.dateFormat = "yyyy-MM-dd HH:mm:ss"
        return formatter.date(from: string)
    }
    static func money(_ value: Double, currency: String) -> String {
        let formatter = NumberFormatter(); formatter.numberStyle = .decimal
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.maximumFractionDigits = 2
        formatter.minimumFractionDigits = 2
        let amount = formatter.string(from: NSNumber(value: max(0, value))) ?? String(format: "%.2f", value)
        let symbol: String
        switch currency.uppercased() {
        case "USD", "$", "US$": symbol = "$"
        case "CNY", "JPY": symbol = "¥"
        case "EUR": symbol = "€"
        case "GBP": symbol = "£"
        default: symbol = currency.uppercased()
        }
        return "\(amount) \(symbol)"
    }
    static func isUSDCurrency(_ currency: String) -> Bool {
        ["USD", "$", "US$"].contains(currency.trimmingCharacters(in: .whitespacesAndNewlines).uppercased())
    }
    static func parseMiMoDate(_ value: String) -> Date? {
        let formatter = DateFormatter(); formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(secondsFromGMT: 0); formatter.dateFormat = "yyyy-MM-dd HH:mm:ss"
        return formatter.date(from: value)
    }
    private static func resetDate(for period: String) -> Date? {
        var calendar = Calendar(identifier: .gregorian); calendar.timeZone = TimeZone(secondsFromGMT: 0)!
        let now = Date()
        switch period {
        case "daily": return calendar.nextDate(after: now, matching: DateComponents(hour: 0), matchingPolicy: .nextTime)
        case "weekly":
            return calendar.nextDate(after: now, matching: DateComponents(hour: 0, weekday: 2), matchingPolicy: .nextTime)
        case "monthly":
            return calendar.nextDate(after: now, matching: DateComponents(day: 1, hour: 0), matchingPolicy: .nextTime)
        default: return nil
        }
    }
}

private final class ProviderRedirectDelegate: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    private let allowedHost: String
    init(allowedHost: String) { self.allowedHost = allowedHost }
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) {
        guard request.url?.scheme?.lowercased() == "https", request.url?.host?.lowercased() == allowedHost.lowercased() else {
            completionHandler(nil)
            return
        }
        completionHandler(request)
    }
}
