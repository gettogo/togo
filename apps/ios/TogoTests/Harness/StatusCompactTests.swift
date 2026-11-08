import Foundation
import Testing
import TogoKit
@testable import Togo

/// The Status section: no Usage or Compact rows, Compact on the context meter only with the
/// session's `compact` action, "Compacting" until the meter drops, and the honest line before
/// the first status arrives.
@MainActor
struct StatusCompactTests {
    private static let claude = HarnessDescriptor(
        adapter: .init(id: "claude", name: "Claude Code", harnessVersion: "2.1.319", revision: 1),
        capabilities: [HarnessCapability.status, HarnessCapability.compact],
        screens: [
            .init(id: "usage", primitive: .status, title: "Usage", section: "status"),
            .init(id: "compact", primitive: .confirm, title: "Compact", section: "status"),
            .init(id: "resume", primitive: .picker, title: "Resume", section: "more"),
        ]
    )

    private static let compact = HarnessActionRef(id: "compact", label: "Compact", confirm: "Compact the conversation now?")

    @Test func theStatusSectionListsNeitherUsageNorCompact() {
        #expect(MissionLayout.statusScreens(Self.claude).isEmpty)
        #expect(MissionLayout.statusScreens(nil).isEmpty)
    }

    @Test func aRealPageInTheStatusSectionStays() {
        let omp = HarnessDescriptor(
            adapter: .init(id: "omp", name: "omp", harnessVersion: "1", revision: 1),
            capabilities: [HarnessCapability.status],
            screens: [.init(id: "compaction", primitive: .settings, title: "Compaction", section: "status")]
        )
        #expect(MissionLayout.statusScreens(omp).map(\.id) == ["compaction"])
    }

    @Test func compactSitsOnTheContextMeterOnlyWithTheAction() {
        let with = HarnessStatus(contextPercent: 62, actions: [Self.compact])
        #expect(StatusLogic.compactAction(with)?.id == "compact")
        #expect(StatusLogic.otherActions(with).isEmpty)
        let without = HarnessStatus(contextPercent: 62)
        #expect(StatusLogic.compactAction(without) == nil)
    }

    @Test func ompsOneShotCompactIsTheSameButton() {
        let omp = HarnessStatus(contextPercent: 40, actions: [HarnessActionRef(id: "snapcompact", label: "Compact")])
        #expect(StatusLogic.compactAction(omp)?.id == "snapcompact")
    }

    @Test func withoutAContextReadingCompactStaysInTheActionRow() {
        let status = HarnessStatus(actions: [Self.compact, HarnessActionRef(id: "restart", label: "Restart")])
        #expect(StatusLogic.compactAction(status) == nil)
        #expect(StatusLogic.otherActions(status).map(\.id) == ["compact", "restart"])
    }

    @Test func otherActionsKeepTheirRow() {
        let status = HarnessStatus(contextPercent: 10, actions: [Self.compact, HarnessActionRef(id: "restart", label: "Restart")])
        #expect(StatusLogic.otherActions(status).map(\.id) == ["restart"])
    }

    @Test func compactingLastsUntilTheMeterDrops() {
        #expect(StatusLogic.compacting(running: true, from: nil, percent: 62))
        #expect(StatusLogic.compacting(running: false, from: 62, percent: 62))
        #expect(StatusLogic.compacting(running: false, from: 62, percent: 64))
        #expect(!StatusLogic.compacting(running: false, from: 62, percent: 18))
        #expect(!StatusLogic.compacting(running: false, from: nil, percent: 62))
    }

    @Test func beforeTheFirstStatusTheSectionSaysWhenItWillShow() {
        #expect(MissionCopy.statusPending("Claude") == "Usage shows after Claude's next reply")
        #expect(MissionCopy.statusPending(nil) == "Usage shows after the next reply")
        #expect(MissionCopy.statusPending("") == "Usage shows after the next reply")
    }

    @Test func theLongPressCompactUsesTheStatusActionOrTheCapability() {
        let store = HarnessSessionStore(subject: ChatTests.subject, services: FakeHarnessServices(descriptor: Self.claude), cached: Self.claude)
        let ref = MissionWorkspaceModel.compactRef(store)
        #expect(ref?.id == HarnessCapability.compact)
        // It asks first, like the Status block's own button.
        #expect(ref?.confirm?.isEmpty == false)
        let plain = HarnessSessionStore(subject: ChatTests.subject, services: FakeHarnessServices(descriptor: ChatTests.claude), cached: ChatTests.claude)
        #expect(MissionWorkspaceModel.compactRef(plain) == nil)
    }

    /// "ctx 0%" before the first reply is unknown, not empty: the meter and the roster leave it out.
    @Test func zeroContextIsUnknown() {
        #expect(StatusLogic.knownContext(0) == nil)
        #expect(StatusLogic.knownContext(0.2) == nil)
        #expect(StatusLogic.knownContext(.nan) == nil)
        #expect(StatusLogic.knownContext(42) == 42)
        #expect(!RosterMission.text(meta: SessionMeta(contextPercent: 0), mission: nil).contains("ctx"))
    }
}
