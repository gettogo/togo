import Testing
@testable import Togo

/// The vertical bar folds its low-priority items into the overflow menu so the terminal's
/// controller, Back and Changes always have a place (owner's live test, 2026-11-10).
@MainActor struct DockPriorityTests {
    private let bar: [DockPriority.Item] = [.runAgain, .changes, .layer, .mission, .preview, .actions, .settings, .showKeyboard, .keys]

    @Test func theControllerBackAndChangesOutrankTheRest() {
        for kept in [DockPriority.Item.keys, .changes, .actions, .showKeyboard] {
            for folded in [DockPriority.Item.layer, .mission, .preview, .settings] {
                #expect(DockPriority.tier(kept) > DockPriority.tier(folded))
            }
        }
    }

    @Test func aConstrainedBarFoldsSettingsThenPreviewThenMissionThenTheLayerSwitch() {
        #expect(DockPriority.overflowing(bar, count: 1) == [.settings])
        #expect(DockPriority.overflowing(bar, count: 4) == [.settings, .preview, .mission, .layer])
    }

    @Test func theControllerAndChangesAreNeverTheFirstToGo() {
        for count in 0...4 {
            let folded = DockPriority.overflowing(bar, count: count)
            #expect(!folded.contains(.keys) && !folded.contains(.changes))
        }
    }

    @Test func missionControlWithABadgeStays() {
        let folded = DockPriority.overflowing(bar, count: 3, missionNeedsYou: true)
        #expect(!folded.contains(.mission))
        #expect(folded == [.settings, .preview, .layer])
    }

    @Test func theControllerStaysWhenTheKeyboardLeavesOnlyTwoSlots() {
        let terminal: [DockPriority.Item] = [.changes, .actions, .keys]
        #expect(DockPriority.overflowing(terminal, count: 1) == [.actions])
        #expect(DockPriority.overflowing(terminal, count: 2) == [.actions, .changes])
    }

    @Test func evenABadgedMissionYieldsBeforeTheGestureController() {
        #expect(DockPriority.tier(.keys) > DockPriority.tier(.mission, missionNeedsYou: true))
        #expect(!DockPriority.overflowing(bar, count: bar.count - 1, missionNeedsYou: true).contains(.keys))
    }

    @Test func nothingFoldsWhenThereIsRoom() {
        #expect(DockPriority.overflowing(bar, count: 0).isEmpty)
    }
}
