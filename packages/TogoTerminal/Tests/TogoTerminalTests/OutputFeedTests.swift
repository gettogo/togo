import Foundation
import Testing
@testable import TogoTerminal

/// The feed reads a fake screen: the rows SwiftTerm would show, and whether the user has
/// scrolled back from the live output.
@MainActor
@Suite struct OutputFeedTests {
    final class FakeScreen {
        var rows: [String]
        var scrolledBack = false
        var alternate = false
        var voiceOver = true
        var focused = true
        var spoken: [String] = []
        var activeChanges: [Bool] = []

        init(_ rows: [String]) { self.rows = rows }

        /// New output scrolls the screen up, as a shell does once it is full.
        func print(_ lines: String...) {
            let height = rows.count
            rows = Array((rows + lines).suffix(height))
        }
    }

    private func make(_ rows: [String], enabled: Bool = true) -> (OutputFeed, ManualClock, FakeScreen) {
        let clock = ManualClock()
        let screen = FakeScreen(rows)
        let feed = OutputFeed(announcer: OutputAnnouncer(clock: clock), clock: clock)
        feed.screen = { screen.scrolledBack ? nil : TerminalScreen(rows: screen.rows, isAlternate: screen.alternate) }
        feed.voiceOverRunning = { screen.voiceOver }
        feed.focused = { screen.focused }
        feed.onActiveChange = { screen.activeChanges.append($0) }
        feed.announcer.announce = { screen.spoken.append($0) }
        feed.enabled = enabled
        feed.refresh()
        return (feed, clock, screen)
    }

    /// Lets the quiet spell and the gap after an announcement both pass.
    private func settle(_ clock: ManualClock) async {
        await clock.waitForSleepers(1)
        await clock.advance(by: .milliseconds(1500))
    }

    @Test func scrollDoesNotRepeatHeardLines() async {
        let (feed, clock, screen) = make(["$ make", "building", "linking"])
        screen.print("done")
        feed.screenChanged()
        await settle(clock)
        #expect(await settles { screen.spoken.count == 1 })
        #expect(screen.spoken == ["done"])

        // The gap after "done" runs out; then one more line scrolls everything up a row.
        await clock.waitForSleepers(1)
        await clock.advance(by: .seconds(3))
        screen.print("$ ")
        feed.screenChanged()
        // A full redraw of the same rows is not new output either.
        feed.screenChanged()
        await settle(clock)
        #expect(await settles { screen.spoken.count == 2 })
        #expect(screen.spoken == ["done", "$"])

        // VoiceOver's three-finger scroll shows older rows; coming back shows the same screen.
        await clock.waitForSleepers(1)
        await clock.advance(by: .seconds(3))
        screen.scrolledBack = true
        feed.screenChanged()
        screen.scrolledBack = false
        feed.screenChanged()
        await clock.advance(by: .seconds(10))
        #expect(screen.spoken == ["done", "$"])
    }

    @Test func floodSpeaksNewest() async {
        let (feed, clock, screen) = make(Array(repeating: "", count: 24))
        for i in 0..<100 {
            screen.print("output line number \(i) with some more text after it")
            feed.screenChanged()
        }
        await settle(clock)
        #expect(await settles { screen.spoken.count == 1 })
        let text = screen.spoken.first ?? ""
        #expect(text.count <= 200)
        #expect(text.hasSuffix("output line number 99 with some more text after it"))
    }

    @Test func silentWithoutFocus() async {
        let (feed, clock, screen) = make(["$ "])
        screen.focused = false
        feed.refresh()
        screen.print("written while away")
        feed.screenChanged()
        await clock.advance(by: .seconds(10))
        #expect(screen.spoken.isEmpty)

        // Coming back does not read what arrived while away, only what follows.
        screen.focused = true
        feed.refresh()
        screen.print("after")
        feed.screenChanged()
        await settle(clock)
        #expect(await settles { screen.spoken.count == 1 })
        #expect(screen.spoken == ["after"])
    }

    @Test func silentWhenSettingOff() async {
        let (feed, clock, screen) = make(["$ "], enabled: false)
        screen.print("hidden")
        feed.screenChanged()
        await clock.advance(by: .seconds(10))
        #expect(screen.spoken.isEmpty)
        #expect(!feed.isActive)

        // Turned off while text waits: it is never spoken.
        feed.enabled = true
        screen.print("waiting")
        feed.screenChanged()
        await clock.waitForSleepers(1)
        feed.enabled = false
        await clock.advance(by: .seconds(10))
        #expect(screen.spoken.isEmpty)
    }

    @Test func silentWithoutVoiceOver() async {
        let (feed, clock, screen) = make(["$ "])
        screen.voiceOver = false
        feed.refresh()
        screen.print("hidden")
        feed.screenChanged()
        await clock.advance(by: .seconds(10))
        #expect(screen.spoken.isEmpty)
        #expect(screen.activeChanges == [true, false])
    }

    @Test func growingLineSpokenOnce() async {
        let (feed, clock, screen) = make(["$ ", ""])
        screen.rows[1] = "Hel"
        feed.screenChanged()
        screen.rows[1] = "Hello, wor"
        feed.screenChanged()
        screen.rows[1] = "Hello, world"
        feed.screenChanged()
        await settle(clock)
        #expect(await settles { screen.spoken.count == 1 })
        #expect(screen.spoken == ["Hello, world"])
    }

    @Test func tickingTimerDoesNotBlockQuiet() async {
        let (feed, clock, screen) = make(["$ claude", "", "", ""])
        screen.rows[1] = "Edited Foo.swift"
        screen.rows[3] = "✻ Working… (0s · esc to interrupt)"
        feed.screenChanged()
        await clock.waitForSleepers(1)
        for tick in 1...20 {
            await clock.advance(by: .milliseconds(500))
            screen.rows[3] = "✻ Working… (\(tick / 2)s · ↑ \(tick * 37) tokens · esc to interrupt)"
            feed.screenChanged()
            // A restarted quiet spell starts its sleep before the clock moves on.
            for _ in 0..<50 { await Task.yield() }
            // The first tick adds token counts, so the row reads as new once more (0.5 s).
            if tick == 4 {
                #expect(await settles { screen.spoken.count == 1 })
            }
        }
        #expect(screen.spoken == ["Edited Foo.swift"])
        #expect(!screen.spoken.contains { $0.contains("esc to interrupt") })
    }

    @Test func churnStillSpeaksAfterMaxWait() async {
        let (feed, clock, screen) = make(["$ claude", ""])
        let verbs = ["Pondering", "Musing", "Brewing", "Spinning"]
        for step in 0..<12 {
            screen.rows[1] = verbs[step % verbs.count]
            feed.screenChanged()
            await clock.waitForSleepers(1)
            await clock.advance(by: .milliseconds(500))
        }
        // Twelve half-second steps: six seconds since the text first waited.
        #expect(await settles { screen.spoken.count == 1 })
    }

    @Test func typedCommandIsNotEchoed() async {
        let (feed, clock, screen) = make(["$ ls", "a.txt", "$ ", "", ""])
        var typed = "$ "
        for character in "git status" {
            feed.noteInput()
            typed.append(character)
            screen.rows[2] = typed
            feed.screenChanged()
            await clock.advance(by: .milliseconds(200))
        }
        await clock.advance(by: .seconds(3))
        #expect(screen.spoken.isEmpty)

        feed.noteInput()
        screen.print("On branch main", "nothing to commit", "$ ")
        feed.screenChanged()
        await settle(clock)
        #expect(await settles { screen.spoken.count == 1 })
        #expect(screen.spoken == ["On branch main nothing to commit"])
    }

    @Test func shortOutputRightAfterReturnIsRead() async {
        let (feed, clock, screen) = make(["$ pwd", "", ""])
        feed.noteInput()
        screen.rows[1] = "/Users/sam"
        screen.rows[2] = "$ "
        feed.screenChanged()
        await settle(clock)
        #expect(await settles { screen.spoken.count == 1 })
        #expect(screen.spoken == ["/Users/sam $"])
    }

    @Test func alternateScreenSwitchIsSilent() async {
        let (feed, clock, screen) = make(["$ vim foo.txt", "", ""])
        screen.alternate = true
        screen.rows = ["~", "~", "\"foo.txt\" 12L, 340B"]
        feed.screenChanged()
        await clock.advance(by: .seconds(10))
        #expect(screen.spoken.isEmpty)

        screen.rows[1] = "a new reply line"
        feed.screenChanged()
        await settle(clock)
        #expect(await settles { screen.spoken.count == 1 })
        #expect(screen.spoken == ["a new reply line"])

        // Back to the shell: its screen is not read again.
        await clock.waitForSleepers(1)
        await clock.advance(by: .seconds(3))
        screen.alternate = false
        screen.rows = ["$ vim foo.txt", "", "$ "]
        feed.screenChanged()
        await clock.advance(by: .seconds(10))
        #expect(screen.spoken == ["a new reply line"])
    }

    @Test func dashboardNumbersAreNotRead() async {
        let (feed, clock, screen) = make(["load 1.02", "cpu 12%", "mem 3.1G"])
        for step in 1...6 {
            screen.rows = ["load \(step).02", "cpu \(10 + step)%", "mem 3.\(step)G"]
            feed.screenChanged()
            await clock.advance(by: .seconds(3))
        }
        #expect(screen.spoken.isEmpty)
    }

    @Test func numberedLinesScrollingPastAreRead() async {
        let (feed, clock, screen) = make(["$ make test", "", ""])
        for i in 1...3 {
            screen.print("test \(i) passed")
            feed.screenChanged()
        }
        await settle(clock)
        #expect(await settles { screen.spoken.count == 1 })
        #expect(screen.spoken == ["test 1 passed test 2 passed test 3 passed"])
    }
}
