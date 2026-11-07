import Foundation
import Testing
import TogoKit
@testable import Togo

/// The rules behind the list and swap choreographies in mission control, the roster and the
/// prompt bar (motion spec §2.3, §2.5): what counts as a change of identity, what lands at
/// once, and how the bar sequences one request replacing another.
@MainActor
struct ListMotionTests {
    // MARK: Identity

    @Test func onlyIdentityChangesAnimate() {
        let key: ([String]) -> [String] = { $0.map { String($0.prefix(1)) } }
        #expect(IdentityMotion.changesIdentity(from: ["a1"], to: ["a1", "b1"], key: key))
        #expect(!IdentityMotion.changesIdentity(from: ["a1"], to: ["a2"], key: key))
    }

    @Test func aQuietChangeLandsAtOnceEvenWhenRowsChange() {
        struct List { var ids: [Int]; var query: String }
        let changed = IdentityMotion.changesIdentity(
            from: List(ids: [1, 2], query: ""), to: List(ids: [1], query: "ap"), key: \.ids, quiet: { $0.query }
        )
        #expect(!changed)
        let removed = IdentityMotion.changesIdentity(
            from: List(ids: [1, 2], query: "ap"), to: List(ids: [1], query: "ap"), key: \.ids, quiet: { $0.query }
        )
        #expect(removed)
    }

    @Test func caseNamesIgnorePayloads() {
        enum Phase { case loading, failed(String), loaded(Int) }
        #expect(CaseName.of(Phase.failed("a")) == CaseName.of(Phase.failed("b")))
        #expect(CaseName.of(Phase.loading) != CaseName.of(Phase.failed("a")))
        #expect(CaseName.of(Phase.loaded(1)) == "loaded")
    }

    // MARK: Mission control

    private func section(_ kind: MissionSectionKind, _ rows: [HarnessRow]) -> HarnessSection {
        HarnessSection(kind: kind, rows: rows)
    }

    @Test func missionShapeFollowsRowsNotTheirWords() {
        let a = HarnessRow(id: "a", title: "Explore", state: .running)
        var renamed = a
        renamed.title = "Explore the repo"
        let b = HarnessRow(id: "b", title: "Test", state: .running)
        let before = MissionLayout.shape([section(.agents, [a])])
        #expect(MissionLayout.shape([section(.agents, [renamed])]) == before)
        #expect(MissionLayout.shape([section(.agents, [a, b])]) != before)
        #expect(MissionLayout.shape([section(.agents, [a]), section(.ports, [])]) != before)
    }

    @Test func missionShapeMovesWhenARowGainsALine() {
        let row = HarnessRow(id: "a", title: "Build", state: .running)
        var progressing = row
        progressing.progress = 0.4
        var further = progressing
        further.progress = 0.8
        #expect(MissionLayout.shape([section(.background, [row])]) != MissionLayout.shape([section(.background, [progressing])]))
        #expect(MissionLayout.shape([section(.background, [progressing])]) == MissionLayout.shape([section(.background, [further])]))
    }

    // MARK: Prompt bar

    private static let tab = TabID(hostID: UUID(), sessionID: "s")

    private static func request(_ id: String) -> PromptRequest {
        PromptRequest(
            id: id, sessionID: "s", bib: "04", sessionName: "app", agentName: "Claude Code",
            machineName: "Studio", question: "Run?", command: nil
        )
    }

    @Test func aRequestReplacingAnotherConcealsBeforeItRises() {
        let a = PromptBarModel.Stage.request(Self.request("a"), position: 1, count: 2)
        let b = PromptBarModel.Stage.request(Self.request("b"), position: 1, count: 1)
        let change = PromptBarModel.change(from: a, to: b)
        #expect(PromptBarSwap.plan(change, from: a, to: b, reduce: false) == .concealThenRise(.dropIn))
        #expect(PromptBarSwap.plan(change, from: a, to: b, reduce: true) == .inPlace(.dropIn))
    }

    @Test func theBarArrivingOrLeavingIsOneTransaction() {
        let a = PromptBarModel.Stage.request(Self.request("a"), position: 1, count: 1)
        #expect(PromptBarSwap.plan(PromptBarModel.change(from: .hidden, to: a), from: .hidden, to: a, reduce: false) == .inPlace(.dropIn))
        #expect(PromptBarSwap.plan(PromptBarModel.change(from: a, to: .hidden), from: a, to: .hidden, reduce: false) == .inPlace(.collapse))
        #expect(PromptBarSwap.plan(.none, from: a, to: a, reduce: false) == .instant)
        #expect(PromptBarSwap.plan(.resize, from: a, to: a, reduce: false) == .inPlace(.chrome))
    }

    @Test func aNewerSwapMakesTheOlderStale() {
        var swaps = PromptBarSwap()
        let first = swaps.begin()
        let second = swaps.begin()
        #expect(!swaps.isCurrent(first))
        #expect(swaps.isCurrent(second))
    }

    @Test func sendingOnlyDimsTheAnswers() {
        #expect(PromptOutcomeMotion.isSmall(from: .open, to: .sending))
        #expect(PromptOutcomeMotion.isSmall(from: .sending, to: .open))
        #expect(!PromptOutcomeMotion.isSmall(from: .sending, to: .done(.approved)))
        #expect(!PromptOutcomeMotion.isSmall(from: .open, to: .failed("Lost")))
    }
}
