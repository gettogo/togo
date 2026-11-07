import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct SignInCardTests {
    private func card(_ signIn: SignInStep?, options: [ApprovalOption] = [], allowText: Bool = false) -> RequestCard {
        RequestCard(
            id: "r_abcdefghij", hostID: Samples.studio, sessionID: "s_abcdefghij", bib: "01", agentName: "Amp",
            title: "Confirm code QRLJ-CBJZ at ampcode.com", detail: nil, options: options, allowText: allowText,
            phase: .open, signIn: signIn
        )
    }

    @Test func deviceCodeOffersCopyAndOpenWithoutYesOrNo() throws {
        let step = SignInStep(url: URL(string: "https://ampcode.com/auth/cli/device?user_code=QRLJ-CBJZ"), code: "QRLJ-CBJZ")
        let request = PromptFeed.request(card(step), row: nil, machineName: "Studio")
        #expect(!request.asksYesOrNo)
        #expect(!request.allowsReply)
        #expect(request.hostID == Samples.studio)
        let offer = try #require(SignInOffer(request.signIn))
        #expect(offer.code == "QRLJ-CBJZ")
        #expect(offer.page?.host() == "ampcode.com")
        #expect(offer.copiesOnOpen)
        #expect(!offer.secret)
    }

    @Test func secretPromptTakesAMaskedReply() throws {
        let step = SignInStep(url: URL(string: "https://accounts.google.com/o/oauth2/v2/auth?client_id=x"), secret: true)
        let request = PromptFeed.request(card(step, allowText: true), row: nil, machineName: "Studio")
        #expect(request.allowsReply)
        #expect(!request.asksYesOrNo)
        let offer = try #require(SignInOffer(request.signIn))
        #expect(offer.secret)
        #expect(!offer.copiesOnOpen)
        #expect(offer.replyLabel == SignInCopy.pasteKey)
    }

    @Test func emptyStepIsNoOffer() {
        #expect(SignInOffer(SignInStep()) == nil)
        #expect(SignInOffer(nil) == nil)
    }

    @Test func requestsWithoutSignInKeepYesAndNo() {
        let options = [ApprovalOption(n: 1, label: "Yes", role: .approve), ApprovalOption(n: 2, label: "No", role: .deny)]
        let request = PromptFeed.request(card(nil, options: options), row: nil, machineName: "Studio")
        #expect(request.asksYesOrNo)
        #expect(request.signIn == nil)
    }

    @Test func codeIsSpelledForVoiceOver() {
        #expect(SignInCopy.codeLabel("AB-1") == "Code A B - 1")
    }
}
