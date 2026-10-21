import CryptoKit
import Foundation

// The MiMo console's platform cookies are short-lived (about a day) and the
// web client silently re-mints them through the Xiaomi Account session. When a
// userId/passToken seed is configured, the same read-only SSO exchange is
// replayed here and the refreshed cookies live only in memory.
extension UsageManager {
    private static let miMoSessionStore = MiMoSessionStore()

    static func fetchMiMo(_ cookie: String, accountCookie: String) async -> (String, [QuotaProvider], [BalanceProvider]) {
        let page = URL(string: "https://platform.xiaomimimo.com/#/console/balance")!
        let store = miMoSessionStore
        await store.seedIfNeeded(platformSource: cookie, accountSource: accountCookie,
                                 platform: parseMiMoCookie(cookie), account: parseMiMoCookie(accountCookie))
        let stored = await store.cookies()
        var platform = stored.platform
        var account = stored.account

        var response = validMiMoCookies(platform) ? await miMoEndpoints(platform) : MiMoEndpointResult()
        if response.authFailed || !validMiMoCookies(platform) {
            guard validMiMoSeed(account) else {
                if !validMiMoCookies(platform) {
                    return ("Xiaomi MiMo", [], [BalanceProvider(id: "mimo", name: "Xiaomi MiMo", balance: nil,
                        error: "Xiaomi MiMo requires api-platform_serviceToken and userId cookies", url: page)])
                }
                return ("Xiaomi MiMo", [], [BalanceProvider(id: "mimo", name: "Xiaomi MiMo", balance: nil,
                    error: "Session expired. Sign in at platform.xiaomimimo.com and update providers.mimo.cookie, or set providers.mimo.account_cookie for automatic refresh.",
                    url: page)])
            }
            do {
                let refreshed = try await refreshMiMoSession(platform: platform, account: account)
                platform = refreshed.platform
                account = refreshed.account
                await store.update(platform: platform, account: account)
                response = await miMoEndpoints(platform)
                guard !response.authFailed else {
                    return ("Xiaomi MiMo", [], [BalanceProvider(id: "mimo", name: "Xiaomi MiMo", balance: nil,
                        error: "MiMo rejected the automatically refreshed session. Sign in again and update providers.mimo.account_cookie.",
                        url: page)])
                }
            } catch {
                return ("Xiaomi MiMo", [], [BalanceProvider(id: "mimo", name: "Xiaomi MiMo", balance: nil,
                    error: "Session expired and automatic refresh failed: \(error.localizedDescription)", url: page)])
            }
        }

        guard let balanceData = response.data["balance"], let root = object(balanceData), number(root["code"]) == 0,
              let data = root["data"] as? [String: Any] else {
            return ("Xiaomi MiMo", [], [BalanceProvider(id: "mimo", name: "Xiaomi MiMo", balance: nil,
                error: response.balanceError ?? "Invalid balance response", url: page)])
        }
        let currency = data["currency"] as? String ?? "CNY"
        guard let balanceValue = optionalNumber(data["balance"]) else {
            return ("Xiaomi MiMo", [], [BalanceProvider(id: "mimo", name: "Xiaomi MiMo", balance: nil,
                error: "Invalid balance response", url: page)])
        }
        let amount = money(balanceValue, currency: currency)
        var quotas: [UsageQuota] = []
        if let usageData = response.data["tokenPlan/usage"], let usageRoot = object(usageData), number(usageRoot["code"]) == 0,
           let usage = usageRoot["data"] as? [String: Any], let month = usage["monthUsage"] as? [String: Any],
           let items = month["items"] as? [[String: Any]], let item = items.first {
            let used = number(item["used"]), limit = number(item["limit"])
            if limit > 0 {
                let usedPercent = item["percent"].map(number) ?? used / limit * 100
                let reset = response.data["tokenPlan/detail"].flatMap(object).flatMap { $0["data"] as? [String: Any] }
                    .flatMap { $0["currentPeriodEnd"] as? String }.flatMap(parseMiMoDate)
                quotas.append(UsageQuota(id: "mimo-month", label: "m", availablePercent: min(100, max(0, 100 - usedPercent)), resetsAt: reset))
            }
        }
        return ("Xiaomi MiMo", quotas.isEmpty ? [] : [QuotaProvider(id: "mimo", name: "Xiaomi MiMo", quotas: quotas)],
            [BalanceProvider(id: "mimo", name: "Xiaomi MiMo", balance: amount, error: nil, url: page,
                balanceUSD: isUSDCurrency(currency) ? balanceValue : nil)])
    }

    private static func miMoEndpoints(_ cookies: [String: String]) async -> MiMoEndpointResult {
        var result = MiMoEndpointResult()
        await withTaskGroup(of: (String, MiMoHTTPResult).self) { group in
            for endpoint in ["balance", "tokenPlan/detail", "tokenPlan/usage"] {
                group.addTask {
                    guard let url = URL(string: "https://platform.xiaomimimo.com/api/v1/\(endpoint)") else {
                        return (endpoint, MiMoHTTPResult(error: "Invalid provider URL"))
                    }
                    return (endpoint, await miMoRequest(url, cookies: cookies, accept: "application/json, text/plain, */*"))
                }
            }
            for await (endpoint, http) in group {
                if let error = http.error {
                    if endpoint == "balance" { result.balanceError = error }
                    continue
                }
                if http.status == 302 || http.status == 401 {
                    result.authFailed = true
                    if endpoint == "balance" { result.balanceError = "HTTP \(http.status)" }
                    continue
                }
                guard (200..<300).contains(http.status) else {
                    if endpoint == "balance" { result.balanceError = "HTTP \(http.status)" }
                    continue
                }
                if let root = object(http.data), number(root["code"]) == 401 {
                    result.authFailed = true
                    if endpoint == "balance" { result.balanceError = "Xiaomi MiMo rejected the session cookie" }
                    continue
                }
                result.data[endpoint] = http.data
            }
        }
        return result
    }

    private static func refreshMiMoSession(platform: [String: String], account: [String: String]) async throws
        -> (platform: [String: String], account: [String: String]) {
        var updatedAccount = account
        let loginURL = try await miMoLoginURL(platform: platform)
        let auth = try await exchangeMiMoAccountSession(account: account, loginURL: loginURL)
        if let userId = auth["userId"] as? String, !userId.isEmpty { updatedAccount["userId"] = userId }
        if let passToken = auth["passToken"] as? String, !passToken.isEmpty { updatedAccount["passToken"] = passToken }
        let stsURL = try signedMiMoSTSURL(auth)
        let refreshed = try await visitMiMoSTS(stsURL)
        try await verifyMiMoSession(refreshed)
        return (refreshed, updatedAccount)
    }

    private static func miMoLoginURL(platform: [String: String]) async throws -> URL {
        var components = URLComponents(string: "https://platform.xiaomimimo.com/api/v1/genLoginUrl")!
        components.queryItems = [URLQueryItem(name: "currentPath", value: "/api/v1/userProfile")]
        guard let url = components.url else { throw MiMoError.invalid("Invalid MiMo login endpoint") }
        let http = await miMoRequest(url, cookies: platform, accept: "application/json, text/plain, */*")
        if let error = http.error { throw MiMoError.network(error) }
        var raw = http.location
        if raw == nil, let root = object(http.data) { raw = root["loginUrl"] as? String }
        guard let raw, let loginURL = URL(string: raw) else { throw MiMoError.invalid("MiMo did not return a login URL") }
        try validateMiMoLoginURL(loginURL)
        return loginURL
    }

    private static func validateMiMoLoginURL(_ url: URL) throws {
        guard miMoTrustedURL(url, host: "account.xiaomi.com"), url.path == "/pass/serviceLogin" else {
            throw MiMoError.invalid("MiMo returned an unsafe Xiaomi Account login URL")
        }
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        func value(_ name: String) -> String? { items.first(where: { $0.name == name })?.value }
        guard value("sid") == "api-platform" else {
            throw MiMoError.invalid("MiMo returned an unexpected Xiaomi Account service ID")
        }
        guard let callback = value("callback").flatMap({ URL(string: $0) }),
              miMoTrustedURL(callback, host: "platform.xiaomimimo.com"), callback.path == "/sts" else {
            throw MiMoError.invalid("MiMo returned an unsafe authentication callback")
        }
    }

    private static func exchangeMiMoAccountSession(account: [String: String], loginURL: URL) async throws -> [String: Any] {
        guard var components = URLComponents(url: loginURL, resolvingAgainstBaseURL: false) else {
            throw MiMoError.invalid("MiMo returned an invalid login URL")
        }
        var items = components.queryItems ?? []
        items.removeAll { $0.name == "_json" }
        items.append(URLQueryItem(name: "_json", value: "true"))
        components.queryItems = items
        guard let url = components.url else { throw MiMoError.invalid("MiMo returned an invalid login URL") }
        let http = await miMoRequest(url, cookies: account, accept: "application/json, text/plain, */*")
        if let error = http.error { throw MiMoError.network(error) }
        guard http.status < 400 else {
            throw MiMoError.auth("Xiaomi Account rejected the saved session (HTTP \(http.status))")
        }
        var text = String(decoding: http.data, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
        if text.hasPrefix("&&&START&&&") { text = String(text.dropFirst("&&&START&&&".count)) }
        guard let payload = object(Data(text.utf8)) else {
            throw MiMoError.auth("Xiaomi Account requires interactive sign-in. Sign in at account.xiaomi.com and update providers.mimo.account_cookie.")
        }
        guard optionalNumber(payload["code"]) == 0, let location = payload["location"] as? String, !location.isEmpty else {
            throw MiMoError.auth("Xiaomi Account session expired. Sign in at account.xiaomi.com and update providers.mimo.account_cookie.")
        }
        return payload
    }

    private static func signedMiMoSTSURL(_ auth: [String: Any]) throws -> URL {
        guard let raw = auth["location"] as? String, let url = URL(string: raw),
              miMoTrustedURL(url, host: "platform.xiaomimimo.com"), url.path == "/sts" else {
            throw MiMoError.invalid("Xiaomi Account returned an unsafe MiMo STS URL")
        }
        guard var components = URLComponents(url: url, resolvingAgainstBaseURL: false) else {
            throw MiMoError.invalid("Xiaomi Account returned an unsafe MiMo STS URL")
        }
        var items = components.queryItems ?? []
        if items.contains(where: { $0.name == "clientSign" || $0.name == "_ssign" }) { return url }
        guard let nonce = nonceString(auth["nonce"]), let ssecurity = auth["ssecurity"] as? String, !ssecurity.isEmpty else {
            throw MiMoError.invalid("Xiaomi Account response cannot be signed")
        }
        let digest = Insecure.SHA1.hash(data: Data("nonce=\(nonce)&\(ssecurity)".utf8))
        items.removeAll { $0.name == "clientSign" }
        items.append(URLQueryItem(name: "clientSign", value: Data(digest).base64EncodedString()))
        components.queryItems = items
        guard let signed = components.url else { throw MiMoError.invalid("Xiaomi Account response cannot be signed") }
        return signed
    }

    private static func nonceString(_ value: Any?) -> String? {
        if let value = value as? String, !value.isEmpty { return value }
        guard let value = value as? NSNumber else { return nil }
        let number = value.doubleValue
        return number.rounded() == number ? String(value.int64Value) : String(number)
    }

    private static func visitMiMoSTS(_ stsURL: URL) async throws -> [String: String] {
        let http = await miMoRequest(stsURL, cookies: [:], accept: "text/html,*/*")
        if let error = http.error { throw MiMoError.network(error) }
        guard http.status < 400 else { throw MiMoError.invalid("MiMo STS returned HTTP \(http.status)") }
        if let location = http.location {
            guard let target = URL(string: location, relativeTo: stsURL)?.absoluteURL,
                  miMoTrustedURL(target, host: "platform.xiaomimimo.com"), target.path == "/api/v1/userProfile" else {
                throw MiMoError.invalid("MiMo STS returned an unsafe redirect")
            }
        }
        let cookies = parseSetCookies(http.setCookie)
        for name in ["api-platform_serviceToken", "api-platform_slh", "api-platform_ph", "userId"] {
            guard cookieValue(cookies, name) != nil else {
                throw MiMoError.invalid("MiMo STS did not issue the \(name) cookie")
            }
        }
        return cookies
    }

    private static func verifyMiMoSession(_ cookies: [String: String]) async throws {
        guard let url = URL(string: "https://platform.xiaomimimo.com/api/v1/userProfile") else {
            throw MiMoError.invalid("Invalid MiMo verification endpoint")
        }
        let http = await miMoRequest(url, cookies: cookies, accept: "application/json, text/plain, */*")
        if let error = http.error { throw MiMoError.network(error) }
        if http.status == 302 || http.status == 401 {
            throw MiMoError.auth("MiMo rejected the new platform session (HTTP \(http.status))")
        }
        guard http.status < 400 else {
            throw MiMoError.invalid("MiMo session verification returned HTTP \(http.status)")
        }
    }

    private static func miMoRequest(_ url: URL, cookies: [String: String], accept: String) async -> MiMoHTTPResult {
        var request = URLRequest(url: url)
        request.timeoutInterval = 15
        request.setValue(accept, forHTTPHeaderField: "Accept")
        request.setValue("en-US,en;q=0.9", forHTTPHeaderField: "Accept-Language")
        request.setValue("UTC\(miMoTimeZoneOffset())", forHTTPHeaderField: "x-timeZone")
        request.setValue("https://platform.xiaomimimo.com", forHTTPHeaderField: "Origin")
        request.setValue("https://platform.xiaomimimo.com/#/console/balance", forHTTPHeaderField: "Referer")
        request.setValue("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/143.0.0.0 Safari/537.36",
                         forHTTPHeaderField: "User-Agent")
        if !cookies.isEmpty { request.setValue(cookieHeader(cookies), forHTTPHeaderField: "Cookie") }
        let session = URLSession(configuration: .ephemeral, delegate: MiMoNoRedirectDelegate(), delegateQueue: nil)
        defer { session.finishTasksAndInvalidate() }
        do {
            let (data, response) = try await session.data(for: request)
            guard let http = response as? HTTPURLResponse else { return MiMoHTTPResult(error: "Invalid MiMo response") }
            var result = MiMoHTTPResult(data: data, status: http.statusCode)
            for (key, value) in http.allHeaderFields {
                guard let name = key as? String, let text = value as? String else { continue }
                switch name.lowercased() {
                case "location": result.location = text
                case "set-cookie": result.setCookie = text
                default: break
                }
            }
            return result
        } catch {
            return MiMoHTTPResult(error: error.localizedDescription)
        }
    }

    private static func miMoTrustedURL(_ url: URL, host: String) -> Bool {
        url.scheme?.lowercased() == "https" && url.host?.lowercased() == host &&
            (url.port == nil || url.port == 443) && url.user == nil && url.password == nil
    }

    private static func miMoTimeZoneOffset() -> String {
        let seconds = TimeZone.current.secondsFromGMT()
        let total = abs(seconds) / 60
        return String(format: "%@%02d:%02d", seconds < 0 ? "-" : "+", total / 60, total % 60)
    }

    private static func parseMiMoCookie(_ value: String) -> [String: String] {
        var text = value.trimmingCharacters(in: .whitespacesAndNewlines)
        if text.lowercased().hasPrefix("cookie:") {
            text = String(text.dropFirst("cookie:".count)).trimmingCharacters(in: .whitespacesAndNewlines)
        }
        var cookies: [String: String] = [:]
        for part in text.split(separator: ";") {
            guard let equals = part.firstIndex(of: "=") else { continue }
            let name = part[..<equals].trimmingCharacters(in: .whitespaces)
            let cookieValue = part[part.index(after: equals)...].trimmingCharacters(in: .whitespaces)
            if !name.isEmpty { cookies[name] = cookieValue }
        }
        return cookies
    }

    private static func cookieHeader(_ cookies: [String: String]) -> String {
        cookies.keys.sorted().map { "\($0)=\(cookies[$0]!)" }.joined(separator: "; ")
    }

    private static func cookieValue(_ cookies: [String: String], _ name: String) -> String? {
        cookies.first { $0.key.caseInsensitiveCompare(name) == .orderedSame && !$0.value.isEmpty }?.value
    }

    private static func validMiMoCookies(_ cookies: [String: String]) -> Bool {
        cookieValue(cookies, "api-platform_serviceToken") != nil && cookieValue(cookies, "userId") != nil
    }

    private static func validMiMoSeed(_ cookies: [String: String]) -> Bool {
        cookieValue(cookies, "userId") != nil && cookieValue(cookies, "passToken") != nil
    }

    /// `HTTPURLResponse` merges repeated `Set-Cookie` headers with commas, so
    /// split only where a comma starts a new `name=` pair. Commas inside an
    /// `Expires=Wed, 21 Oct …` value are followed by a date, not a cookie name.
    private static func parseSetCookies(_ header: String?) -> [String: String] {
        guard let header, !header.isEmpty else { return [:] }
        var segments: [String] = []
        var start = header.startIndex
        var index = header.startIndex
        while index < header.endIndex {
            if header[index] == "," {
                var look = header.index(after: index)
                while look < header.endIndex, header[look] == " " { look = header.index(after: look) }
                var cursor = look
                while cursor < header.endIndex, header[cursor] != "=", header[cursor] != ";", header[cursor] != " " {
                    cursor = header.index(after: cursor)
                }
                if cursor < header.endIndex, header[cursor] == "=", cursor > look {
                    segments.append(String(header[start..<index]))
                    start = header.index(after: index)
                }
            }
            index = header.index(after: index)
        }
        segments.append(String(header[start...]))
        var cookies: [String: String] = [:]
        for segment in segments {
            let pair = segment.split(separator: ";", maxSplits: 1, omittingEmptySubsequences: true).first.map(String.init) ?? ""
            guard let equals = pair.firstIndex(of: "=") else { continue }
            let name = pair[..<equals].trimmingCharacters(in: .whitespaces)
            let value = pair[pair.index(after: equals)...].trimmingCharacters(in: .whitespaces)
            if !name.isEmpty, !value.isEmpty { cookies[name] = value }
        }
        return cookies
    }
}

private struct MiMoHTTPResult: Sendable {
    var data = Data()
    var status = 0
    var location: String?
    var setCookie: String?
    var error: String?

    init(data: Data = Data(), status: Int = 0, location: String? = nil, setCookie: String? = nil, error: String? = nil) {
        self.data = data
        self.status = status
        self.location = location
        self.setCookie = setCookie
        self.error = error
    }
}

private struct MiMoEndpointResult {
    var data: [String: Data] = [:]
    var balanceError: String?
    var authFailed = false
}

private actor MiMoSessionStore {
    private var platform: [String: String] = [:]
    private var account: [String: String] = [:]
    private var platformSource = ""
    private var accountSource = ""
    private var seeded = false

    func seedIfNeeded(platformSource: String, accountSource: String, platform: [String: String], account: [String: String]) {
        guard !seeded || platformSource != self.platformSource || accountSource != self.accountSource else { return }
        seeded = true
        self.platformSource = platformSource
        self.accountSource = accountSource
        self.platform = platform
        self.account = account
    }

    func cookies() -> (platform: [String: String], account: [String: String]) { (platform, account) }

    func update(platform: [String: String], account: [String: String]) {
        self.platform = platform
        self.account = account
    }
}

private enum MiMoError: LocalizedError {
    case auth(String)
    case network(String)
    case invalid(String)

    var errorDescription: String? {
        switch self {
        case .auth(let message), .network(let message), .invalid(let message): return message
        }
    }
}

private final class MiMoNoRedirectDelegate: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}
