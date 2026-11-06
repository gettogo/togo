import Foundation
import Testing
@testable import TogoTerminal

@MainActor
@Suite struct OutputAnnouncerTests {
    private func make(active: Bool = true) -> (OutputAnnouncer, ManualClock, Box) {
        let clock = ManualClock()
        let box = Box()
        let a = OutputAnnouncer(clock: clock)
        a.isActive = { box.active }
        a.announce = { box.spoken.append($0) }
        box.active = active
        return (a, clock, box)
    }

    final class Box { var spoken: [String] = []; var active = true }

    @Test func announcesAfterQuiet() async {
        let (a, clock, box) = make()
        a.output(newLines: ["Build passed"])
        await clock.waitForSleepers(1)
        await clock.advance(by: .milliseconds(1400))
        #expect(box.spoken.isEmpty)
        await clock.advance(by: .milliseconds(100))
        #expect(await settles { box.spoken.count == 1 })
        #expect(box.spoken == ["Build passed"])
    }

    @Test func continuedOutputDelaysAnnouncement() async {
        let (a, clock, box) = make()
        a.output(newLines: ["one"])
        await clock.waitForSleepers(1)
        await clock.advance(by: .milliseconds(1000))
        a.output(newLines: ["two"])
        await clock.waitForSleepers(1)
        await clock.advance(by: .milliseconds(1000))
        #expect(box.spoken.isEmpty)
        await clock.advance(by: .milliseconds(500))
        #expect(await settles { box.spoken.count == 1 })
        #expect(box.spoken == ["one two"])
    }

    @Test func rateLimited() async {
        let (a, clock, box) = make()
        a.output(newLines: ["first"])
        await clock.waitForSleepers(1)
        await clock.advance(by: .milliseconds(1500))
        #expect(await settles { box.spoken.count == 1 })
        await clock.waitForSleepers(1)
        a.output(newLines: ["second"])
        await clock.waitForSleepers(2)
        await clock.advance(by: .milliseconds(1500))
        #expect(box.spoken == ["first"])
        await clock.advance(by: .milliseconds(1500))
        #expect(await settles { box.spoken.count == 2 })
        #expect(box.spoken == ["first", "second"])
    }

    @Test func truncatesTo200() async {
        let (a, clock, box) = make()
        a.output(newLines: [String(repeating: "a", count: 500)])
        await clock.waitForSleepers(1)
        await clock.advance(by: .milliseconds(1500))
        #expect(await settles { !box.spoken.isEmpty })
        #expect(box.spoken.first?.count == 200)
    }

    @Test func graphemeSafeTruncation() async {
        let (a, clock, box) = make()
        let family = "\u{1F468}\u{200D}\u{1F469}\u{200D}\u{1F467}\u{200D}\u{1F466}"
        a.output(newLines: ["head" + family + String(repeating: "x", count: 199)])
        await clock.waitForSleepers(1)
        await clock.advance(by: .milliseconds(1500))
        #expect(await settles { !box.spoken.isEmpty })
        let text = box.spoken.first ?? ""
        #expect(text.count == 200)
        #expect(text.hasPrefix(family))
    }

    @Test func silentWhenVoiceOverOff() async {
        let (a, clock, box) = make(active: false)
        a.output(newLines: ["hidden"])
        await clock.advance(by: .seconds(10))
        #expect(box.spoken.isEmpty)
    }

    @Test func silentWhenTurnedOffBeforeSpeaking() async {
        let (a, clock, box) = make()
        a.output(newLines: ["late"])
        await clock.waitForSleepers(1)
        box.active = false
        await clock.advance(by: .seconds(2))
        #expect(box.spoken.isEmpty)
    }

    @Test func ignoresSpinnerOnlyUpdates() async {
        let (a, clock, box) = make()
        a.output(newLines: ["\u{280B} Thinking"])
        await clock.waitForSleepers(1)
        await clock.advance(by: .milliseconds(1500))
        #expect(await settles { box.spoken.count == 1 })
        await clock.waitForSleepers(1)
        await clock.advance(by: .seconds(3))
        a.output(newLines: ["\u{2819} Thinking", "\u{2839} Thinking", "\u{2838}"])
        await clock.advance(by: .seconds(5))
        #expect(box.spoken == ["Thinking"])
    }

    @Test func outputWhileInactiveIsNeverAnnounced() async {
        let (a, clock, box) = make(active: false)
        a.output(newLines: ["old news"])
        box.active = true
        await clock.advance(by: .seconds(10))
        #expect(box.spoken.isEmpty)
        #expect(a.pendingCount == 0)
    }

    @Test func minGapBoundary() async {
        let (a, clock, box) = make()
        a.output(newLines: ["first"])
        await clock.waitForSleepers(1)
        await clock.advance(by: .milliseconds(1500))
        #expect(await settles { box.spoken.count == 1 })
        await clock.waitForSleepers(1)
        a.output(newLines: ["second"])
        await clock.waitForSleepers(2)
        await clock.advance(by: .milliseconds(1500))
        await clock.advance(by: .milliseconds(1499))
        #expect(box.spoken == ["first"])
        await clock.advance(by: .milliseconds(1))
        #expect(await settles { box.spoken.count == 2 })
    }

    @Test func floodInOneCallStaysBounded() async {
        let (a, clock, box) = make()
        let lines = (0..<100_000).map { "line number \($0)" }
        let start = ContinuousClock.now
        a.output(newLines: lines)
        #expect(ContinuousClock.now - start < .seconds(5))
        #expect(a.pendingCount <= 40)
        await clock.waitForSleepers(1)
        await clock.advance(by: .milliseconds(1500))
        #expect(await settles { box.spoken.count == 1 })
        #expect((box.spoken.first?.count ?? 0) <= 200)
        // The newest text is kept: an agent's question is usually the last thing it prints.
        #expect(box.spoken.first?.hasSuffix("line number 99999") == true)
    }

    @Test func floodInManyCallsStaysBounded() async {
        let (a, _, _) = make()
        let start = ContinuousClock.now
        for i in 0..<100_000 { a.output(newLines: ["row \(i)"]) }
        #expect(ContinuousClock.now - start < .seconds(10))
        #expect(a.pendingCount <= 60)
    }

    @Test func cutStartsAtAWord() async {
        let (a, clock, box) = make()
        a.waiting(["first words that will be cut away", String(repeating: "word ", count: 39) + "end"])
        await clock.waitForSleepers(1)
        await clock.advance(by: .milliseconds(1500))
        #expect(await settles { box.spoken.count == 1 })
        let text = box.spoken.first ?? ""
        #expect(text.count <= 200)
        #expect(text.hasPrefix("word "))
        #expect(text.hasSuffix("end"))
    }
}
