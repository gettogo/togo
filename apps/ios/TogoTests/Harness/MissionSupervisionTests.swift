import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor struct MissionSupervisionTests {
    private func mission(_ sections: [HarnessSection]) -> HarnessMission {
        .init(rev: 1, summary: .init(), sections: sections)
    }

    @Test(arguments: [MissionSectionKind.agents, .background])
    func runningRowsLeadWithoutDroppingOrChangingSourceRows(_ kind: MissionSectionKind) {
        // Old helpers retain these completed rows; their ids, routes and actions still work.
        let rows: [HarnessRow] = [
            .init(id: "old", title: "Earlier work", state: .ok),
            .init(id: "failed", title: "Failed work", state: .failed),
            .init(id: "live", title: "Current work", state: .running, open: .init(kind: .chat, ref: "child")),
            .init(id: "unknown", title: "Future state", state: "future-state"),
            .init(id: "wait", title: "Waiting work", state: .waiting),
            .init(id: "ready", title: "Shell", state: "ready"),
        ]
        let source = mission([.init(kind: kind, rows: rows)])
        let section = MissionLayout.presentedSections(source, descriptor: nil).first
        #expect(section?.rows.map(\.id) == ["live", "wait", "old", "failed", "unknown", "ready"])
        #expect(Set(section?.rows ?? []) == Set(rows))
        #expect(source.sections[0].rows == rows)
        #expect(section.map { MissionLayout.headerCount($0, summary: .init(agents: 99, background: 99)) } == 2)
    }

    @Test func emptySectionsHideButStructuredStatusAndRealPagesStay() {
        let all = mission(MissionSectionKind.order.map { .init(kind: $0) })
        #expect(MissionLayout.presentedSections(all, descriptor: nil).isEmpty)
        let status = HarnessStatus(model: "Current model")
        #expect(MissionLayout.presentedSections(mission([.init(kind: .status, status: status)]), descriptor: nil)
            .first?.status == status)
        let descriptor = HarnessDescriptor(
            adapter: .init(id: "omp", name: "OMP", harnessVersion: "18", revision: 1),
            capabilities: [HarnessCapability.status],
            screens: [.init(id: "settings", primitive: .settings, title: "Settings", section: "status")]
        )
        #expect(MissionLayout.presentedSections(all, descriptor: descriptor).map(\.kind) == [.status])
        #expect(MissionLayout.presentedSections(mission([.init(kind: .agents, rows: [
            .init(id: "a", title: "Agent", state: .running)
        ])]), descriptor: descriptor).isEmpty)
    }

    @Test func ordinaryRemovesHideTheLastRowAndReplacementRestoresTheSection() {
        var state = MissionState()
        state.load(mission([.init(kind: .agents, rows: [.init(id: "a", title: "Agent", state: .running)])]))
        #expect(MissionLayout.presentedSections(state.mission, descriptor: nil).count == 1)
        #expect(state.apply(.init(rev: 2, ops: [.init(op: .remove, section: .agents, id: "a")])) == .applied)
        #expect(MissionLayout.presentedSections(state.mission, descriptor: nil).isEmpty)
        let failed = HarnessRow(id: "b", title: "Retained failure", state: .failed)
        #expect(state.apply(.init(rev: 3, ops: [.init(op: .replace, section: .agents, rows: [failed])])) == .applied)
        let section = MissionLayout.presentedSections(state.mission, descriptor: nil).first
        #expect(section?.rows == [failed])
        #expect(section.map { MissionLayout.headerCount($0, summary: state.summary) } == 0)
        #expect(MissionActivity.countLabel(0) == "0 running")
    }

    @Test func otherSectionsKeepTheirMeaningAndSourceOrder() {
        let rows: [HarnessRow] = [.init(id: "done", title: "Done", state: .done), .init(id: "doing", title: "Doing", state: .active)]
        let sections = MissionLayout.presentedSections(mission([
            .init(kind: .plan, rows: rows), .init(kind: .needs, rows: rows), .init(kind: .changes, rows: rows)
        ]), descriptor: nil)
        #expect(sections.map(\.kind) == [.needs, .plan, .changes])
        #expect(sections.allSatisfy { $0.rows == rows })
        #expect(MissionLayout.headerCount(sections[0], summary: .init(needsYou: 3)) == 3)
        #expect(MissionLayout.headerCount(sections[1], summary: .init()) == 0)
        #expect(MissionLayout.headerCount(sections[2], summary: .init()) == 2)
    }

    @Test(arguments: [
        ("running", "Running"), ("waiting", "Waiting"), ("ready", "Ready"), ("idle", "Idle"),
        ("ok", "Finished"), ("done", "Finished"), ("exited", "Exited"), ("failed", "Failed"),
        ("error", "Failed"), ("cancelled", "Stopped"), ("killed", "Stopped"), ("timeout", "Timed out"),
        ("denied", "Denied"), ("interrupted", "Interrupted"), ("active", "Active"), ("detached", "Detached"),
        ("paused", "Paused"), ("not_ready", "Not ready"), ("", "Unknown state"), ("<state>", "Unknown state"),
    ])
    func everyRetainedStateHasTruthfulWords(_ pair: (String, String)) {
        #expect(MissionActivity.label(HarnessState(rawValue: pair.0)) == pair.1)
    }

    @Test(arguments: [true, false])
    func sessionMenuKeepsActionsWithOrWithoutTheDedicatedToolbarButton(_ missionInBar: Bool) {
        let choices = MissionMenu.items(needsYou: 1, working: true, canCompact: true)
        let entries = MissionMenu.sessionEntries(choices, missionInBar: missionInBar)
        #expect(entries.showsToggle == !missionInBar)
        #expect(entries.actions == [.goToRequest, .stop, .compact])
        let idle = MissionMenu.sessionEntries(MissionMenu.items(needsYou: 0, working: false, canCompact: false), missionInBar: missionInBar)
        #expect(idle.actions.isEmpty)
        #expect(idle.showsToggle == !missionInBar)
    }

    @Test func onlyKnownRunningRowsWithAStartHaveElapsedTime() {
        let start = WireDate(Date(timeIntervalSince1970: 1_700_000_000))
        for state: HarnessState in [.running, .waiting, .failed, .ok, .cancelled, "exited", "ready", "future"] {
            let row = HarnessRow(id: "a", title: "Work", state: state, startedAt: start)
            #expect(MissionActivity.clockStart(row) == (MissionActivity.isRunning(state) ? start.date : nil))
            #expect(MissionActivity.clockStart(.init(id: "a", title: "No start", state: state)) == nil)
        }
    }

    @Test(arguments: [CGFloat(320), CGFloat(700)], [DynamicTypeSize.large, .accessibility5])
    func activityHasItsOwnWrappingLine(width: CGFloat, type: DynamicTypeSize) {
        let row = HarnessRow(id: "agent", title: "Review a long multilingual workspace 日本語 with several independent tasks", state: .failed)
        func size(_ view: some View) -> CGSize {
            let host = UIHostingController(rootView: view.environment(\.dynamicTypeSize, type))
            return host.sizeThatFits(in: .init(width: width, height: .greatestFiniteMagnitude))
        }
        let plain = size(MissionRowView(row: row, opens: true))
        let supervised = size(MissionRowView(row: row, opens: true, showsActivity: true))
        #expect(supervised.width <= width + 1)
        #expect(supervised.height > plain.height)
        let background = size(MissionBackgroundRow(row: row))
        #expect(background.width <= width + 1)
        #expect(background.height >= 44)
    }
}
