import Foundation
import Yams

public struct Configuration: Sendable {
    public let baseURL: URL
    public let apiKey: String
    public let openCodeAPIKey: String
    public let commandCodeCookie: String
    public let codexAuthPath: String
    public let openRouterAPIKey: String
    public let deepSeekAPIKey: String
    public let miMoCookie: String

    public init(baseURL: URL, apiKey: String, openCodeAPIKey: String = "", commandCodeCookie: String = "",
                codexAuthPath: String = "", openRouterAPIKey: String = "", deepSeekAPIKey: String = "", miMoCookie: String = "") {
        self.baseURL = baseURL
        self.apiKey = apiKey
        self.openCodeAPIKey = openCodeAPIKey
        self.commandCodeCookie = commandCodeCookie
        self.codexAuthPath = codexAuthPath
        self.openRouterAPIKey = openRouterAPIKey
        self.deepSeekAPIKey = deepSeekAPIKey
        self.miMoCookie = miMoCookie
    }

    public static func defaultPath(environment: [String: String] = ProcessInfo.processInfo.environment) -> String {
        if let path = environment["GOMODELTUI_CONFIG"], !path.isEmpty { return path }
        let root = environment["XDG_CONFIG_HOME"] ?? NSHomeDirectory() + "/.config"
        return root + "/gomodeltui/config.yaml"
    }

    public static func load(path: String, environment: [String: String] = ProcessInfo.processInfo.environment) throws -> Configuration {
        var root: [String: Any] = [:]
        let expanded = NSString(string: path).expandingTildeInPath
        if FileManager.default.fileExists(atPath: expanded) {
            do {
                let text = try String(contentsOfFile: expanded, encoding: .utf8)
                if let value = try Yams.load(yaml: text) {
                    guard let mapping = value as? [String: Any] else { throw ConfigurationError.invalidFile }
                    root = mapping
                }
            } catch {
                // YAML parser diagnostics may include the source line and its credential.
                throw ConfigurationError.invalidFile
            }
        } else if environment["GOMODEL_API_KEY"]?.isEmpty != false {
            throw ConfigurationError.missingFile
        }
        let goModel = root["gomodel"] as? [String: Any] ?? [:]
        func setting(_ env: String, _ value: Any?, _ fallback: String = "") -> String {
            let override = environment[env]?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            return override.isEmpty ? (value as? String ?? fallback) : override
        }
        let address = setting("GOMODEL_URL", goModel["url"], "http://localhost:8080")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard let url = URL(string: address), ["http", "https"].contains(url.scheme?.lowercased() ?? ""),
              url.host != nil, url.user == nil, url.password == nil, url.query == nil, url.fragment == nil else {
            throw ConfigurationError.invalidURL
        }
        let key = setting("GOMODEL_API_KEY", goModel["api_key"]).trimmingCharacters(in: .whitespacesAndNewlines)
        guard !key.isEmpty, !key.contains("\n"), !key.contains("\r") else { throw ConfigurationError.missingKey }
        let providers = root["providers"] as? [String: Any] ?? [:]
        let openCode = providers["opencode"] as? [String: Any] ?? [:]
        let commandCode = providers["commandcode"] as? [String: Any] ?? [:]
        let codex = providers["codex"] as? [String: Any] ?? [:]
        let openRouter = providers["openrouter"] as? [String: Any] ?? [:]
        let deepSeek = providers["deepseek"] as? [String: Any] ?? [:]
        let miMo = providers["mimo"] as? [String: Any] ?? [:]
        var authPath = setting("PROVIDERS_CODEX_AUTH_PATH", codex["auth_path"]).trimmingCharacters(in: .whitespacesAndNewlines)
        if authPath.isEmpty {
            let defaultAuth = NSHomeDirectory() + "/.codex/auth.json"
            if FileManager.default.fileExists(atPath: defaultAuth) { authPath = defaultAuth }
        } else {
            authPath = NSString(string: authPath).expandingTildeInPath
        }
        return Configuration(baseURL: url, apiKey: key,
            openCodeAPIKey: setting("PROVIDERS_OPENCODE_API_KEY", openCode["api_key"]).trimmingCharacters(in: .whitespacesAndNewlines),
            commandCodeCookie: setting("PROVIDERS_COMMANDCODE_COOKIE", commandCode["cookie"]).trimmingCharacters(in: .whitespacesAndNewlines),
            codexAuthPath: authPath,
            openRouterAPIKey: setting("OPENROUTER_API_KEY", openRouter["api_key"]).trimmingCharacters(in: .whitespacesAndNewlines),
            deepSeekAPIKey: setting("DEEPSEEK_API_KEY", deepSeek["api_key"]).trimmingCharacters(in: .whitespacesAndNewlines),
            miMoCookie: setting("MIMO_COOKIE", miMo["cookie"]).trimmingCharacters(in: .whitespacesAndNewlines))
    }
}

public enum ConfigurationError: LocalizedError {
    case missingFile, invalidFile, invalidURL, missingKey
    public var errorDescription: String? {
        switch self {
        case .missingFile: return "Choose your gomodeltui config.yaml, or set GOMODEL_API_KEY before launching."
        case .invalidFile: return "Could not read the YAML configuration. Check its syntax and file permissions."
        case .invalidURL: return "GoModel URL must be an HTTP(S) address without embedded credentials, query, or fragment."
        case .missingKey: return "Set gomodel.api_key in the configuration, or GOMODEL_API_KEY in the launch environment."
        }
    }
}
