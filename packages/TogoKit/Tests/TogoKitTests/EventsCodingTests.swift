import Foundation
import Testing
import TogoKit

struct EventsCodingTests {
    static let session = #"""
        {"id":"s_abcdefghij","bib":"01","name":"Shell","agent":"claude","cwd":"/home/u/proj",\#
        "createdAt":"2026-11-03T12:00:00Z","status":"needs_you","activity":"Editing main.go",\#
        "attachedClients":1,"managed":true,"muted":false,\#
        "meta":{"model":"opus","contextPercent":42.5,"costUsd":1.25}}
        """#

    static let approval = #"""
        {"requestId":"r_abcdefghij","sessionId":"s_abcdefghij","bib":"01","agent":"claude",\#
        "source":"hook","title":"Run make test?","detail":null,"snippet":"$ make test",\#
        "options":[{"n":1,"label":"Yes","role":"approve"},{"n":2,"label":"Always","role":"approve_always"},\#
        {"n":3,"label":"No","role":"deny"}],"allowText":true,"createdAt":"2026-11-03T12:00:01Z",\#
        "expiresAt":null,"state":"pending"}
        """#

    static let job = #"""
        {"jobId":"j_abcdefghij","name":"build","progress":40,"message":null,"status":"running",\#
        "startedAt":"2026-11-03T12:00:00Z","endedAt":null}
        """#

    /// One event per kind the helper sends, shaped as protocol/v1/schema.json `Event`.
    static let events: [(json: String, kind: String)] = [
        (#"{"seq":1,"ts":"2026-11-03T12:00:02Z","kind":"session","sessionId":"s_abcdefghij","session":\#(session)}"#, "session"),
        (#"{"seq":2,"ts":"2026-11-03T12:00:02Z","kind":"session.removed","sessionId":"s_abcdefghij"}"#, "session.removed"),
        (#"{"seq":3,"ts":"2026-11-03T12:00:02Z","kind":"approval.open","sessionId":"s_abcdefghij","approval":\#(approval)}"#, "approval.open"),
        (#"{"seq":4,"ts":"2026-11-03T12:00:02Z","kind":"approval.closed","sessionId":"s_abcdefghij","requestId":"r_abcdefghij","resolution":"desk"}"#, "approval.closed"),
        (#"{"seq":5,"ts":"2026-11-03T12:00:02Z","kind":"notify","sessionId":null,"title":"Deploy","body":"Finished","level":"needs_you"}"#, "notify"),
        (#"{"seq":6,"ts":"2026-11-03T12:00:02Z","kind":"job","sessionId":null,"job":\#(job)}"#, "job"),
    ]

    @Test func decodesEveryEventFixture() throws {
        for (json, kind) in Self.events {
            let original = try JSONDecoder().decode(JSONValue.self, from: Data(json.utf8))
            let envelope = try JSONDecoder().decode(EventEnvelope.self, from: Data(json.utf8))
            let reencoded = try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(envelope))
            #expect(reencoded == original, "\(kind) round trip")
            #expect(Self.kind(of: envelope.event) == kind)
        }
    }

    @Test func eventFieldsDecode() throws {
        let decoded = try Self.events.map { try JSONDecoder().decode(EventEnvelope.self, from: Data($0.json.utf8)) }
        guard case .session(let session) = decoded[0].event,
              case .approvalOpen(let approval) = decoded[2].event,
              case .job(let job) = decoded[5].event
        else {
            Issue.record("events decoded to the wrong cases")
            return
        }
        #expect(session.meta == SessionMeta(model: "opus", contextPercent: 42.5, costUsd: 1.25))
        #expect(approval.id == "r_abcdefghij")
        #expect(approval.options.map(\.role) == [.approve, .approveAlways, .deny])
        #expect(approval.expiresAt == nil)
        #expect(job.progress == 40 && job.endedAt == nil)
        #expect(decoded[3].event == .approvalClosed(requestId: "r_abcdefghij", resolution: .desk))
        #expect(decoded[4].event == .notify(title: "Deploy", body: "Finished", level: .needsYou))
        #expect(decoded[4].sessionId == nil)
        #expect(decoded[1].ts == Date(timeIntervalSince1970: 1_793_707_202))
    }

    @Test func unknownKindDecodesAsUnknown() throws {
        let json = #"{"seq":9,"ts":"2026-11-03T12:00:02Z","kind":"tunnel.open","sessionId":null,"port":8080}"#
        let envelope = try JSONDecoder().decode(EventEnvelope.self, from: Data(json.utf8))
        #expect(envelope.event == .unknown(kind: "tunnel.open"))
        #expect(envelope.seq == 9)
    }

    @Test func unknownWireValuesDecodeAndRoundTrip() throws {
        let closed = #"{"seq":7,"ts":"2026-11-03T12:00:02Z","kind":"approval.closed","sessionId":null,"requestId":"r_abcdefghij","resolution":"revoked"}"#
        let envelope = try JSONDecoder().decode(EventEnvelope.self, from: Data(closed.utf8))
        #expect(envelope.event == .approvalClosed(requestId: "r_abcdefghij", resolution: .unknown("revoked")))
        #expect(try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(envelope))
            == JSONDecoder().decode(JSONValue.self, from: Data(closed.utf8)))

        let approval = Self.approval
            .replacingOccurrences(of: #""role":"deny""#, with: #""role":"skip""#)
            .replacingOccurrences(of: #""source":"hook""#, with: #""source":"mcp""#)
        let decoded = try JSONDecoder().decode(Approval.self, from: Data(approval.utf8))
        #expect(decoded.options.map(\.role) == [.approve, .approveAlways, .unknown("skip")])
        #expect(decoded.source == .unknown("mcp"))

        let decoder = JSONDecoder()
        #expect(try decoder.decode(SessionStatus.self, from: Data(#""paused""#.utf8)) == .unknown("paused"))
        #expect(try decoder.decode(CloseResolution.self, from: Data(#""gone""#.utf8)) == .gone)
        #expect(try decoder.decode(NotifyLevel.self, from: Data(#""urgent""#.utf8)) == .unknown("urgent"))
        #expect(try decoder.decode(Job.Status.self, from: Data(#""cancelled""#.utf8)) == .unknown("cancelled"))
        #expect(try decoder.decode(FsReadResult.Encoding.self, from: Data(#""hex""#.utf8)) == .unknown("hex"))
        #expect(try decoder.decode(ApprovalOption.Role.self, from: Data(#""other""#.utf8)) == .other)
    }

    @Test func sessionWithoutMuteOrMetaDecodes() throws {
        let json = #"""
            {"id":"s_abcdefghij","bib":"01","name":"Shell","agent":"shell","cwd":"/tmp",\#
            "createdAt":"2026-11-03T12:00:00Z","status":"idle","activity":null,"attachedClients":0,"managed":true}
            """#
        let session = try JSONDecoder().decode(Session.self, from: Data(json.utf8))
        #expect(session.muted == false)
        #expect(session.meta == SessionMeta(model: nil, contextPercent: nil, costUsd: nil))
    }

    @Test func decisionWireForm() {
        #expect(Decision.approve.wire == "approve")
        #expect(Decision.deny.wire == "deny")
        #expect(Decision.option(3).wire == "option:3")
        #expect(Decision.text("a b").wire == "text:a b")
    }

    @Test func uploadDataTravelsAsUnpaddedBase64URL() throws {
        let params = UploadParams(sessionId: "s_abcdefghij", name: "shot.png", data: Data([0xfb, 0xff, 0x00, 0x01]))
        let encoded = try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(params))
        guard case .object(let fields) = encoded else {
            Issue.record("upload params did not encode as an object")
            return
        }
        #expect(fields["data"] == .string("-_8AAQ"))
        #expect(try JSONDecoder().decode(UploadParams.self, from: JSONEncoder().encode(params)) == params)
    }

    static let uploadFixtures: [URL] = (try? FileManager.default
        .contentsOfDirectory(at: FixtureTests.fixturesDirectory, includingPropertiesForKeys: nil)
        .filter { $0.lastPathComponent.hasPrefix("fs.upload") }) ?? []

    @Test(.enabled(if: !uploadFixtures.isEmpty, "no fs.upload fixtures in protocol/v1/fixtures yet"))
    func uploadFixturesRoundTrip() throws {
        for file in Self.uploadFixtures {
            let fixture = try JSONDecoder().decode(JSONValue.self, from: Data(contentsOf: file))
            guard case .object(let fields) = fixture, case .object(let request)? = fields["request"],
                  let params = request["params"]
            else {
                Issue.record("\(file.lastPathComponent): no request params")
                continue
            }
            let typed = try JSONDecoder().decode(UploadParams.self, from: JSONEncoder().encode(params))
            let reencoded = try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(typed))
            #expect(reencoded == params, "\(file.lastPathComponent)")
        }
    }

    private static func kind(of event: HelperEvent) -> String {
        switch event {
        case .session: "session"
        case .sessionRemoved: "session.removed"
        case .approvalOpen: "approval.open"
        case .approvalClosed: "approval.closed"
        case .notify: "notify"
        case .job: "job"
        case .unknown(let kind): kind
        }
    }
}
