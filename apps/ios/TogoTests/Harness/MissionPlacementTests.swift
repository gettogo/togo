import Foundation
import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// Mission control's place on every size class: sheet or inspector by the window's room, closed
/// until the user opens it, the bar item's visibility, badge, bounce, subtitle and long-press
/// menu.
@MainActor
struct MissionPlacementTests {
    // MARK: Presentation

    @Test func compactWidthsGetASheetRegularOnesTheInspector() {
        #expect(MissionPresentation.choose(.compact) == .sheet)
        #expect(MissionPresentation.choose(.regular) == .inspector)
        #expect(MissionPresentation.choose(nil) == .sheet)
    }

    @Test func theWindowNotTheSidebarDecidesInspectorOrSheet() {
        // The choice reads the window's width, which a sidebar shown or hidden does not change,
        // so collapsing the sidebar can never switch it, let alone open it.
        #expect(MissionPresentation.choose(.regular, windowWidth: 1194) == .inspector)
        #expect(MissionPresentation.choose(.regular, windowWidth: 1366) == .inspector)
        // An unfolded Duo held wide or tall, and an iPad in portrait: a sheet over the chat.
        #expect(MissionPresentation.choose(.regular, windowWidth: 951) == .sheet)
        #expect(MissionPresentation.choose(.regular, windowWidth: 669) == .sheet)
        #expect(MissionPresentation.choose(.regular, windowWidth: 1032) == .sheet)
    }

    @Test func missionControlStartsClosedAndOnlyTheUserOpensIt() {
        let store = HarnessSessionStore(subject: ChatTests.subject, services: FakeHarnessServices(descriptor: ChatTests.claude))
        #expect(!store.showsMission)
        store.showsMission.toggle()
        #expect(store.showsMission)
        store.showsMission.toggle()
        #expect(!store.showsMission)
    }

    @Test func missionControlNeverWearsAGauge() {
        #expect(MissionPlacement.symbol(.inspector) == "sidebar.trailing")
        #expect(MissionPlacement.symbol(.sheet) == "list.bullet.rectangle")
        for presentation in [MissionPresentation.inspector, .sheet] {
            #expect(!MissionPlacement.symbol(presentation).hasPrefix("gauge"))
        }
    }

    @Test func aSessionKeyNamesTheMachineAndTheSession() {
        let host = UUID()
        let subject = HarnessSubject(hostID: host, sessionID: "s_one", capabilities: [])
        #expect(MissionKey.of(subject) == "\(host.uuidString)/s_one")
    }

    // MARK: The bar item

    @Test func theItemIsHiddenWithoutAMission() {
        // No session in front, and a session whose harness has reported no mission yet.
        let app = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        #expect(app.missionStore == nil)
        app.open(hostID: Samples.studio, sessionID: "s-01")
        #expect(app.missionStore == nil)
    }

    @Test func theBadgeIsTheNeedsCountAlone() {
        #expect(MissionPlacement.badge(MissionSummary(needsYou: 2, agents: 3, working: true)) == 2)
        #expect(MissionPlacement.badge(MissionSummary(agents: 3)) == 0)
        #expect(MissionPlacement.badge(MissionSummary(needsYou: -1)) == 0)
    }

    @Test func theItemBouncesOnceOnARiseOnTheSameSession() {
        let before = MissionNudge.Snapshot(session: "a", needsYou: 0)
        let after = MissionNudge.Snapshot(session: "a", needsYou: 1)
        #expect(MissionNudge.bounces(before, after, reduceMotion: false))
        // An answer, or nothing new, does not bounce.
        #expect(!MissionNudge.bounces(after, before, reduceMotion: false))
        #expect(!MissionNudge.bounces(after, after, reduceMotion: false))
    }

    @Test func theItemNeverBouncesUnderReduceMotionOrOnASessionChange() {
        let before = MissionNudge.Snapshot(session: "a", needsYou: 0)
        #expect(!MissionNudge.bounces(before, .init(session: "a", needsYou: 1), reduceMotion: true))
        #expect(!MissionNudge.bounces(before, .init(session: "b", needsYou: 2), reduceMotion: false))
        // The haptic and the spoken line follow the same arrival, whatever Reduce Motion says.
        #expect(MissionNudge.arrived(before, .init(session: "a", needsYou: 1)))
    }

    @Test func theSubtitleCountsWhatTheBadgeDoesNot() {
        let summary = MissionSummary(needsYou: 1, agents: 2, plan: .init(done: 3, total: 7), working: true)
        #expect(MissionPlacement.glance(summary) == "2 agents  plan 3/7")
    }

    @Test func theSubtitleIsEmptyWhenNothingIsCounted() {
        #expect(MissionPlacement.glance(MissionSummary()) == "")
        #expect(MissionPlacement.glance(MissionSummary(working: true)) == "")
        #expect(MissionPlacement.glance(MissionSummary(needsYou: 1)) == "")
    }

    @Test func theTitleShowsTheGlanceAndKeepsTheStatusForTheMenu() {
        let tab = WorkspaceTab(hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", link: .connecting)
        let plain = WorkspaceTitle(tab: tab, row: nil, machine: "Studio", message: nil)
        #expect(plain.subtitle == plain.detail)
        let glance = WorkspaceTitle(tab: tab, row: nil, machine: "Studio", message: nil, glance: "2 agents")
        #expect(glance.subtitle == "2 agents")
        #expect(glance.detail == plain.detail)
        let idle = WorkspaceTitle(tab: tab, row: nil, machine: "Studio", message: nil, glance: "")
        #expect(idle.subtitle.isEmpty)
        let message = WorkspaceTitle(tab: tab, row: nil, machine: "Studio", message: "Copied", glance: "2 agents")
        #expect(message.subtitle == "Copied")
    }

    // MARK: Long press

    @Test func theLongPressOffersOnlyWhatTheSessionCanDoNow() {
        #expect(MissionMenu.items(needsYou: 0, working: false, canCompact: false) == [.terminal])
        #expect(MissionMenu.items(needsYou: 1, working: true, canCompact: true) == [.goToRequest, .stop, .compact, .terminal])
        #expect(MissionMenu.items(needsYou: 0, working: true, canCompact: false) == [.stop, .terminal])
        #expect(MissionMenu.items(needsYou: 2, working: false, canCompact: true) == [.goToRequest, .compact, .terminal])
    }

    @Test func overTheTerminalTheLongPressDropsTerminal() {
        #expect(MissionMenu.items(needsYou: 0, working: false, canCompact: false, chatShows: false).isEmpty)
        #expect(MissionMenu.items(needsYou: 1, working: true, canCompact: true, chatShows: false) == [.goToRequest, .stop, .compact])
    }

    @Test func theLongPressSaysWhatEachItemDoes() {
        #expect(MissionMenu.title(.goToRequest) == "Go to request")
        #expect(MissionMenu.title(.stop) == "Stop")
        #expect(MissionMenu.title(.compact) == "Compact…")
        #expect(MissionMenu.title(.terminal) == ChatCopy.terminal)
    }

    // MARK: Review items on the chat layer

    /// Preview folds into mission control where it has Ports; Changes never folds, since the
    /// bar's Changes item is the way into the review.
    @Test func onlyPreviewFoldsIntoMissionControlWhereItHasPorts() {
        let both = HarnessDescriptor(
            adapter: .init(id: "claude", name: "Claude Code", harnessVersion: "2.1.319", revision: 1),
            capabilities: [HarnessCapability.ports, HarnessCapability.changes], screens: []
        )
        #expect(ReviewFold(both) == ReviewFold(preview: true))
        let none = HarnessDescriptor(
            adapter: .init(id: "aider", name: "Aider", harnessVersion: "1", revision: 1),
            capabilities: [HarnessCapability.chat], screens: []
        )
        #expect(ReviewFold(none) == ReviewFold(preview: false))
        // The terminal layer, or no mission: nothing folds.
        #expect(ReviewFold(nil) == ReviewFold(preview: false))
    }
}

extension ReviewFold {
    init(preview: Bool) {
        self.init(nil)
        self.preview = preview
    }
}
