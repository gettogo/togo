import Foundation
import Testing
@testable import Togo

/// The presentation coordinator's rules, on its plain state: never two sheets stacked unless the
/// policy nests them, a request never vanishes, only the user or the owning flow closes one.
struct PresentationLedgerTests {
    private func sheet(_ id: String, parent: String? = nil, nests: Bool = false, flow: String? = nil,
                       yields: Bool = false) -> PresentationRequest {
        PresentationRequest(id: id, name: id, kind: .sheet, parent: parent, nests: nests, flow: flow, yields: yields)
    }

    private func of(_ kind: PresentationKind, _ id: String, parent: String? = nil) -> PresentationRequest {
        PresentationRequest(id: id, name: id, kind: kind, parent: parent)
    }

    // MARK: One modal at a time

    @Test func aSheetShowsWhenNothingElseIsUp() {
        var ledger = PresentationLedger()
        let showsNow = ledger.request(sheet("a"))
        #expect(showsNow)
        #expect(ledger.shown.map(\.id) == ["a"])
        #expect(ledger.violations.isEmpty)
    }

    @Test func aSecondSheetFromElsewhereWaitsInsteadOfStacking() {
        var ledger = PresentationLedger()
        ledger.request(sheet("picker"))
        let showsNow = ledger.request(sheet("login"))
        #expect(!showsNow)
        #expect(ledger.modalDepth == 1)
        #expect(ledger.queued.map(\.id) == ["login"])
        #expect(ledger.violations.isEmpty)
    }

    @Test func aWaitingRequestShowsOnceTheSheetHasGone() {
        var ledger = PresentationLedger()
        ledger.request(sheet("picker"))
        ledger.request(sheet("login"))
        ledger.userDismissed("picker")
        // Still leaving: the slot is held until the dismissal finishes.
        #expect(!ledger.isShown("login"))
        ledger.didLeave("picker")
        #expect(ledger.isShown("login"))
        #expect(ledger.modalDepth == 1)
    }

    @Test func waitingRequestsShowInTheOrderTheyAsked() {
        var ledger = PresentationLedger()
        ledger.request(sheet("a"))
        ledger.request(sheet("b"))
        ledger.request(sheet("c"))
        ledger.withdraw("a")
        ledger.didLeave("a")
        #expect(ledger.shown.map(\.id) == ["b"])
        #expect(ledger.queued.map(\.id) == ["c"])
    }

    @Test func coversAndPopoversShareTheModalSlot() {
        var ledger = PresentationLedger()
        ledger.request(of(.cover, "workspace"))
        let showsNow = ledger.request(of(.popover, "palette"))
        #expect(!showsNow)
        let showsNow2 = ledger.request(sheet("form"))
        #expect(!showsNow2)
        #expect(ledger.shown.map(\.id) == ["workspace"])
        // A cover is a root, not a stacked sheet.
        #expect(ledger.modalDepth == 0)
    }

    // MARK: Nesting

    @Test func aSheetAskedFromInsideASheetStacksButIsAViolation() {
        var ledger = PresentationLedger()
        ledger.request(sheet("picker"))
        let showsNow = ledger.request(sheet("outcome", parent: "picker"))
        #expect(showsNow)
        #expect(ledger.modalDepth == 2)
        #expect(ledger.violations.map(\.rule) == [.stacked])
        #expect(ledger.violations.first?.name == "outcome")
    }

    @Test func aNestingThePolicyAllowsIsNoViolation() {
        var ledger = PresentationLedger()
        ledger.request(of(.cover, "workspace"))
        let showsNow = ledger.request(sheet("detail", parent: "workspace", nests: true))
        #expect(showsNow)
        #expect(ledger.violations.isEmpty)
    }

    @Test func oneSheetOverAFullScreenCoverIsTheRuleNotAStack() {
        var ledger = PresentationLedger()
        ledger.request(of(.cover, "workspace"))
        let showsNow = ledger.request(sheet("palette", parent: "workspace"))
        #expect(showsNow)
        #expect(ledger.violations.isEmpty)
        // A second sheet from the workspace waits for the first.
        let showsNow2 = ledger.request(sheet("detail", parent: "workspace"))
        #expect(!showsNow2)
        #expect(ledger.queued.map(\.id) == ["detail"])
    }

    @Test func questionsShowOverAnySheetOneAtATime() {
        var ledger = PresentationLedger()
        ledger.request(sheet("form"))
        let showsNow = ledger.request(of(.dialog, "discard", parent: "form"))
        #expect(showsNow)
        let showsNow2 = ledger.request(of(.alert, "error"))
        #expect(!showsNow2)
        ledger.userDismissed("discard")
        #expect(ledger.isShown("error"))
        #expect(ledger.violations.isEmpty)
    }

    @Test func anInspectorNeverWaits() {
        var ledger = PresentationLedger()
        ledger.request(sheet("form"))
        let showsNow = ledger.request(of(.inspector, "mission"))
        #expect(showsNow)
        #expect(ledger.modalDepth == 1)
    }

    // MARK: Nothing vanishes

    @Test func aHostThatGoesWhileItsSheetIsUpIsAViolation() {
        var ledger = PresentationLedger()
        ledger.request(sheet("login"))
        ledger.hostGone("login", stillWanted: true)
        #expect(ledger.violations.map(\.rule) == [.hostVanished])
        #expect(ledger.shown.isEmpty)
    }

    @Test func aHostThatGoesWhileItsRequestWaitsIsAViolation() {
        var ledger = PresentationLedger()
        ledger.request(sheet("a"))
        ledger.request(sheet("b"))
        ledger.hostGone("b", stillWanted: true)
        #expect(ledger.violations.map(\.rule) == [.hostVanished])
    }

    @Test func aHostThatGoesAfterItsFlowWithdrewIsFine() {
        var ledger = PresentationLedger()
        ledger.request(sheet("a"))
        ledger.withdraw("a")
        ledger.hostGone("a", stillWanted: false)
        #expect(ledger.violations.isEmpty)
    }

    @Test func whatASheetHeldUpGoesWithItWithoutAViolation() {
        var ledger = PresentationLedger()
        ledger.request(sheet("outer"))
        ledger.request(of(.dialog, "inner", parent: "outer"))
        ledger.hostGone("outer", stillWanted: false)
        #expect(ledger.entries.isEmpty)
        #expect(ledger.violations.isEmpty)
    }

    @Test func askingAgainWhileStillLeavingIsANewRequest() {
        var ledger = PresentationLedger()
        ledger.request(sheet("a"))
        ledger.userDismissed("a")
        // The same host presents it again; SwiftUI sequences its own exit and entry.
        let showsNow = ledger.request(sheet("a"))
        #expect(showsNow)
        // The old exit reporting late does not take the new one down.
        ledger.didLeave("a")
        #expect(ledger.isShown("a"))
    }

    @Test func askingAgainWhileShownIsANoOp() {
        var ledger = PresentationLedger()
        ledger.request(sheet("a"))
        let showsNow = ledger.request(sheet("a"))
        #expect(showsNow)
        #expect(ledger.entries.count == 1)
    }

    // MARK: Who closes

    @Test func theUserAndTheOwnerCloseAndAreRecorded() {
        var ledger = PresentationLedger()
        ledger.request(sheet("a"))
        ledger.request(sheet("b"))
        ledger.userDismissed("a")
        ledger.didLeave("a")
        ledger.withdraw("b")
        #expect(ledger.dismissals.map(\.by) == [.user, .owner])
    }

    @Test func aLaterStepOfTheSameFlowReplacesTheSheet() {
        var ledger = PresentationLedger()
        ledger.request(sheet("signIn", flow: "signIn"))
        let showsNow = ledger.request(sheet("outcome", flow: "signIn"))
        #expect(!showsNow)
        #expect(ledger.phase(of: "signIn") == .leaving)
        #expect(ledger.retired == ["signIn"])
        ledger.didLeave("signIn")
        #expect(ledger.shown.map(\.id) == ["outcome"])
        #expect(ledger.violations.isEmpty)
        ledger.acknowledgeRetired("signIn")
        #expect(ledger.retired.isEmpty)
    }

    @Test func anotherFlowNeverReplacesASheet() {
        var ledger = PresentationLedger()
        ledger.request(sheet("signIn", flow: "signIn"))
        let showsNow = ledger.request(sheet("settings", flow: "settings"))
        #expect(!showsNow)
        #expect(ledger.isShown("signIn"))
        #expect(ledger.retired.isEmpty)
    }

    @Test func aSideContextStepsAsideAndComesBack() {
        var ledger = PresentationLedger()
        ledger.request(sheet("mission", yields: true))
        let showsNow = ledger.request(sheet("detail"))
        #expect(!showsNow)
        #expect(ledger.phase(of: "mission") == .leaving)
        ledger.didLeave("mission")
        #expect(ledger.shown.map(\.id) == ["detail"])
        #expect(ledger.queued.map(\.id) == ["mission"])
        ledger.userDismissed("detail")
        ledger.didLeave("detail")
        #expect(ledger.shown.map(\.id) == ["mission"])
        #expect(ledger.violations.isEmpty)
    }

    @Test func aSideContextClosedWhileSteppedAsideStaysClosed() {
        var ledger = PresentationLedger()
        ledger.request(sheet("mission", yields: true))
        ledger.request(sheet("detail"))
        ledger.withdraw("mission")
        ledger.didLeave("mission")
        #expect(ledger.phase(of: "mission") == nil)
        #expect(ledger.isShown("detail"))
    }

    @Test func aSheetTurningIntoAPopoverOnResizeIsTheSameRequest() {
        var ledger = PresentationLedger()
        ledger.request(sheet("palette"))
        let showsNow = ledger.request(of(.popover, "palette"))
        #expect(showsNow)
        #expect(ledger.violations.isEmpty)
        let showsNow2 = ledger.request(of(.alert, "palette"))
        #expect(showsNow2)
        #expect(ledger.violations.map(\.rule) == [.kindChanged])
    }
}

/// The coordinator over the ledger: violations reach the log hook, a dismissal that never
/// reports still frees the slot.
@MainActor
struct PresentationCoordinatorTests {
    @Test func violationsReachTheHook() {
        let coordinator = PresentationCoordinator(strict: false)
        var seen: [PresentationViolation.Rule] = []
        coordinator.onViolation = { seen.append($0.rule) }
        coordinator.request(PresentationRequest(id: "a", name: "a", kind: .sheet, parent: nil))
        coordinator.request(PresentationRequest(id: "b", name: "b", kind: .sheet, parent: "a"))
        #expect(seen == [.stacked])
        #expect(coordinator.summary.hasPrefix("violations=1 depth=2"))
    }

    @Test func aDismissalThatNeverReportsFreesTheSlot() async throws {
        let coordinator = PresentationCoordinator(strict: false)
        coordinator.request(PresentationRequest(id: "a", name: "a", kind: .sheet, parent: nil))
        coordinator.request(PresentationRequest(id: "b", name: "b", kind: .sheet, parent: nil))
        coordinator.userDismissed("a")
        #expect(!coordinator.isShown("b"))
        try await Task.sleep(for: PresentationCoordinator.leaveTimeout + .milliseconds(300))
        #expect(coordinator.isShown("b"))
    }

    @Test func theStyleComesFromThePolicyNeverTheDevice() {
        let coordinator = PresentationCoordinator(strict: false)
        #expect(coordinator.style(for: .browse, sizeClass: .regular, containerWidth: 475) == .fullHeightSheet)
        #expect(coordinator.style(for: .browse, sizeClass: .regular, containerWidth: 956) == .pageSheet)
        #expect(coordinator.style(for: .sideContext, sizeClass: .regular, containerWidth: 1194, hostsInspector: true) == .inspector)
        #expect(PresentationCoordinator.kind(of: .inspector) == .inspector)
        #expect(PresentationCoordinator.kind(of: .popover) == .popover)
        #expect(PresentationCoordinator.kind(of: .mediumSheet) == .sheet)
    }
}
