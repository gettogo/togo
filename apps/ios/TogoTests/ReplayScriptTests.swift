import Foundation
import Testing
import TogoKit
@testable import Togo

/// The UI tests' fake helper: the recorded streams it ships parse, decode as the protocol's
/// types, and release their chapters to the requests that caused them.
@MainActor
struct ReplayScriptTests {
    private let decoder = JSONDecoder()

    @Test(arguments: ["replay-chat", "replay-approval", "replay-question", DemoScript.resource])
    func everyShippedRecordingDecodes(_ name: String) throws {
        let script = try #require(ReplayScript.load(name), "\(name).jsonl is in the app's resources and parses")
        let opened = try #require(script.opened)
        _ = try decoder.decode(HarnessOpenResult.self, from: opened)
        let describe = try #require(script.read("harness.describe", atOrBefore: .max))
        #expect(try decoder.decode(HarnessDescriptor.self, from: describe).adapter != nil)
        for chapter in script.chapters {
            for note in chapter.notes {
                switch note.method {
                case "harness":
                    _ = try decoder.decode(HarnessNotification.self, from: note.params)
                case "event":
                    let event = try #require(try JSONSerialization.jsonObject(with: note.params) as? [String: Any])
                    if let approval = event["approval"] {
                        _ = try decoder.decode(Approval.self, from: JSONSerialization.data(withJSONObject: approval))
                    }
                default:
                    Issue.record("unexpected notification \(note.method)")
                }
                #expect((0.05...0.5).contains(note.delay))
            }
        }
    }

    @Test func requestsThatChangeTheSessionOpenChapters() throws {
        let script = try ReplayScript(jsonl: """
        // a comment
        {"jsonrpc":"2.0","id":1,"method":"harness.open","params":{"id":"s"}}
        {"jsonrpc":"2.0","id":1,"result":{"epoch":"e","cursor":0,"items":[],"more":false}}
        {"jsonrpc":"2.0","method":"harness","params":{"ts":"2026-11-07T12:00:00Z"}}

        {"jsonrpc":"2.0","id":2,"method":"harness.action","params":{"action":"set_model"}}
        {"jsonrpc":"2.0","method":"harness","params":{"ts":"2026-11-07T12:00:03Z"}}
        {"jsonrpc":"2.0","id":2,"result":{"result":"applied"}}
        {"jsonrpc":"2.0","id":3,"method":"harness.screen","params":{"screen":"model"}}
        {"jsonrpc":"2.0","id":3,"result":{"later":true}}
        {"jsonrpc":"2.0","id":4,"method":"approval.answer","params":{"requestId":"r"}}
        {"jsonrpc":"2.0","id":4,"result":{"state":"answered"}}
        """)
        #expect(script.opened != nil)
        #expect(script.chapters.map(\.trigger) == [nil, "harness.action:set_model", "approval.answer"])
        #expect(script.chapters[0].notes.count == 1)
        #expect(script.chapters[1].notes.count == 1)
        // Three seconds apart on the recording, half a second at most on replay.
        #expect(script.chapters[1].notes[0].delay == 0.5)
        #expect(script.chapters[1].response != nil)
        #expect(script.chapter(for: "approval.answer", after: 0) == 2)
        #expect(script.chapter(for: "harness.send", after: 0) == nil)
        // A read made after the action answers from then on; before it, from the first answer.
        #expect(script.read("harness.screen:model", atOrBefore: 0) != nil)
    }

    @Test func aBadLineSaysWhichLine() {
        #expect(throws: ReplayScript.ParseError.badLine(2)) {
            try ReplayScript(jsonl: "{\"jsonrpc\":\"2.0\"}\nnot json")
        }
    }
}
