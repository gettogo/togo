import Foundation

/// Repeats a held chord that Togo encoded itself, since those presses skip SwiftTerm's own
/// repeat. Timings match SwiftTerm's so held keys feel the same either way.
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

    init(schedule: @escaping Schedule = ChordRepeater.timer) {
        self.schedule = schedule
    }

    /// `isHeld` is checked before every repeat, so a release this view never hears about
    /// still ends the repeat within one interval.
    func start(key: Int, bytes: [UInt8], isHeld: @escaping @MainActor () -> Bool) {
        stop()
        heldKey = key
        self.isHeld = isHeld
        arm(after: Self.initialDelay, bytes)
    }

    /// Stops the repeat; with a key, only when that key is the one held.
    func stop(key: Int? = nil) {
        if let key, key != heldKey { return }
        cancel?()
        cancel = nil
        heldKey = nil
    }

    private func arm(after delay: TimeInterval, _ bytes: [UInt8]) {
        cancel = schedule(delay) { [weak self] in
            guard let self else { return }
            guard isHeld() else { return stop() }
            send(bytes)
            arm(after: Self.interval, bytes)
        }
    }

    static func timer(_ delay: TimeInterval, _ body: @escaping @MainActor () -> Void) -> Cancel {
        let timer = Timer.scheduledTimer(withTimeInterval: delay, repeats: false) { _ in
            MainActor.assumeIsolated { body() }
        }
        return { timer.invalidate() }
    }
}
