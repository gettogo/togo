import Foundation
import Testing
import TogoKit
@testable import Togo

/// Live card thumbnails: only cards on screen fetch, a few at a time, and nothing runs while
/// the board is away.
@MainActor struct ThumbnailModelTests {
    private let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private let two = TabID(hostID: Samples.studio, sessionID: "s-02")
    private let plain = TabID(hostID: Samples.build, sessionID: "s-09")

    /// A capture that records its calls and answers as the test says.
    @MainActor final class Captures {
        var calls: [TabID] = []
        var hold = false
        var failure: (any Error)?
        var text = "line"
        var now = Date(timeIntervalSinceReferenceDate: 0)

        func model(supported: Set<TabID>? = nil) -> ThumbnailModel {
            ThumbnailModel(
                capture: { [self] id in
                    calls.append(id)
                    if hold { try await Task.sleep(for: .seconds(60)) }
                    if let failure { throw failure }
                    return text
                },
                canCapture: { id in supported?.contains(id) ?? true },
                now: { [self] in now }
            )
        }
    }

    private struct Refused: Error {}

    private func tabs(_ count: Int) -> [TabID] {
        (0..<count).map { TabID(hostID: Samples.studio, sessionID: "s-\($0)") }
    }

    @Test func pollsOnlyVisibleCards() async {
        let captures = Captures()
        let model = captures.model()
        model.setActive(true)
        model.setVisible([one])
        model.pump(at: captures.now)
        await model.settle()
        #expect(captures.calls == [one])
        #expect(model.text(for: one) == "line")
        #expect(model.text(for: two) == nil)

        // Not again before three seconds.
        model.pump(at: captures.now.addingTimeInterval(2.9))
        await model.settle()
        #expect(captures.calls == [one])
        model.pump(at: captures.now.addingTimeInterval(3))
        await model.settle()
        #expect(captures.calls == [one, one])

        // A card scrolled off screen stops, and its capture in flight is cancelled.
        captures.hold = true
        model.pump(at: captures.now.addingTimeInterval(6))
        #expect(model.inFlight == [one])
        model.setVisible([two])
        #expect(model.inFlight.isEmpty)
        model.setActive(false)
    }

    @Test func stopsInBackground() async {
        let captures = Captures()
        let model = captures.model()
        model.setVisible([one, two])
        // Not active yet: the board is not on screen in an active scene.
        model.pump(at: captures.now)
        #expect(captures.calls.isEmpty)

        model.setActive(true)
        captures.hold = true
        model.pump(at: captures.now)
        #expect(model.inFlight == [one, two])
        model.setActive(false)
        #expect(model.inFlight.isEmpty)
        let calls = captures.calls.count
        model.pump(at: captures.now.addingTimeInterval(60))
        await model.settle()
        #expect(captures.calls.count == calls)
    }

    @Test func fallsBackWithoutCapture() async {
        let captures = Captures()
        let model = captures.model(supported: [one])
        model.setActive(true)
        model.setVisible([one, plain])
        model.pump(at: captures.now)
        await model.settle()
        #expect(captures.calls == [one])
        #expect(!model.supportsCapture(plain))
        #expect(model.text(for: plain) == nil)
    }

    @Test func capsConcurrentCaptures() async {
        let captures = Captures()
        captures.hold = true
        let model = captures.model()
        model.setActive(true)
        model.setVisible(Set(tabs(7)))
        model.pump(at: captures.now)
        #expect(model.inFlight.count == 4)
        model.pump(at: captures.now.addingTimeInterval(10))
        #expect(model.inFlight.count == 4)
        model.setActive(false)
    }

    @Test func backsOffOnError() async {
        let captures = Captures()
        captures.failure = Refused()
        let model = captures.model()
        model.setActive(true)
        model.setVisible([one])
        model.pump(at: captures.now)
        await model.settle()
        #expect(captures.calls.count == 1)

        model.pump(at: captures.now.addingTimeInterval(3))
        model.pump(at: captures.now.addingTimeInterval(14.9))
        await model.settle()
        #expect(captures.calls.count == 1)

        captures.failure = nil
        model.pump(at: captures.now.addingTimeInterval(15))
        await model.settle()
        #expect(captures.calls.count == 2)
        #expect(model.text(for: one) == "line")
    }

    @Test func thumbnailIsSanitised() async {
        let captures = Captures()
        let lines = (1...20).map { "line \($0)" }
        captures.text = lines.joined(separator: "\r\n")
            + "\r\n\u{1b}[32mgreen\u{1b}[0m \u{1b}]0;title\u{07}tail \u{1b}[201~end\u{9b}31mc1\u{7}"
            + "\r\n" + String(repeating: "x", count: 120) + "\r\n\r\n"
        let model = captures.model()
        model.setActive(true)
        model.setVisible([one])
        model.pump(at: captures.now)
        await model.settle()

        let shown = model.text(for: one) ?? ""
        #expect(!shown.isEmpty)
        #expect(!shown.unicodeScalars.contains { $0.value < 0x20 && $0 != "\n" })
        #expect(!shown.unicodeScalars.contains { (0x7F...0x9F).contains($0.value) })
        #expect(!shown.contains("[32m") && !shown.contains("title") && !shown.contains("[201~"))
        let shownLines = shown.components(separatedBy: "\n")
        #expect(shownLines.count == ThumbnailModel.lines)
        #expect(shownLines.last == String(repeating: "x", count: ThumbnailText.columns))
        #expect(shownLines[shownLines.count - 2] == "green tail endc1")
        #expect(shownLines.first == "line 11")
    }

    /// A capture that does not stop when cancelled still holds its slot until it returns, so a
    /// fast scroll never has more than four requests out.
    @Test func cancelledCapturesCountUntilTheyReturn() async {
        var waiting: [CheckedContinuation<Void, Never>] = []
        var calls = 0
        let model = ThumbnailModel(
            capture: { _ in
                calls += 1
                await withCheckedContinuation { waiting.append($0) }
                return "line"
            },
            canCapture: { _ in true }, now: { Date(timeIntervalSinceReferenceDate: 0) }
        )
        let start = Date(timeIntervalSinceReferenceDate: 0)
        model.setActive(true)
        model.setVisible(Set(tabs(4)))
        model.pump(at: start)
        for _ in 0..<100 where waiting.count < 4 { await Task.yield() }
        #expect(waiting.count == 4)

        // Scrolled away: the four are cancelled but still running.
        model.setVisible(Set(tabs(8).suffix(4)))
        model.pump(at: start)
        for _ in 0..<50 { await Task.yield() }
        #expect(calls == 4)

        // One returns, one new capture starts.
        waiting.removeFirst().resume()
        for _ in 0..<100 where calls < 5 { await Task.yield() }
        model.pump(at: start)
        for _ in 0..<100 where calls < 5 { await Task.yield() }
        #expect(calls == 5)
        for continuation in waiting { continuation.resume() }
        waiting.removeAll()
        model.setActive(false)
        for _ in 0..<100 { await Task.yield() }
        for continuation in waiting { continuation.resume() }
    }
}
