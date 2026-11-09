import Foundation
import Testing
@testable import TogoKit

private struct ContractExchange<R: Decodable>: Decodable {
    struct Response: Decodable { var result: R }
    var response: Response
}

/// The audit-2 protocol fields: answerable approvals and questions, question ↔ approval links,
/// command kinds and selection confirms. Old payloads without them decode to the defaults.
struct HarnessContractCodingTests {
    private static func result<T: Decodable>(_ type: T.Type, _ name: String) throws -> T {
        try JSONDecoder().decode(ContractExchange<T>.self, from: HarnessFixtureTests.fixture(name)).response.result
    }

    @Test func unanswerableApprovalDecodesAndRoundTrips() throws {
        struct List: Codable { var approvals: [Approval] }
        let list = try Self.result(List.self, "approval.list-unanswerable.json")
        let a = try #require(list.approvals.first)
        #expect(!a.answerable)
        #expect(a.options.isEmpty)
        let again = try JSONDecoder().decode(Approval.self, from: JSONEncoder().encode(a))
        #expect(!again.answerable)
    }

    @Test func approvalWithoutTheFieldIsAnswerable() throws {
        let a = try JSONDecoder().decode(Approval.self, from: Data(EventsCodingTests.approval.utf8))
        #expect(a.answerable)
        let json = String(decoding: try JSONEncoder().encode(a), as: UTF8.self)
        #expect(!json.contains("answerable"))
    }

    @Test func questionLinksAndAnswerable() throws {
        let page = try Self.result(HarnessPage.self, "harness.history-needs.json")
        let bodies = page.items.map(\.body)
        guard case .question(let linked) = bodies[0], case .approval(let owned) = bodies[1],
              case .question(let readOnly) = bodies[2], case .question(let old) = bodies[3] else {
            Issue.record("unexpected bodies \(page.items.map(\.id))")
            return
        }
        #expect(linked.requestId == "r_abcdefghij")
        #expect(linked.isAnswerable)
        #expect(linked.questions[1].multi)
        #expect(owned.owner == "toolu_09")
        #expect(!readOnly.isAnswerable)
        #expect(old.answerable == nil && old.isAnswerable)
        #expect(old.requestId == nil)
    }

    @Test func questionWithNullOptionsDecodesAsNone() throws {
        let json = #"{"id":"0","prompt":"Kimi is waiting for you","options":null,"multi":false,"allowText":false}"#
        let q = try JSONDecoder().decode(HarnessQuestion.self, from: Data(json.utf8))
        #expect(q.options.isEmpty)
        let missing = #"{"id":"0","prompt":"p","multi":true,"allowText":true}"#
        #expect(try JSONDecoder().decode(HarnessQuestion.self, from: Data(missing.utf8)).multi)
    }

    @Test func commandKinds() throws {
        let items = try Self.result(HarnessCompleteResult.self, "harness.complete-command-kinds.json").items
        #expect(items.map(\.kind) == [.choice, .picker, .run, .run, .run, .terminal])
        #expect(items[0].choices?.first { $0.selected == true }?.id == "high")
        #expect(items[2].action == "compact")
        #expect(items[3].role == .destructive)
        #expect(items[4].opens == "status")
    }

    @Test func oldCompletionHasNoKind() throws {
        let json = #"{"insert":"/model ","title":"/model","hint":"[model]","screen":"model"}"#
        let c = try JSONDecoder().decode(HarnessCompletion.self, from: Data(json.utf8))
        #expect(c.kind == nil && c.role == nil && c.action == nil && c.opens == nil && c.choices == nil)
    }

    @Test func pickerSelectConfirm() throws {
        let screen = try Self.result(HarnessScreen.self, "harness.screen-picker-confirm.json")
        let confirm = try #require(screen.picker?.onSelectConfirm)
        #expect(confirm.effect == .relaunch)
        #expect(confirm.text.hasPrefix("Restart"))
    }

    @Test func fieldActionConfirmDecodes() throws {
        let json = #"""
            {"id":"model","label":"Model","kind":"choice","value":"a","action":"set_model",\#
            "actionConfirm":{"text":"Restart?","effect":null}}
            """#
        let f = try JSONDecoder().decode(HarnessScreen.Settings.Field.self, from: Data(json.utf8))
        #expect(f.actionConfirm?.text == "Restart?")
        #expect(f.actionConfirm?.effect == nil)
    }
}

/// Error notices carry an optional reason; a notice without one decodes as before.
struct HarnessNoticeReasonTests {
    @Test func reasonDecodesAndIsOptional() throws {
        let with = try JSONDecoder().decode(HarnessItem.Notice.self, from: Data(#"{"level":"error","code":"error","reason":"auth","text":"401"}"#.utf8))
        #expect(with.reason == "auth")
        #expect(with.isError)
        let without = try JSONDecoder().decode(HarnessItem.Notice.self, from: Data(#"{"level":"info","code":"ended","text":"Aider exited."}"#.utf8))
        #expect(without.reason == nil)
        #expect(without.code == HarnessItem.Notice.ended)
        let json = String(decoding: try JSONEncoder().encode(without), as: UTF8.self)
        #expect(!json.contains("reason"))
    }
}
