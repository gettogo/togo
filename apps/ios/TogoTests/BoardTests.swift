import CoreGraphics
import Foundation
import Testing
import TogoKit
@testable import Togo

/// The board: every live session as a card, grouped by what it needs from the user.
@MainActor struct BoardTests {
    private let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private let four = TabID(hostID: Samples.studio, sessionID: "s-04")

    private func machine(_ rows: [SessionRow]) -> MachineSection {
        MachineSection(
            id: Samples.studio, name: "Studio", symbol: .desktop, reachability: .online,
            helper: .present(version: "0.3.0", live: true), sessions: rows, lastSeen: Samples.now
        )
    }

    @Test func groupsByStatus() {
        let rows = [
            Samples.row(bib: "01", name: "api", status: .working, cwd: "/a"),
            Samples.row(bib: "02", name: "web", status: .done, cwd: "/b"),
            Samples.row(bib: "03", name: "ops", status: .idle, cwd: "/c"),
            Samples.row(bib: "04", name: "app", status: .needsYou, cwd: "/d"),
            Samples.row(bib: "05", name: "old", status: .exited, cwd: "/e"),
            Samples.row(bib: "06", name: "cli", status: .working, cwd: "/f"),
            Samples.row(bib: "07", name: "odd", status: .unknown("paused"), cwd: "/g"),
        ]
        let sections = Board.sections(machines: [machine(rows)])

        #expect(sections.map(\.kind) == [.needsYou, .working, .done, .idle])
        #expect(sections.map { $0.cards.map(\.row.name) } == [["app"], ["api", "cli"], ["web"], ["ops", "odd"]])
        // Ended sessions stay with the archive.
        #expect(!sections.flatMap(\.cards).contains { $0.row.status == .exited })
        #expect(sections[0].cards[0].machine == "Studio")
        #expect(sections[0].cards[0].id == four)

        // An empty group has no heading.
        let quiet = Board.sections(machines: [machine([rows[0]])])
        #expect(quiet.map(\.kind) == [.working])
    }

    @Test func evenColumnsOnFoldable() {
        let duo = FoldInfo.from(active: nil, inactive: CGRect(x: 463, y: 0, width: 25, height: 669))
        // Three cards fit across an open Duo; the fold makes it two, one each side.
        #expect(BoardLayout.columnCount(width: 951, fold: .flat) == 3)
        #expect(BoardLayout.columnCount(width: 951, fold: duo) == 2)
        // An iPad keeps an odd count.
        #expect(BoardLayout.columnCount(width: 1032, fold: .flat) == 3)
        // One column never rounds down to none.
        #expect(BoardLayout.columnCount(width: 466, fold: duo) == 1)
        #expect(BoardLayout.columnCount(width: 300, fold: .flat) == 1)
        // Even counts stay as they are.
        #expect(BoardLayout.columnCount(width: 1300, fold: duo) == 4)

        // With the fold across the middle, the middle gap covers it and no card sits on it.
        let grid = BoardLayout.grid(width: 951, fold: duo)
        #expect(grid.columns.count == 2)
        let band = CGRect(x: 463, y: 0, width: 25, height: 669)
        for column in grid.columns {
            #expect(column.minX >= 0 && column.maxX <= 951)
            #expect(column.maxX <= band.minX || column.minX >= band.maxX)
            #expect(column.width >= BoardLayout.minimumCardWidth)
        }
        // A fold that is not in the middle of the board, such as beside a sidebar, is not used.
        let offCentre = BoardLayout.grid(width: 631, fold: duo)
        #expect(offCentre.columns.count == 2)
        #expect(abs(offCentre.columns[0].width - offCentre.columns[1].width) < 0.5)
    }

    @Test func cardTapOpensInFocusedPaneAndBesideIsExplicit() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.setWorkspaceInline(true)
        store.boardShown = true
        let rows = Samples.controlCenter.machines.flatMap(\.sessions)
        let api = rows.first { $0.id == one.sessionID }!
        let app = rows.first { $0.id == four.sessionID }!

        store.openFromBoard(api)
        #expect(store.workspace.selected == one)
        #expect(store.workspace.secondary == nil)
        #expect(store.workspaceOpen)
        #expect(!store.boardShown)

        // A second tap never opens a second pane on its own.
        store.boardShown = true
        store.openFromBoard(app)
        #expect(store.workspace.selected == four)
        #expect(store.workspace.secondary == nil)

        store.boardShown = true
        store.setPanesVisible(true)
        store.openBesideFromBoard(api)
        #expect(store.workspace.secondary == one)
        #expect(store.workspace.selected == four)
        #expect(!store.boardShown)
    }

    @Test func compactTapKeepsBoardUnderTheWorkspace() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.boardShown = true
        let api = Samples.controlCenter.machines.flatMap(\.sessions).first { $0.id == one.sessionID }!
        store.openFromBoard(api)
        #expect(store.workspaceOpen)
        #expect(store.boardShown)
    }

    @Test func boardShortcutToggles() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        #expect(store.workspaceOpen)
        // In a compact window the board comes up from under the workspace.
        store.toggleBoard()
        #expect(store.boardShown)
        #expect(!store.workspaceOpen)
        store.toggleBoard()
        #expect(!store.boardShown)

        store.setWorkspaceInline(true)
        store.workspaceOpen = true
        store.toggleBoard()
        #expect(store.boardShown)
        #expect(store.workspaceOpen)
        #expect(ShellLayout.resolve(
            size: CGSize(width: 1032, height: 1376), sizeClass: .regular, workspaceOpen: true, hasTabs: true, boardShown: true
        ) == .split(detail: .board))
    }

    /// In book posture the sidebar ends where the fold starts, so the board's own band is at its
    /// leading edge. No card may reach into it.
    @Test func bookPostureKeepsCardsOffTheFold() {
        let leading = FoldInfo.from(active: CGRect(x: 0, y: 0, width: 25, height: 669), inactive: nil)
        let grid = BoardLayout.grid(width: 463, fold: leading)
        #expect(!grid.columns.isEmpty)
        #expect(grid.columns.allSatisfy { $0.minX >= 25 })
        #expect(grid.columns.allSatisfy { $0.width >= BoardLayout.minimumCardWidth })

        let trailing = FoldInfo.from(active: CGRect(x: 438, y: 0, width: 25, height: 669), inactive: nil)
        let other = BoardLayout.grid(width: 463, fold: trailing)
        #expect(other.columns.allSatisfy { $0.maxX <= 438 })

        // A fold that is not bent takes no room away.
        let flat = FoldInfo.from(active: nil, inactive: CGRect(x: 143, y: 0, width: 25, height: 669))
        #expect(BoardLayout.grid(width: 631, fold: flat).columns.first?.minX == BoardLayout.margin)
    }

    /// Cards that left the board stop counting as on screen.
    @Test func goneCardsLeaveTheVisibleSet() {
        let sections = Board.sections(machines: Samples.controlCenter.machines)
        let gone = TabID(hostID: Samples.studio, sessionID: "s-gone")
        #expect(Board.onBoard([one, gone], sections: sections) == [one])
    }

    /// With the board as the detail the workspace is not on screen, so its shortcuts wait.
    @Test func workspaceShortcutsWaitBehindTheBoard() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.setWorkspaceInline(true)
        store.panesFit = true
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.open(hostID: four.hostID, sessionID: four.sessionID)
        store.boardShown = true
        #expect(!store.canToggleSplit)
        store.toggleZen()
        #expect(!store.zen)
        store.boardShown = false
        #expect(store.canToggleSplit)
        store.toggleZen()
        #expect(store.zen)
    }
}
