import Foundation
import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// Mission control: the summary's words, which sections show and in what order, how it is
/// presented per width, the session menu's screens, and what the roster says from a session's
/// mission summary.
@MainActor
struct MissionTests {
    // MARK: Pill

    @Test func pillJoinsCountsWithSpacesNotDots() {
        let summary = MissionSummary(agents: 2, background: 1, plan: .init(done: 3, total: 7), working: true)
        let text = MissionText.pill(summary)
        #expect(text == "2 agents  1 shell  plan 3/7")
        #expect(!text.contains("·"))
    }

    @Test func pillPluralises() {
        #expect(MissionText.pill(MissionSummary(agents: 1)) == "1 agent")
        #expect(MissionText.pill(MissionSummary(background: 3)) == "3 shells")
        #expect(MissionText.pill(MissionSummary(needsYou: 1)) == "1 needs you")
        #expect(MissionText.pill(MissionSummary(needsYou: 2, agents: 1)) == "2 need you  1 agent")
    }

    @Test func pillLeavesOutZeroCountsAndEmptyPlans() {
        #expect(MissionText.pill(MissionSummary(agents: 0, background: 2, plan: .init(done: 0, total: 0))) == "2 shells")
    }

    @Test func pillWithNothingCountedSaysWorkingOrAllClear() {
        #expect(MissionText.pill(MissionSummary(working: true)) == "Working")
        #expect(MissionText.pill(MissionSummary()) == "All clear")
    }

    @Test func pillSpeaksThePlan() {
        let spoken = MissionText.spoken(MissionSummary(agents: 2, plan: .init(done: 3, total: 7)))
        #expect(spoken == "2 agents, plan 3 of 7 done")
    }

    @Test func planFractionIsClampedAndNilWithoutSteps() {
        #expect(MissionText.planFraction(MissionSummary(plan: .init(done: 3, total: 6))) == 0.5)
        #expect(MissionText.planFraction(MissionSummary(plan: .init(done: 9, total: 6))) == 1)
        #expect(MissionText.planFraction(MissionSummary(plan: .init(done: 0, total: 0))) == nil)
        #expect(MissionText.planFraction(nil) == nil)
    }

    // MARK: Sections

    private static let all: [MissionSectionKind] = [.status, .ports, .changes, .plan, .background, .agents, .needs]

    private func mission(_ kinds: [MissionSectionKind]) -> HarnessMission {
        HarnessMission(rev: 1, summary: MissionSummary(), sections: kinds.map { HarnessSection(kind: $0) })
    }

    @Test func sectionsComeInTheFixedOrderWhateverTheSnapshotSent() {
        let sections = MissionLayout.sections(mission(Self.all), capabilities: nil)
        #expect(sections.map(\.kind) == MissionSectionKind.order)
    }

    @Test func sectionsHideWhenTheHarnessLacksTheCapability() {
        let caps: Set<String> = [HarnessCapability.plan, HarnessCapability.status, HarnessCapability.agents]
        let sections = MissionLayout.sections(mission(Self.all), capabilities: caps)
        #expect(sections.map(\.kind) == [.agents, .plan, .status])
    }

    @Test func needsShowsForApprovalsOrQuestions() {
        let approvals = MissionLayout.sections(mission([.needs]), capabilities: [HarnessCapability.needsApprovals])
        let questions = MissionLayout.sections(mission([.needs]), capabilities: [HarnessCapability.needsQuestions])
        let neither = MissionLayout.sections(mission([.needs]), capabilities: [HarnessCapability.chat])
        #expect(approvals.map(\.kind) == [.needs])
        #expect(questions.map(\.kind) == [.needs])
        #expect(neither.isEmpty)
    }

    @Test func aSupportedSectionTheSnapshotLacksStaysHidden() {
        let caps: Set<String> = [HarnessCapability.changes, HarnessCapability.ports]
        #expect(MissionLayout.sections(mission([.ports]), capabilities: caps).map(\.kind) == [.ports])
    }

    @Test func unknownKindsAndNoMissionShowNothing() {
        #expect(MissionLayout.sections(mission(["future"]), capabilities: nil).isEmpty)
        #expect(MissionLayout.sections(nil, capabilities: nil).isEmpty)
    }

    @Test func statusScreensAreRealPagesInTheStatusSection() {
        let descriptor = HarnessDescriptor(
            adapter: .init(id: "claude", name: "Claude Code", harnessVersion: "2.1.319", revision: 1),
            capabilities: [HarnessCapability.status],
            screens: [
                .init(id: "usage", primitive: .status, title: "Usage", section: "status"),
                .init(id: "compaction", primitive: .settings, title: "Compaction", section: "status"),
                .init(id: "model", primitive: .picker, title: "Model", section: "composer"),
            ]
        )
        #expect(MissionLayout.statusScreens(descriptor).map(\.id) == ["compaction"])
        #expect(MissionLayout.statusScreens(nil).isEmpty)
    }

    @Test func theSessionMenuListsTheMoreScreens() {
        let descriptor = HarnessDescriptor(
            adapter: .init(id: "opencode", name: "OpenCode", harnessVersion: "1", revision: 1),
            capabilities: [HarnessCapability.chat],
            screens: [
                .init(id: "resume", primitive: .picker, title: "Resume", section: "more"),
                .init(id: "mcp", primitive: .liveList, title: "MCP servers", section: "more"),
                .init(id: "providers", primitive: .picker, title: "Providers", section: "more"),
                .init(id: "model", primitive: .picker, title: "Model", section: "composer"),
                .init(id: "usage", primitive: .status, title: "Usage", section: "status"),
            ]
        )
        #expect(MissionLayout.moreScreens(descriptor).map(\.id) == ["resume", "mcp", "providers"])
        #expect(MissionLayout.moreScreens(nil).isEmpty)
        // Each item says it opens a screen.
        #expect(MissionCopy.opensScreen("Resume") == "Resume…")
        #expect(MissionSessionSection.symbol("mcp") == "server.rack")
    }

    @Test func onlyTheNeedsCountRolls() {
        #expect(MissionSectionHeader.countRolls(.needs))
        for kind in [MissionSectionKind.agents, .background, .plan, .changes, .ports, .status] {
            #expect(!MissionSectionHeader.countRolls(kind))
        }
    }

    @Test func aSessionWithoutMissionControlSaysTerminalOnly() {
        #expect(MissionCopy.unavailableDetail == "Terminal only")
    }

    @Test func planRowsBecomeAChecklist() {
        let rows = [
            HarnessRow(id: "a", title: "Read", state: .done),
            HarnessRow(id: "b", title: "Fix", state: .active, children: [HarnessRow(id: "b1", title: "Test", state: .pending)]),
        ]
        let fromRows = MissionLayout.checklist(rows, summary: nil)
        #expect(fromRows.items.map(\.id) == ["a", "b"])
        #expect(fromRows.items[1].children?.map(\.id) == ["b1"])
        #expect(fromRows.done == 1)
        #expect(fromRows.total == 3)
        let fromSummary = MissionLayout.checklist(rows, summary: .init(done: 2, total: 5))
        #expect(fromSummary.done == 2)
        #expect(fromSummary.total == 5)
    }

    @Test func rowsOpenTheirOwnTargetOrTheSectionsDefault() {
        let own = HarnessRow(id: "x", title: "dev", state: .running, open: .init(kind: .url, ref: "3000"))
        #expect(MissionLayout.target(for: own, in: .background) == .init(kind: .url, ref: "3000"))
        let bare = HarnessRow(id: "a1", title: "explorer", state: .running)
        #expect(MissionLayout.target(for: bare, in: .agents) == .init(kind: .chat, ref: "a1"))
        #expect(MissionLayout.target(for: bare, in: .background) == .init(kind: .terminal, ref: "a1"))
        #expect(MissionLayout.target(for: bare, in: .changes) == .init(kind: .diff, ref: "a1"))
        #expect(MissionLayout.target(for: bare, in: .plan) == nil)
    }

    // MARK: Presentation

    @Test func presentationFollowsTheWidthNotTheDevice() {
        #expect(MissionPresentation.choose(.compact) == .sheet)
        #expect(MissionPresentation.choose(.regular) == .inspector)
        #expect(MissionPresentation.choose(nil) == .sheet)
    }

    // MARK: Roster

    @Test func rosterMissionLineSaysModelContextAndCounts() {
        let meta = SessionMeta(model: "opus", contextPercent: 41.6)
        let mission = MissionSummary(needsYou: 1, agents: 2, background: 1, plan: .init(done: 3, total: 7))
        #expect(RosterMission.text(meta: meta, mission: mission) == "opus  ctx 42%  2 agents  1 shell  plan 3/7")
        #expect(RosterMission.spoken(meta: meta, mission: mission)
            == "opus, context 42 percent full, 2 agents, 1 shell, plan 3 of 7 done")
    }

    @Test func rosterMissionLineIsEmptyWithNothingToSay() {
        #expect(RosterMission.text(meta: SessionMeta(), mission: nil).isEmpty)
        #expect(RosterMission.text(meta: SessionMeta(model: "  "), mission: MissionSummary(working: true)).isEmpty)
        #expect(RosterMission.text(meta: SessionMeta(contextPercent: 120), mission: nil) == "ctx 100%")
    }

    private func machine(_ rows: [SessionRow]) -> MachineSection {
        MachineSection(
            id: Samples.studio, name: "Studio", symbol: .desktop, reachability: .online,
            helper: .present(version: "0.3.0", live: true), sessions: rows, lastSeen: Samples.now
        )
    }

    @Test func rosterLinesCarryTheirMissionAndItsNeedsCount() {
        let api = Samples.row(bib: "01", name: "api", status: .working, cwd: "/a")
        let web = Samples.row(bib: "02", name: "web", status: .idle, cwd: "/b")
        let missions = [
            TabID(hostID: Samples.studio, sessionID: api.id): MissionSummary(needsYou: 1, agents: 2),
            TabID(hostID: Samples.studio, sessionID: web.id): MissionSummary(background: 1),
        ]
        let roster = RosterModel.make(machines: [machine([api, web])], requests: [], missions: missions)
        #expect(roster.lines.map(\.mission) == [MissionSummary(needsYou: 1, agents: 2), MissionSummary(background: 1)])
        #expect(roster.lines.map(\.needsYou) == [true, false])
        #expect(roster.waiting == 1)
        #expect(roster.headline == "1 needs you")
        #expect(roster.tally == RosterModel.Tally(idle: 1))
    }

    @Test func aMissionNeedAndARequestOnOneLineCountOnce() {
        let api = Samples.row(bib: "01", name: "api", status: .working, cwd: "/a")
        let roster = RosterModel.make(
            machines: [machine([api])], requests: [Samples.request(id: "r", bib: "01", title: "Run npm test?")],
            missions: [TabID(hostID: Samples.studio, sessionID: api.id): MissionSummary(needsYou: 1)]
        )
        #expect(roster.waiting == 1)
    }

    @Test func rosterWithoutMissionsIsUnchanged() {
        let api = Samples.row(bib: "01", name: "api", status: .working, cwd: "/a")
        let roster = RosterModel.make(machines: [machine([api])], requests: [])
        #expect(roster.lines[0].mission == nil)
        #expect(!roster.lines[0].needsYou)
        #expect(roster.waiting == 0)
    }
}
