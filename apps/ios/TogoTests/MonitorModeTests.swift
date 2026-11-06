import CoreGraphics
import Foundation
import Testing
@testable import Togo

/// "Keep screen on": the screen stays awake only while the board is on screen in an active
/// scene, dims after ten minutes alone and lets go on its own when the battery runs low.
@MainActor struct MonitorModeTests {
    final class FakeIdleTimer: IdleTimerControl {
        var isIdleTimerDisabled = false
    }

    final class FakeBattery: BatterySource {
        var level: Float = 0.8
        var isPluggedIn = false
    }

    private let start = Date(timeIntervalSinceReferenceDate: 0)
    private let needsYou = TabID(hostID: Samples.studio, sessionID: "s-04")
    private let other = TabID(hostID: Samples.studio, sessionID: "s-07")

    private func monitor(
        _ timer: FakeIdleTimer = FakeIdleTimer(), battery: FakeBattery = FakeBattery(), on: Bool = true
    ) -> MonitorMode {
        let monitor = MonitorMode(enabled: on, idleTimer: timer, battery: battery, now: start)
        monitor.setBoardVisible(true, at: start)
        monitor.setSceneActive(true, at: start)
        return monitor
    }

    @Test func idleTimerOnlyWhileBoardVisibleAndActive() {
        let timer = FakeIdleTimer()
        let monitor = MonitorMode(enabled: true, idleTimer: timer, battery: FakeBattery(), now: start)
        #expect(!timer.isIdleTimerDisabled)
        monitor.setSceneActive(true, at: start)
        #expect(!timer.isIdleTimerDisabled)
        monitor.setBoardVisible(true, at: start)
        #expect(timer.isIdleTimerDisabled)
        #expect(monitor.isHolding)

        // Leaving the board lets go.
        monitor.setBoardVisible(false, at: start)
        #expect(!timer.isIdleTimerDisabled)
        monitor.setBoardVisible(true, at: start)
        #expect(timer.isIdleTimerDisabled)

        // Turning the choice off lets go too.
        monitor.enabled = false
        #expect(!timer.isIdleTimerDisabled)
    }

    @Test func idleTimerRestoredOnBackground() {
        let timer = FakeIdleTimer()
        let monitor = monitor(timer)
        monitor.tick(at: start.addingTimeInterval(MonitorMode.dimAfter))
        #expect(monitor.dimmed)
        monitor.setSceneActive(false, at: start.addingTimeInterval(MonitorMode.dimAfter))
        #expect(!timer.isIdleTimerDisabled)
        #expect(!monitor.dimmed)
        // The choice is kept for the next time the board is on screen.
        #expect(monitor.enabled)
        monitor.setSceneActive(true, at: start.addingTimeInterval(700))
        #expect(timer.isIdleTimerDisabled)
    }

    @Test func lowBatteryEndsMonitor() {
        let timer = FakeIdleTimer()
        let battery = FakeBattery()
        let monitor = monitor(timer, battery: battery)
        #expect(timer.isIdleTimerDisabled)
        battery.level = 0.19
        monitor.batteryChanged()
        #expect(!timer.isIdleTimerDisabled)
        #expect(monitor.note == BoardCopy.lowBattery)

        // Plugged in, it comes back on its own.
        battery.isPluggedIn = true
        monitor.batteryChanged()
        #expect(timer.isIdleTimerDisabled)
        #expect(monitor.note == nil)
    }

    @Test func chargingKeepsMonitor() {
        let timer = FakeIdleTimer()
        let battery = FakeBattery()
        battery.level = 0.05
        battery.isPluggedIn = true
        let monitor = monitor(timer, battery: battery)
        #expect(timer.isIdleTimerDisabled)
        #expect(monitor.note == nil)
        // A level the system does not know is not low.
        battery.level = -1
        battery.isPluggedIn = false
        monitor.batteryChanged()
        #expect(timer.isIdleTimerDisabled)
    }

    @Test func dimsAfterTenMinutes() {
        let monitor = monitor()
        monitor.tick(at: start.addingTimeInterval(599))
        #expect(!monitor.dimmed)
        monitor.tick(at: start.addingTimeInterval(600))
        #expect(monitor.dimmed)
        #expect(MonitorMode.dimOpacity == 0.7)

        // A touch lifts it and starts the ten minutes again.
        monitor.touch(at: start.addingTimeInterval(700))
        #expect(!monitor.dimmed)
        #expect(monitor.shift == .zero)
        monitor.tick(at: start.addingTimeInterval(1_299))
        #expect(!monitor.dimmed)
        monitor.tick(at: start.addingTimeInterval(1_300))
        #expect(monitor.dimmed)

        // Nothing dims while the monitor is off.
        let off = self.monitor(on: false)
        off.tick(at: start.addingTimeInterval(3_600))
        #expect(!off.dimmed)
    }

    @Test func needsYouWakesDim() {
        let monitor = monitor()
        monitor.needsYou([other], at: start)
        monitor.tick(at: start.addingTimeInterval(600))
        #expect(monitor.dimmed)
        // The same sessions again do not wake it.
        monitor.needsYou([other], at: start.addingTimeInterval(610))
        #expect(monitor.dimmed)
        monitor.needsYou([], at: start.addingTimeInterval(615))
        #expect(monitor.dimmed)
        // A session that newly needs the user does.
        monitor.needsYou([needsYou], at: start.addingTimeInterval(620))
        #expect(!monitor.dimmed)
        monitor.tick(at: start.addingTimeInterval(1_219))
        #expect(!monitor.dimmed)
    }

    @Test func burnInShiftStaysWithin4pt() {
        let monitor = monitor()
        var previous = CGSize.zero
        var positions: Set<String> = []
        for minute in 10...130 {
            monitor.tick(at: start.addingTimeInterval(Double(minute) * 60))
            let shift = monitor.shift
            #expect(abs(shift.width) <= MonitorMode.maxShift && abs(shift.height) <= MonitorMode.maxShift)
            #expect((shift.width * shift.width + shift.height * shift.height).squareRoot() <= MonitorMode.maxShift)
            let step = CGSize(width: shift.width - previous.width, height: shift.height - previous.height)
            #expect((step.width * step.width + step.height * step.height).squareRoot() <= MonitorMode.maxShift)
            positions.insert("\(shift.width),\(shift.height)")
            previous = shift
        }
        #expect(positions.count > 4)
        // Ticks within the same minute do not move it.
        let before = monitor.shift
        monitor.tick(at: start.addingTimeInterval(130 * 60 + 20))
        #expect(monitor.shift == before)
    }

    /// A monitor made while another holds the window's timer, as a view rebuild makes one, does
    /// not let go of it.
    @Test func newMonitorLeavesTheTimerAlone() {
        let timer = FakeIdleTimer()
        let held = monitor(timer)
        #expect(timer.isIdleTimerDisabled)
        _ = MonitorMode(enabled: true, idleTimer: timer, battery: FakeBattery(), now: start)
        #expect(timer.isIdleTimerDisabled)
        #expect(held.isHolding)
    }
}
