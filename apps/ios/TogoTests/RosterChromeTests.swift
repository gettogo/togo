import Foundation
import Testing
import TogoKit
@testable import Togo

/// Where the roster's controls sit on each layout (roster.md): each once, the filter in the
/// list rather than a bar, no "more" menu, the archive only when it holds something.
@MainActor struct RosterChromeTests {
    /// The sidebar's bar holds only its search field: New session is each machine's "+",
    /// Settings and the sidebar toggle are the detail column's (root.md).
    @Test func theSidebarBarHoldsNothingButSearch() {
        let bar = RosterBar.layout(wide: true, hasMachines: true)
        #expect(bar.all.isEmpty)
    }

    @Test func aMachineHeadingPlusNamesItsMachineAndWhyItIsOff() {
        #expect(SidebarCopy.newSessionOn("Studio") == "New Session on Studio")
        #expect(SidebarCopy.newSessionUnavailable("Studio", reason: "Offline").contains("Offline"))
        #expect(SidebarCopy.newSessionUnavailable("Studio", reason: nil).contains("Studio"))
    }

    /// The folded Duo's vertical bar reads New session, Settings, then search at the foot (D13).
    @Test func aPhoneLeadsWithNewSessionThenSettingsAndSearchesAtTheFoot() {
        let bar = RosterBar.layout(wide: false, hasMachines: true)
        #expect(bar.topTrailing == [.newSession, .settings])
        #expect(bar.bottom == [.search])
        #expect(bar.all == [.newSession, .settings, .search])
    }

    @Test func eachControlAppearsOncePerLayout() {
        for wide in [false, true] {
            for machines in [false, true] {
                let all = RosterBar.layout(wide: wide, hasMachines: machines).all
                #expect(Set(all).count == all.count)
                // Settings is the roster's in a compact window and never the sidebar's.
                #expect(all.contains(.settings) == !wide)
            }
        }
    }

    @Test func beforeAMachineOnlySettingsIsOffered() {
        #expect(RosterBar.layout(wide: false, hasMachines: false).all == [.settings])
        #expect(RosterBar.layout(wide: true, hasMachines: false).all.isEmpty)
    }

    @Test func theListEndsWithAddMachineAndTheArchiveOnlyWhenItHoldsSessions() {
        #expect(RosterListEnd.rows(archived: 0) == [.addMachine])
        #expect(RosterListEnd.rows(archived: 3) == [.addMachine, .archive(3)])
    }

    @Test func theFilterRowSaysTheFilterThenWhatNeedsYouThenTheTally() {
        let store = AppStore(services: FakeAppServices(hosts: Samples.hosts), controlCenter: Samples.controlCenter)
        let roster = store.roster
        let spoken = RosterHeadline.spoken(roster: roster, filter: .all)
        #expect(spoken.hasPrefix(ControlCenterCopy.filterName(.all)))
        if roster.waiting > 0 { #expect(spoken.contains(roster.headline)) }
        if !roster.tallyText.isEmpty { #expect(spoken.hasSuffix(roster.tallyText)) }
        let narrowed = RosterHeadline.spoken(roster: roster, filter: .needsYou)
        #expect(narrowed.hasPrefix(ControlCenterCopy.filterName(.needsYou)))
    }
}
