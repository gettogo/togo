import Foundation

/// When a held key repeats, like the system keyboard: half a second before the first repeat,
/// then ten a second, speeding up the longer the key is held.
enum KeyRepeatTiming {
    static let initialDelay: TimeInterval = 0.5

    /// The wait before repeat number `count`, counted from 0.
    static func delay(beforeRepeat count: Int) -> TimeInterval {
        switch count {
        case ..<1: initialDelay
        case 1...10: 0.1
        case 11...30: 0.06
        default: 0.035
        }
    }
}

/// Repeats a held chord that Togo encoded itself, since those presses skip SwiftTerm's own
/// repeat. Hardware chords keep SwiftTerm's timings so held keys feel the same either way; the
/// key bar uses `KeyRepeatTiming`.
@MainActor final class ChordRepeater {
    typealias Cancel = @MainActor () -> Void
    /// Runs `body` once after the interval; the returned closure cancels it.
    typealias Schedule = @MainActor (TimeInterval, @escaping @MainActor () -> Void) -> Cancel

    static let initialDelay: TimeInterval = 0.4
    static let interval: TimeInterval = 0.1

    var send: ([UInt8]) -> Void = { _ in }
    private(set) var heldKey: Int?
    private let schedule: Schedule
    private var cancel: Cancel?
    private var isHeld: @MainActor () -> Bool = { false }
    private let timing: @MainActor (Int) -> TimeInterval
    private var count = 0

    init(
        schedule: @escaping Schedule = ChordRepeater.timer,
        timing: @escaping @MainActor (Int) -> TimeInterval = { $0 == 0 ? ChordRepeater.initialDelay : ChordRepeater.interval }
    ) {
        self.timing = timing
        self.schedule = schedule
    }

    /// `isHeld` is checked before every repeat, so a release this view never hears about
    /// still ends the repeat within one interval.
    func start(key: Int, bytes: [UInt8], isHeld: @escaping @MainActor () -> Bool) {
        stop()
        heldKey = key
        self.isHeld = isHeld
        count = 0
        arm(bytes)
    }

    /// Stops the repeat; with a key, only when that key is the one held.
    func stop(key: Int? = nil) {
        if let key, key != heldKey { return }
        cancel?()
        cancel = nil
        heldKey = nil
    }

    private func arm(_ bytes: [UInt8]) {
        cancel = schedule(timing(count)) { [weak self] in
            guard let self else { return }
            guard isHeld() else { return stop() }
            send(bytes)
            count += 1
            arm(bytes)
        }
    }

    static func timer(_ delay: TimeInterval, _ body: @escaping @MainActor () -> Void) -> Cancel {
        let timer = Timer.scheduledTimer(withTimeInterval: delay, repeats: false) { _ in
            MainActor.assumeIsolated { body() }
        }
        return { timer.invalidate() }
    }
}
