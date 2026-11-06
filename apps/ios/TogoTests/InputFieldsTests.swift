import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor
struct InputFieldsTests {
    @Test func userLeadsToTheSecretFieldWhenTheSignInHasOne() {
        var draft = MachineDraft()
        draft.auth = .deviceKey
        #expect(draft.field(after: .user) == nil)
        draft.auth = .password
        #expect(draft.field(after: .user) == .secret)
        draft.auth = .importedKey
        #expect(draft.field(after: .user) == .secret)
        #expect(draft.field(after: .secret) == nil)
        #expect(draft.field(after: .port) == .user)
    }

    @Test func jumpHostRoundTripsThroughTheForm() {
        let cases = [
            HostEndpoint(host: "bastion.example.com", port: 22, user: "sam"),
            HostEndpoint(host: "bastion.example.com", port: 2222, user: "sam"),
            HostEndpoint(host: "fe80::1", port: 2222, user: "sam"),
            HostEndpoint(host: "fd7a:115c::1", port: 22, user: "sam"),
        ]
        for endpoint in cases {
            #expect(MachineDraft.endpoint(MachineDraft.jumpText(endpoint)) == endpoint)
        }
        #expect(MachineDraft.jumpText(cases[2]) == "sam@[fe80::1]:2222")
    }

    @Test func composeFieldKeepsDashesAndQuotesAsTyped() {
        let view = PastingTextView(placeholder: "Message")
        #expect(view.smartDashesType == .no)
        #expect(view.smartQuotesType == .no)
        #expect(view.smartInsertDeleteType == .no)
    }

    @Test func commitMessageStraightensQuotesAndTypedDashes() {
        #expect(CommitMessageText.straightened("fix \u{201C}a\u{201D} it\u{2019}s", previous: "fix \u{201C}a\u{201D} it") == "fix \"a\" it's")
        #expect(CommitMessageText.straightened("run \u{2014}", previous: "run -") == "run --")
        #expect(CommitMessageText.straightened("a \u{2014} b", previous: "") == "a \u{2014} b")
    }
}
