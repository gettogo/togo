import Foundation
import Testing
@testable import Togo

@MainActor struct WatchFeedReadinessTests {
    @Test func absentOrUnloadedWindowDoesNotPublishAnEmptyRoster() {
        #expect(WatchFeed.ready(nil) == nil)
        #expect(WatchFeed.ready(ControlCenterState()) == nil)
    }

    @Test func emptyConnectingOrOfflineMachinesAreNotAnAuthoritativeRoster() {
        var machine = MachineSection(id: UUID(), name: "Studio", symbol: .desktop, reachability: .connecting,
                                     helper: .checking, sessions: [])
        var state = ControlCenterState(machines: [machine], loaded: true)
        #expect(WatchFeed.ready(state) == nil)
        machine.reachability = .offline(since: .now, reason: "test")
        state.machines = [machine]
        #expect(WatchFeed.ready(state) == nil)
        machine.reachability = .online
        state.machines = [machine]
        #expect(WatchFeed.ready(state)?.sessions == [])
        state.machines = []
        #expect(WatchFeed.ready(state)?.sessions == [])
    }
}
