import CoreGraphics
import Foundation
import Observation
import UIKit

/// Whether the system may dim and lock the screen when nobody touches it.
@MainActor protocol IdleTimerControl: AnyObject {
    var isIdleTimerDisabled: Bool { get set }
}

/// The battery as the monitor needs it. `level` runs from 0 to 1 and is negative when the
/// system does not know it.
@MainActor protocol BatterySource: AnyObject {
    var level: Float { get }
    var isPluggedIn: Bool { get }
}

/// "Keep screen on" for the board. The screen stays awake only while the board is on screen
/// in an active scene, and lets go on background, on leaving the board, or when the battery is
/// under 20 % and unplugged. After ten minutes without a touch the board dims and its content
/// moves a little each minute, so a status left up for hours does not burn in; a touch or a
/// session that newly needs the user lifts the dim.
@MainActor @Observable final class MonitorMode {
    static let dimAfter: TimeInterval = 10 * 60
    static let dimOpacity = 0.7
    static let maxShift: CGFloat = 4
    static let lowBattery: Float = 0.2

    /// The user's choice, kept in `DeviceSettings.keepScreenOn`.
    var enabled: Bool {
        didSet { update() }
    }

    private(set) var isHolding = false
    private(set) var dimmed = false
    /// How far the board's content is moved while dimmed.
    private(set) var shift: CGSize = .zero
    /// One line on the board when the monitor let go on its own.
    private(set) var note: String?

    @ObservationIgnored private let idleTimer: any IdleTimerControl
    @ObservationIgnored private let battery: any BatterySource
    @ObservationIgnored private var boardVisible = false
    @ObservationIgnored private var sceneActive = false
    @ObservationIgnored private var lastTouch: Date
    @ObservationIgnored private var dimmedAt: Date?
    /// The sessions that needed the user at the last report; nil before the first.
    @ObservationIgnored private var needingYou: Set<TabID>?

    /// Each step moves at most `maxShift`, and no position is further than that from the middle.
    private static let shifts: [CGSize] = [
        .zero, CGSize(width: 2, height: 0), CGSize(width: 2, height: 2), CGSize(width: 0, height: 2),
        CGSize(width: -2, height: 2), CGSize(width: -2, height: 0), CGSize(width: -2, height: -2),
        CGSize(width: 0, height: -2), CGSize(width: 2, height: -2),
    ]

    init(enabled: Bool, idleTimer: any IdleTimerControl, battery: any BatterySource, now: Date = Date()) {
        self.enabled = enabled
        self.idleTimer = idleTimer
        self.battery = battery
        lastTouch = now
        // Nothing holds yet, and the timer may be another board's: only a later change writes it.
        note = enabled && batteryLow ? BoardCopy.lowBattery : nil
    }

    func setBoardVisible(_ visible: Bool, at now: Date = Date()) {
        guard visible != boardVisible else { return }
        boardVisible = visible
        lastTouch = now
        update()
    }

    func setSceneActive(_ active: Bool, at now: Date = Date()) {
        guard active != sceneActive else { return }
        sceneActive = active
        lastTouch = now
        update()
    }

    func batteryChanged() {
        update()
    }

    func touch(at now: Date = Date()) {
        lastTouch = now
        lift()
    }

    /// The sessions that need the user now. One that was not in the last report wakes the board;
    /// the first report only sets where to start from.
    func needsYou(_ ids: Set<TabID>, at now: Date = Date()) {
        defer { needingYou = ids }
        guard let known = needingYou, !ids.subtracting(known).isEmpty else { return }
        touch(at: now)
    }

    /// Dims once ten minutes have passed without a touch, then moves the content each minute.
    func tick(at now: Date = Date()) {
        guard isHolding, !frozen else { return }
        let dimFrom = lastTouch.addingTimeInterval(Self.dimAfter)
        guard now >= dimFrom else { return }
        if !dimmed {
            dimmed = true
            dimmedAt = dimFrom
        }
        let minutes = Int(now.timeIntervalSince(dimmedAt ?? dimFrom) / 60)
        let next = Self.shifts[minutes % Self.shifts.count]
        if next != shift { shift = next }
    }

    #if DEBUG
    /// Holds the dimmed board still for previews and snapshots: later inputs change nothing.
    func freezeDimmed() {
        dimmed = true
        shift = Self.shifts[2]
        frozen = true
    }
    #endif

    // MARK: Private

    @ObservationIgnored private var frozen = false

    private var batteryLow: Bool {
        battery.level >= 0 && battery.level < Self.lowBattery && !battery.isPluggedIn
    }

    private func update() {
        guard !frozen else { return }
        // The battery is only read while the choice is on.
        let low = enabled && batteryLow
        let hold = enabled && boardVisible && sceneActive && !low
        let line = low ? BoardCopy.lowBattery : nil
        if note != line { note = line }
        if isHolding != hold { isHolding = hold }
        if idleTimer.isIdleTimerDisabled != hold { idleTimer.isIdleTimerDisabled = hold }
        if !hold { lift() }
    }

    private func lift() {
        guard !frozen else { return }
        dimmedAt = nil
        if dimmed { dimmed = false }
        if shift != .zero { shift = .zero }
    }
}

/// The app's idle timer, shared by every window: the screen stays on while any window's board
/// holds it, so one window going to the background cannot let go for another.
@MainActor final class WindowIdleTimer: IdleTimerControl {
    private static var holders: Set<ObjectIdentifier> = []

    var isIdleTimerDisabled = false {
        didSet {
            guard isIdleTimerDisabled != oldValue else { return }
            if isIdleTimerDisabled {
                Self.holders.insert(ObjectIdentifier(self))
            } else {
                Self.holders.remove(ObjectIdentifier(self))
            }
            UIApplication.shared.isIdleTimerDisabled = !Self.holders.isEmpty
        }
    }

    isolated deinit {
        guard Self.holders.remove(ObjectIdentifier(self)) != nil else { return }
        UIApplication.shared.isIdleTimerDisabled = !Self.holders.isEmpty
    }
}

/// The device's battery. The system's battery monitoring goes on the first time it is read,
/// which happens only while "Keep screen on" is on.
@MainActor final class DeviceBattery: BatterySource {
    var level: Float {
        monitor()
        return UIDevice.current.batteryLevel
    }

    var isPluggedIn: Bool {
        monitor()
        return switch UIDevice.current.batteryState {
        case .charging, .full: true
        case .unplugged, .unknown: false
        @unknown default: false
        }
    }

    private func monitor() {
        if !UIDevice.current.isBatteryMonitoringEnabled { UIDevice.current.isBatteryMonitoringEnabled = true }
    }
}
