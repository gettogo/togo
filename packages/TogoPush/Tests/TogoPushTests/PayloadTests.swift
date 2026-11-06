import Foundation
import Testing
@testable import TogoPush

struct PayloadTests {
    private struct File: Decodable { var plaintext: PushPayload }

    private static func load(_ name: String) throws -> PushPayload {
        var url = URL(filePath: #filePath)
        for _ in 0..<5 { url.deleteLastPathComponent() }
        url.append(path: "protocol/v1/fixtures/plaintext/\(name)")
        return try JSONDecoder().decode(File.self, from: Data(contentsOf: url)).plaintext
    }

    @Test func offlinePayloadDecodes() throws {
        let p = try Self.load("push.offline.json")
        #expect(p.kind == .offline)
        #expect(p.active == 2)
        #expect(p.sessionId == nil)
        #expect(p.host == "studio")
        #expect(NotificationTextBuilder.build(p, hostId: "h_1") != nil)
    }

    @Test func reminderPayloadDecodes() throws {
        let p = try Self.load("push.reminder.json")
        #expect(p.kind == .needsYou)
        #expect(p.reminder == true)
        #expect(p.requestId == "r_abcdefghij")
    }

    /// An approval that takes a typed answer says so; older payloads leave it out.
    @Test func allowTextDecodes() throws {
        #expect(try Self.load("push.needs_you-text.json").allowText == true)
        #expect(try Self.load("push-needs-you-subject.json").allowText == nil)
    }

    @Test func endedPayloadDecodes() throws {
        let p = try Self.load("push.ended.json")
        #expect(p.kind == .ended)
        #expect(p.exit == PushExit(code: 0, reason: "exit", detail: nil))
        let text = try #require(NotificationTextBuilder.build(p))
        #expect(text.body == "Ended")
    }

    @Test func failedPayloadDecodes() throws {
        let p = try Self.load("push.failed.json")
        #expect(p.kind == .failed)
        #expect(p.exit?.code != 0)
        #expect(NotificationTextBuilder.build(p) != nil)
    }

    @Test func newFieldsAreOmittedWhenNil() throws {
        let p = PushPayload(kind: .done, title: "t", body: "b", ts: 1)
        let json = try #require(String(data: JSONEncoder().encode(p), encoding: .utf8))
        #expect(!json.contains("exit") && !json.contains("active") && !json.contains("reminder"))
    }
}
