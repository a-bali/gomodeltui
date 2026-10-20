import Foundation
import Testing
@testable import GoModelCore

struct RequestStoreTests {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func event(_ id: String = "one", type: String = "audit.completed", fields: [String: Any] = [:], historical: Bool = false) throws -> AuditEvent {
        var values: [String: Any] = ["status_code": 200, "requested_model": "virtual", "provider_name": "provider", "resolved_model": "model"]
        values.merge(fields) { _, new in new }
        let payload: [String: Any] = ["request_id": id, "timestamp": ISO8601DateFormatter().string(from: now), "type": type, "data": values]
        return AuditEvent(type: type, data: try JSONSerialization.data(withJSONObject: payload), historical: historical)
    }

    @Test func replayAndHistoryOverlapCountOneLogicalRequest() throws {
        var store = RequestStore()
        let completed = try event(fields: ["data": ["attempts": [["success": false], ["success": true]]]])
        try store.apply(completed)
        try store.apply(completed)
        try store.apply(event(fields: ["status_code": 500], historical: true))
        let buckets = store.buckets(now: now, window: 3600)
        #expect(buckets.reduce(0) { $0 + $1.success } == 1)
        #expect(buckets.reduce(0) { $0 + $1.failed } == 0)
        #expect(store.records.count == 1)
        #expect(store.records["one"]?.destinationModel == "provider/model")
        #expect(store.records["one"]?.failover == true)
    }

    @Test func http200StreamFailureAndHistoricalErrorAreRed() throws {
        var store = RequestStore()
        try store.apply(event("stream", type: "audit.failed", fields: ["error": "stream interrupted"]))
        try store.apply(event("history", fields: ["error": "delivery failed"], historical: true))
        #expect(store.buckets(now: now, window: 3600).reduce(0) { $0 + $1.failed } == 2)
    }

    @Test func lateStartDoesNotResurrectCompletedRequest() throws {
        var store = RequestStore()
        try store.apply(event())
        try store.apply(event(type: "audit.started", fields: ["input_tokens": 999]))
        #expect(store.records["one"]!.terminal)
        #expect(store.records["one"]?.inputTokens == nil)
    }

    @Test func lateLiveCompletionSupersedesHistoricalRecord() throws {
        var store = RequestStore()
        try store.apply(event(fields: ["status_code": 500], historical: true))
        try store.apply(event(fields: ["input_tokens": 0, "output_tokens": "123", "cached_input_ratio": 0.95, "duration_ns": 2_500_000_000]))
        let request = store.records["one"]!
        #expect(request.success)
        #expect(request.inputTokens == 0)
        #expect(request.outputTokens == 123)
        #expect(request.cacheRatio == 0.95)
        #expect(request.duration == 2.5)
    }

    @Test func mcpAndInFlightRequestsDoNotEnterChart() throws {
        var store = RequestStore()
        try store.apply(event("mcp", fields: ["provider_name": "mcp", "path": "/mcp", "data": ["request_body": ["method": "tools/call", "params": ["name": "search"]]]]))
        try store.apply(event("running", type: "audit.started"))
        #expect(store.records["mcp"]?.destinationModel == "tools/call search")
        #expect(store.buckets(now: now, window: 3600).reduce(0) { $0 + $1.total } == 0)
        #expect(store.records.count == 2)
    }

    @Test func bucketBoundariesAndPruning() throws {
        var store = RequestStore()
        try store.apply(event())
        #expect(store.buckets(now: now, window: 3600).last?.success == 1)
        #expect(store.buckets(now: now.addingTimeInterval(3600), window: 3600).first?.success == 1)
        #expect(store.buckets(now: now.addingTimeInterval(3601), window: 3600).reduce(0) { $0 + $1.total } == 0)
        #expect(store.buckets(now: now.addingTimeInterval(-1), window: 3600).reduce(0) { $0 + $1.total } == 0)
        store.prune(before: now.addingTimeInterval(1))
        #expect(store.records.isEmpty)
    }

    @Test func previewFormatsAndMissingValues() throws {
        let bodies: [([String: Any], String)] = [
            (["request_body": ["messages": [["role": "user", "content": "hello\n world"]]]], "hello world"),
            (["request_body": ["input": "Responses input"]], "Responses input"),
            (["request_body": ["input": [["type": "function_call", "name": "read", "arguments": "{}"]]]], "tool: read {}"),
            (["response_body": ["output_text": "response fallback"]], "response fallback"),
            (["response_body": ["choices": [["message": ["content": [["text": "typed content"]]]]]]], "typed content"),
        ]
        for (body, expected) in bodies {
            var store = RequestStore()
            try store.apply(event(fields: ["data": body]))
            #expect(store.records["one"]?.preview == expected)
            #expect(store.records["one"]?.inputTokens == nil)
            #expect(store.records["one"]?.cacheRatio == nil)
        }
    }

    @Test func malformedEventsDoNotDestroyExistingHistory() throws {
        var store = RequestStore()
        try store.apply(event())
        #expect(throws: (any Error).self) { try store.apply(AuditEvent(type: "audit.completed", data: Data("{broken".utf8))) }
        #expect(throws: (any Error).self) { try store.apply(AuditEvent(type: "audit.completed", data: Data(#"{"request_id":"bad","data":42}"#.utf8))) }
        #expect(store.records.count == 1)
    }

    @Test func defaultSSEMessageUsesPayloadType() throws {
        var store = RequestStore()
        var value = try event()
        value.type = "message"
        try store.apply(value)
        #expect(store.records["one"]?.terminal == true)
    }

    @Test func usageAfterCompletionEnrichesTokensWithoutChangingOutcomeOrTime() throws {
        var store = RequestStore()
        try store.apply(event(type: "audit.failed", fields: ["error": "stream failed"]))
        try store.apply(event(type: "usage.completed", fields: ["input_tokens": 1500, "output_tokens": 200, "cached_input_ratio": 0.8]))
        let record = try #require(store.records["one"])
        #expect(record.inputTokens == 1500)
        #expect(record.outputTokens == 200)
        #expect(record.cacheRatio == 0.8)
        #expect(record.terminal && !record.success)
        #expect(record.error == "stream failed")
        #expect(record.timestamp == now)
        #expect(store.buckets(now: now, window: 3600).reduce(0) { $0 + $1.total } == 1)
    }

    @Test func usageBeforeCompletionAndNestedProviderCacheDetails() throws {
        var store = RequestStore()
        try store.apply(event(type: "usage.completed", fields: ["usage": ["prompt_tokens": 1000, "completion_tokens": 0, "prompt_tokens_details": ["cached_tokens": 750]]]))
        #expect(store.records["one"]?.terminal == false)
        try store.apply(event())
        #expect(store.records["one"]?.inputTokens == 1000)
        #expect(store.records["one"]?.outputTokens == 0)
        #expect(store.records["one"]?.cacheRatio == 0.75)
        #expect(store.records["one"]?.success == true)
    }

    @Test func historyCanFillMissingUsageOnLiveCompletion() throws {
        var store = RequestStore()
        try store.apply(event())
        try store.apply(event(fields: ["input_tokens": 500, "output_tokens": 80, "cached_input_ratio": 0.5, "status_code": 500], historical: true))
        #expect(store.records["one"]?.success == true)
        #expect(store.records["one"]?.inputTokens == 500)
        #expect(store.records["one"]?.outputTokens == 80)
    }
}
