import Foundation
import Testing
@testable import TogoKit

/// Small builders for harness items, open results and notifications.
enum HarnessSamples {
    static let t0 = Date(timeIntervalSince1970: 1_793_000_000)

    static func at(_ seconds: TimeInterval) -> WireDate { WireDate(t0.addingTimeInterval(seconds)) }

    static func user(_ id: String, seq: Int, turn: String? = nil, text: String = "fix it", queued: Bool = false) -> HarnessItem {
        HarnessItem(id: id, seq: seq, turnId: turn ?? id, at: at(0),
                    body: .user(.init(text: text, queued: queued, source: "phone")))
    }

    static func reply(_ id: String, seq: Int, turn: String?, text: String = "Done.", partial: Bool = false) -> HarnessItem {
        HarnessItem(id: id, seq: seq, turnId: turn, at: at(10), body: .assistant(.init(text: text, partial: partial)))
    }

    static func tool(
        _ id: String, seq: Int, turn: String?, state: HarnessState = .ok, start: TimeInterval = 1, end: TimeInterval? = 2,
        files: [String] = [], parent: String? = nil
    ) -> HarnessItem {
        let changes = files.map { HarnessFileChange(path: $0, added: 1, removed: 0) }
        return HarnessItem(
            id: id, seq: seq, turnId: turn, parent: parent, at: at(start),
            body: .tool(.init(callId: id, name: "Edit", title: "Edit \(id)", category: .edit, state: state,
                              startedAt: at(start), endedAt: end.map(at), files: changes.isEmpty ? nil : changes))
        )
    }

    static func turn(_ turn: String, seq: Int, state: HarnessState = .ok, steps: Int, files: Int) -> HarnessItem {
        HarnessItem(id: "\(turn).turn", seq: seq, turnId: turn, at: at(0),
                    body: .turn(.init(state: state, startedAt: at(0), endedAt: state.isLive ? nil : at(72),
                                      steps: steps, filesChanged: files)))
    }

    static func approval(_ id: String, seq: Int, turn: String?) -> HarnessItem {
        HarnessItem(id: id, seq: seq, turnId: turn, at: at(3), body: .approval(.init(requestId: "r-\(id)")))
    }

    static func agent(_ id: String, seq: Int, turn: String?) -> HarnessItem {
        HarnessItem(id: id, seq: seq, turnId: turn, at: at(4),
                    body: .agent(.init(agentId: id, name: "explorer", state: .running, startedAt: at(4))))
    }

    static func mission(rev: Int, sections: [MissionSectionKind] = MissionSectionKind.order) -> HarnessMission {
        HarnessMission(rev: rev, summary: MissionSummary(), sections: sections.map { HarnessSection(kind: $0) })
    }

    static func open(epoch: String = "e1", cursor: Int, items: [HarnessItem], more: Bool = false, rev: Int = 1) -> HarnessOpenResult {
        HarnessOpenResult(epoch: epoch, cursor: cursor, items: items, more: more, mission: mission(rev: rev))
    }

    static func items(_ items: [HarnessItem], epoch: String = "e1") -> HarnessNotification {
        HarnessNotification(sessionId: "s1", kind: .items, ts: at(0), epoch: epoch, items: items)
    }

    static func delta(_ rev: Int, _ ops: [HarnessDelta.Op], epoch: String = "e1") -> HarnessNotification {
        HarnessNotification(sessionId: "s1", kind: .mission, ts: at(0), epoch: epoch, mission: HarnessDelta(rev: rev, ops: ops))
    }

    static func reset(epoch: String) -> HarnessNotification {
        HarnessNotification(sessionId: "s1", kind: .reset, ts: at(0), epoch: epoch, reason: "epoch")
    }

    static func row(_ id: String, state: HarnessState = .running) -> HarnessRow {
        HarnessRow(id: id, title: id, state: state)
    }
}

/// The harness fixtures (open served, notifications still pending) through the reducers: what the chat draws from them.
struct HarnessFixtureTests {
    static func fixture(_ path: String, pending: Bool = true) throws -> Data {
        try Data(contentsOf: FixtureTests.fixturesDirectory.appending(path: pending ? "pending/\(path)" : path))
    }

    static func openResult() throws -> HarnessOpenResult {
        struct Exchange: Decodable {
            struct Response: Decodable { var result: HarnessOpenResult }
            var response: Response
        }
        return try JSONDecoder().decode(Exchange.self, from: fixture("harness.open.json", pending: false)).response.result
    }

    static func notes() throws -> [HarnessNotification] {
        struct File: Decodable {
            struct Note: Decodable { var params: HarnessNotification }
            var notifications: [Note]
        }
        return try JSONDecoder().decode(File.self, from: fixture("notifications/harness.json")).notifications.map(\.params)
    }

    @Test func openFixtureFoldsIntoPromptWorkAndReply() throws {
        var feed = HarnessFeedState()
        feed.load(try Self.openResult())
        let rows = TurnFold.rows(feed.timeline.conversation())
        #expect(rows.count == 3)
        guard rows.count == 3, case .prompt = rows[0], case .work(_, let summary, let steps) = rows[1], case .reply = rows[2] else {
            Issue.record("unexpected rows \(rows.map(\.id))")
            return
        }
        #expect(summary.steps == 2)
        #expect(summary.filesChanged == 1)
        #expect(!summary.running)
        #expect(summary.elapsed(now: .now) == 76)
        #expect(steps.map(\.id) == ["toolu_01", "toolu_02"])
    }

    @Test func notificationExamplesAfterTheOpenFixture() throws {
        var feed = HarnessFeedState()
        feed.load(try Self.openResult())
        let notes = try Self.notes()
        // The tool update is older than the cursor; the delta is older than the held rev.
        #expect(feed.apply(notes[0]) == .ignored)
        #expect(feed.apply(notes[1]) == .ignored)
        #expect(feed.apply(notes[2]) == .reopen)
        #expect(feed.openAfter == 0)
    }
}
