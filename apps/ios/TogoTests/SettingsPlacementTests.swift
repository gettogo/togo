import SwiftUI
import Testing
@testable import Togo

/// Where Settings puts its panes on each layout, and what survives a fold, an unfold or a resize.
@MainActor struct SettingsPlacementTests {
    /// Side by side the detail is never blank: the first pane shows until one is picked, as a
    /// view, never written into the pick (so a size class change has nothing to write).
    @Test func aWideSettingsShowsTheFirstPaneUntilOneIsPicked() {
        #expect(SettingsSplitView.detail(for: nil) == .machines)
        #expect(SettingsSplitView.detail(for: .privacy) == .privacy)
    }

    /// Folding with a pane open keeps the user in it; with none, on the list.
    @Test func collapsingKeepsTheOpenPaneOnTop() {
        #expect(SettingsSplitView.column(for: .notifications) == .detail)
        #expect(SettingsSplitView.column(for: nil) == .sidebar)
    }

    /// Back on the list clears the pick; the pane on top keeps it. Reading the column again from
    /// the pick gives the same column, so collapse and expand settle in one step, never a loop.
    @Test func theTopColumnAndThePickAgree() {
        #expect(SettingsSplitView.pick(after: .sidebar, pane: .privacy) == nil)
        #expect(SettingsSplitView.pick(after: .detail, pane: .privacy) == .privacy)
        #expect(SettingsSplitView.pick(after: .detail, pane: nil) == nil)
        for pane in SettingsPane.allCases.map(Optional.some) + [nil] {
            let column = SettingsSplitView.column(for: pane)
            #expect(SettingsSplitView.pick(after: column, pane: pane) == pane)
        }
    }

    /// Scene storage keeps the pane by its raw value; every pane round-trips.
    @Test func everyPaneRoundTripsThroughSceneStorage() {
        for page in SettingsPane.allCases {
            #expect(SettingsPane(rawValue: page.rawValue) == page)
        }
    }

    /// A form opened from the Settings sheet takes the one sheet, and Settings comes back after.
    @Test func aFormFromTheSettingsSheetReturnsToSettings() {
        var handover = SettingsHandover()
        #expect(handover.open(.editMachine(Samples.studio), from: .settings) == .editMachine(Samples.studio))
        #expect(handover.closed() == .settings)
        // Settings closing itself does not open again.
        #expect(handover.closed() == nil)
    }

    /// Opened from the roster (Settings pushed in a compact window), the form just closes.
    @Test func aFormFromElsewhereJustCloses() {
        var handover = SettingsHandover()
        _ = handover.open(.addMachine, from: nil)
        #expect(handover.closed() == nil)
    }
}
