import SwiftUI
import Testing
@testable import Togo

/// Where Settings puts its panes on each layout, and what survives a fold, an unfold or a resize.
@MainActor struct SettingsPlacementTests {
    /// Side by side the detail is never blank; collapsed, the list comes first.
    @Test func aWideSettingsShowsTheFirstPaneUntilOneIsPicked() {
        #expect(SettingsSplitView.pane(nil, sizeClass: .regular) == .machines)
        #expect(SettingsSplitView.pane(.privacy, sizeClass: .regular) == .privacy)
        #expect(SettingsSplitView.pane(nil, sizeClass: .compact) == nil)
        #expect(SettingsSplitView.pane(.privacy, sizeClass: .compact) == .privacy)
    }

    /// Folding with a pane open keeps the user in it; with none, on the list.
    @Test func collapsingKeepsTheOpenPaneOnTop() {
        #expect(SettingsSplitView.column(for: .notifications) == .detail)
        #expect(SettingsSplitView.column(for: nil) == .sidebar)
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
