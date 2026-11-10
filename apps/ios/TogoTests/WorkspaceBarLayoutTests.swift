import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// Which top bar the workspace gets: a compact horizontal bar trades the tab rail for the title's
/// menu and moves mission control and Preview into the session menu; a Duo's vertical bar and
/// every regular-width bar keep everything (workspace-chat.md).
@MainActor
struct WorkspaceBarLayoutTests {
    @Test func anIPhoneEitherWayUpGetsTheCompactBar() {
        // Portrait, and landscape on a phone that stays compact wide.
        #expect(WorkspaceBarLayout.choose(horizontal: .compact, vertical: .regular, verticalBar: false) == .compact)
        #expect(WorkspaceBarLayout.choose(horizontal: .compact, vertical: .compact, verticalBar: false) == .compact)
        // A large phone held landscape reports regular width with compact height.
        #expect(WorkspaceBarLayout.choose(horizontal: .regular, vertical: .compact, verticalBar: false) == .compact)
    }

    @Test func aVerticalBarKeepsTheRailAndEveryItemThoughTheWidthIsCompact() {
        // The folded Duo, held either way.
        #expect(WorkspaceBarLayout.choose(horizontal: .compact, vertical: .regular, verticalBar: true) == .full)
        #expect(WorkspaceBarLayout.choose(horizontal: .compact, vertical: .compact, verticalBar: true) == .full)
        // The unfolded Duo in landscape.
        #expect(WorkspaceBarLayout.choose(horizontal: .regular, vertical: .regular, verticalBar: true) == .full)
    }

    @Test func regularWidthKeepsTheRailAndEveryItem() {
        // An iPad, and the unfolded Duo in portrait.
        #expect(WorkspaceBarLayout.choose(horizontal: .regular, vertical: .regular, verticalBar: false) == .full)
    }

    @Test func theLayoutsSayWhereEachThingGoes() {
        #expect(WorkspaceBarLayout.full.showsRail)
        #expect(WorkspaceBarLayout.full.sessionItemsInBar)
        #expect(!WorkspaceBarLayout.full.titleMenu)
        #expect(!WorkspaceBarLayout.compact.showsRail)
        #expect(!WorkspaceBarLayout.compact.sessionItemsInBar)
        #expect(WorkspaceBarLayout.compact.titleMenu)
    }

    @Test func theTitleMenuListsTheTabsInRailOrderWithNumberAndStatus() throws {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.open(hostID: Samples.studio, sessionID: "s-04")
        let entries = store.tabMenuEntries
        #expect(entries.map(\.id) == store.workspace.tabs.map(\.id))
        for (entry, tab) in zip(entries, store.workspace.tabs) {
            let label = store.chipLabel(for: tab)
            #expect(entry.title == [label.bib, label.name].compactMap(\.self).joined(separator: " "))
            let row = try #require(store.session(for: tab))
            #expect(entry.status == LifecycleCopy.status(row.status, exit: row.exit))
        }
    }

    @Test func pickingAnEntrySwitchesTabsAsAChipDoes() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.open(hostID: Samples.studio, sessionID: "s-04")
        let first = TabID(hostID: Samples.studio, sessionID: "s-01")
        // The menu calls the workspace's `show`, which is the chip tap's `choose`.
        store.choose(first)
        #expect(store.focusedTab?.id == first)
    }

    @Test func theSessionMenuRepeatsMissionControlsChoicesButTerminal() {
        #expect(MissionMenu.inSessionMenu([.goToRequest, .stop, .compact, .terminal]) == [.goToRequest, .stop, .compact])
        #expect(MissionMenu.inSessionMenu([.terminal]).isEmpty)
    }
}
