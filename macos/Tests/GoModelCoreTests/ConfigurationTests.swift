import Foundation
import Testing
@testable import GoModelCore

struct ConfigurationTests {
    private func withConfig(_ text: String, run: (String) throws -> Void) throws {
        let file = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".yaml")
        try text.write(to: file, atomically: true, encoding: .utf8)
        defer { try? FileManager.default.removeItem(at: file) }
        try run(file.path)
    }

    @Test func existingYAMLWithQuotedSecretsAndUnrelatedProviders() throws {
        try withConfig("""
        gomodel:
          url: 'http://localhost:8080'
          api_key: 'value:# with spaces'
        providers:
          codex:
            auth_path: ~/.codex/auth.json
        """) { path in
            let config = try Configuration.load(path: path, environment: [:])
            #expect(config.apiKey == "value:# with spaces")
            let override = try Configuration.load(path: path, environment: ["GOMODEL_API_KEY": "override"])
            #expect(override.apiKey == "override")
        }
    }

    @Test func credentialInURLIsRejected() throws {
        try withConfig("gomodel: {url: 'https://user:password@example.com', api_key: token}") { path in
            #expect(throws: (any Error).self) { try Configuration.load(path: path, environment: [:]) }
        }
    }

    @Test func invalidYAMLDiagnosticsDoNotExposeSecret() throws {
        try withConfig("gomodel: {api_key: [SUPERSECRET") { path in
            do {
                _ = try Configuration.load(path: path, environment: [:])
                Issue.record("Expected invalid YAML")
            } catch {
                #expect(!error.localizedDescription.contains("SUPERSECRET"))
            }
        }
    }

    @Test func mimoCredentialsComeFromYAMLAndEnvironment() throws {
        try withConfig("""
        gomodel: {url: 'http://localhost:8080', api_key: token}
        providers:
          mimo:
            cookie: 'api-platform_serviceToken=t; userId=1'
            account_cookie: 'userId=1; passToken=p'
        """) { path in
            let config = try Configuration.load(path: path, environment: [:])
            #expect(config.miMoCookie == "api-platform_serviceToken=t; userId=1")
            #expect(config.miMoAccountCookie == "userId=1; passToken=p")
            let override = try Configuration.load(path: path, environment: ["MIMO_ACCOUNT_COOKIE": "userId=2; passToken=q"])
            #expect(override.miMoAccountCookie == "userId=2; passToken=q")
        }
    }
}
