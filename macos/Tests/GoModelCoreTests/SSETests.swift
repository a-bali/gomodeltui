import Foundation
import Testing
@testable import GoModelCore

struct SSETests {
    @Test func networkFramingBOMCRLFCROnlyAndMultiline() throws {
        var framer = SSEFramer()
        let wire = "\u{feff}: comment\r\nid: 12\r\nevent: audit.completed\r\ndata: {\r\ndata: \"request_id\":\"r\"}\r\n\r\nid: 13\rdata: second\r\r"
        var events: [AuditEvent] = []
        for byte in wire.utf8 {
            if let event = try framer.consume(byte: byte) { events.append(event) }
        }
        #expect(events.count == 2)
        #expect(events[0].id == "12")
        #expect(events[0].type == "audit.completed")
        #expect(String(decoding: events[0].data, as: UTF8.self) == "{\n\"request_id\":\"r\"}")
        #expect(events[1].id == "13")
    }

    @Test func idResetAndComments() throws {
        var parser = SSEParser()
        #expect(try parser.consume(line: ":heartbeat") == nil)
        #expect(try parser.consume(line: "") == nil)
        #expect(try parser.consume(line: "id:") == nil)
        let reset = try parser.consume(line: "")
        #expect(reset?.id == "")
        #expect(reset?.hasExplicitID == true)
        #expect(try parser.consume(line: "data: next") == nil)
        #expect(try parser.consume(line: "")?.hasExplicitID == false)
    }

    @Test func oversizedEventIsRejected() throws {
        var parser = SSEParser()
        #expect(throws: (any Error).self) { try parser.consume(line: "data: " + String(repeating: "x", count: 8 * 1024 * 1024)) }
    }
}
