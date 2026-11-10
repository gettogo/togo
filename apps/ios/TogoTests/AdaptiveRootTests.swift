import CoreGraphics
import Foundation
import SwiftUI
import Testing
@testable import Togo

@MainActor struct AdaptiveRootTests {
    private let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private let four = TabID(hostID: Samples.studio, sessionID: "s-04")
    private let scratch = TabID(hostID: Samples.studio, sessionID: "s-scratch")
    private let release = TabID(hostID: Samples.build, sessionID: "s-02")
    private let iPad = CGSize(width: 1032, height: 1376)

    private func store() -> AppStore {
        AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
    }

    private func row(_ id: TabID) -> SessionRow {
        Samples.controlCenter.machines.flatMap(\.sessions).first { $0.id == id.sessionID }!
    }

    @Test func regularShowsSidebarAndDetail() {
        #expect(ShellLayout.resolve(sizeClass: .regular, workspaceOpen: true, hasTabs: true)
            == .split(detail: .workspace))
        #expect(ShellLayout.resolve(sizeClass: .regular, workspaceOpen: false, hasTabs: true)
            == .split(detail: .empty))
        #expect(ShellLayout.resolve(sizeClass: .regular, workspaceOpen: true, hasTabs: false)
            == .split(detail: .empty))
        // The split view collapses on the size class alone, as the system does.
        #expect(ShellLayout.resolve(sizeClass: nil, workspaceOpen: true, hasTabs: true)
            == .stack(workspaceShown: true))
    }

    /// Compact pushes the workspace column over the roster, and only with a tab to show.
    @Test func compactPushesTheWorkspace() {
        #expect(ShellLayout.resolve(sizeClass: .compact, workspaceOpen: true, hasTabs: true)
            == .stack(workspaceShown: true))
        #expect(ShellLayout.resolve(sizeClass: .compact, workspaceOpen: false, hasTabs: true)
            == .stack(workspaceShown: false))
        #expect(ShellLayout.resolve(sizeClass: .compact, workspaceOpen: true, hasTabs: false)
            == .stack(workspaceShown: false))
    }

    @Test func sizeClassChangeKeepsTabsAndSelection() {
        let store = store()
        store.setWorkspaceInline(true)
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.open(hostID: four.hostID, sessionID: four.sessionID)
        store.setPanesVisible(true)
        store.openBeside(one)
        let workspace = store.workspace
        #expect(workspace.secondary != nil)

        // Split View narrows the window, then gives the width back.
        store.setPanesVisible(false)
        store.setWorkspaceInline(false)
        #expect(store.workspace.tabs == workspace.tabs)
        #expect(store.workspace.selected == workspace.selected)
        #expect(store.workspace.secondary == workspace.secondary)
        #expect(store.workspaceOpen)
        #expect(ShellLayout.resolve(sizeClass: .compact, workspaceOpen: store.workspaceOpen, hasTabs: true)
            == .stack(workspaceShown: true))

        store.setWorkspaceInline(true)
        store.setPanesVisible(true)
        #expect(store.workspace.tabs == workspace.tabs)
        #expect(store.workspace.selected == workspace.selected)
        #expect(store.workspace.secondary == workspace.secondary)
        #expect(ShellLayout.resolve(sizeClass: .regular, workspaceOpen: store.workspaceOpen, hasTabs: true)
            == .split(detail: .workspace))

        // With the cover closed, compact shows the list and regular the empty detail.
        store.workspaceOpen = false
        store.setWorkspaceInline(false)
        #expect(ShellLayout.resolve(sizeClass: .compact, workspaceOpen: store.workspaceOpen, hasTabs: true)
            == .stack(workspaceShown: false))
        #expect(store.workspace.tabs == workspace.tabs)
    }

    @Test func rowSelectionFillsFocusedPane() {
        let store = store()
        store.setWorkspaceInline(true)
        store.showFromSidebar(row(one))
        #expect(store.workspace.selected == one)
        #expect(store.workspaceOpen)

        store.showFromSidebar(row(four))
        #expect(store.workspace.selected == four)
        store.setPanesVisible(true)
        store.openBeside(one)
        #expect(store.workspace.focused == .secondary)

        // The focused pane is the secondary, so a new row fills it and the primary stays.
        store.showFromSidebar(row(scratch))
        #expect(store.workspace.selected == four)
        #expect(store.workspace.secondary == scratch)
        #expect(store.workspace.tabs.contains { $0.id == one })

        store.focus(.primary)
        store.showFromSidebar(row(one))
        #expect(store.workspace.selected == one)
        #expect(store.workspace.secondary == scratch)
        #expect(store.visibleTabs == [one, scratch])
    }

    /// One large terminal is the default: picking rows never opens a second pane.
    @Test func wideWindowWithNoSecondaryNeverSplits() {
        let store = store()
        store.setWorkspaceInline(true)
        for id in [one, four, scratch, release] {
            store.showFromSidebar(row(id))
        }
        store.setPanesVisible(false)
        #expect(store.workspace.secondary == nil)
        #expect(store.visibleTabs == [release])
        #expect(PaneLayout.arrange(
            size: iPad, sizeClass: .regular, fold: .flat, hasSecondary: store.workspace.secondary != nil,
            primaryFraction: store.workspace.primaryFraction
        ) == .single)
    }

    /// Never hide the sidebar to make room: two panes that do not fit beside it wait as one,
    /// and only the user hides it.
    @Test func twoPanesNeverHideTheSidebar() {
        let store = store()
        store.setWorkspaceInline(true)
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.open(hostID: four.hostID, sessionID: four.sessionID)
        store.openBeside(one)
        #expect(!store.sidebar.isHidden)
        store.toggleSidebar()
        #expect(store.sidebar.isHidden)
        store.toggleSidebar()
        #expect(!store.sidebar.isHidden)
    }

    @Test func rotorListsSessionsInOrder() {
        let entries = SessionRotors.sessions(Samples.controlCenter.machines)
        #expect(entries.map(\.label) == [
            "01, Claude Code, Working",
            "04, Claude Code, Needs you",
            "scratch, Shell, Idle",
            "02, Claude Code, Done",
        ])
        #expect(entries.first?.target == .row(one))
        #expect(SessionRotors.machines(Samples.controlCenter.machines).map(\.label) == ["Studio", "Build box"])
    }

    /// Beside a vertical bar the app's item replaces the system toggle, sidebar shown or hidden;
    /// everywhere else the system's toggle is the only one (each control once, P3).
    @Test func sidebarToggleLivesInTheVerticalBar() {
        #expect(SidebarTogglePlace.resolve(inline: true, detailBarVertical: true) == .dock)
        // iPad and the upright unfolded Duo: horizontal bars keep the system toggle.
        #expect(SidebarTogglePlace.resolve(inline: true, detailBarVertical: false) == .system)
        // Compact (folded Duo): no sidebar to show.
        #expect(SidebarTogglePlace.resolve(inline: false, detailBarVertical: true) == .system)
    }

    @Test func needsYouRotorOnlyNeedsYou() {
        let entries = SessionRotors.needsYou(Samples.controlCenter.machines, requests: Samples.controlCenter.requests)
        #expect(entries.map(\.target) == [.row(four)])
        #expect(entries.map(\.label) == ["04, Claude Code, Needs you"])
    }
}
