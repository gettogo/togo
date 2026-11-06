import Foundation
import os

/// Marks the moment the app is usable: the first screen with the person's machines is up and
/// answers touches. Work the first frame does not need (tips, notifications, the Watch,
/// connections) waits for it, so the launch only pays for what is drawn.
///
/// The `interactive` event is on the points-of-interest track, so Instruments' App Launch
/// template shows it beside the first frame.
@MainActor final class PerfSignposts {
    static let app = PerfSignposts(
        emit: { signposter.emitEvent("interactive") },
        recordLaunch: { DiagnosticsRecorder.shared.recordLaunch() },
        nextTurn: { work in RunLoop.main.perform(inModes: [.common]) { MainActor.assumeIsolated(work) } }
    )

    static let signposter = OSSignposter(subsystem: "app.togo.Togo", category: .pointsOfInterest)

    private let emit: () -> Void
    private let recordLaunch: () -> Void
    private let nextTurn: (@escaping @MainActor () -> Void) -> Void
    private var waiting: [@MainActor () -> Void] = []
    private var draining = false
    private(set) var isInteractive = false

    /// `nextTurn` runs its work on a later turn of the main run loop, after the screen had a
    /// chance to draw.
    init(
        emit: @escaping () -> Void, recordLaunch: @escaping () -> Void,
        nextTurn: @escaping (@escaping @MainActor () -> Void) -> Void
    ) {
        self.emit = emit
        self.recordLaunch = recordLaunch
        self.nextTurn = nextTurn
    }

    /// The app's first screen with machines loaded is on screen. Only the first call counts.
    static func interactive() {
        app.interactive()
    }

    func interactive() {
        guard !isInteractive else { return }
        isInteractive = true
        emit()
        recordLaunch()
        drainNext()
    }

    /// Runs `work` once the app is interactive, or now if it already is and nothing waits.
    /// Waiting work runs one item per run-loop turn, in the order it was asked for, so the
    /// first frames after the screen comes up are not held up by all of it at once.
    func afterInteractive(_ work: @escaping @MainActor () -> Void) {
        if isInteractive, !draining {
            work()
        } else {
            waiting.append(work)
        }
    }

    private func drainNext() {
        guard !waiting.isEmpty else {
            draining = false
            return
        }
        draining = true
        nextTurn { [self] in
            let item = waiting.removeFirst()
            item()
            drainNext()
        }
    }

    /// An interval for one launch step, shown in Instruments under points of interest.
    static func measure<T>(_ name: StaticString, _ body: () throws -> T) rethrows -> T {
        let state = signposter.beginInterval(name)
        defer { signposter.endInterval(name, state) }
        return try body()
    }
}
