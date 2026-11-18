import Foundation
import Testing
@testable import TogoKit

struct TerminalRoamingRecoveryTests {
    @Test func lateRoamingAfterForegroundChecksWithoutAnotherMinute() async throws {
        let first = FakeTransport()
        let clock = ManualClock()
        let link = TerminalLink(
            connect: ScriptedConnect([.attach(first, .mosh), .attach(FakeTransport(), .mosh)]).closure,
            path: FakePath(), clock: clock, cols: 80, rows: 24, roamAnswers: { _ in true }
        )
        let log = TerminalLinkTests.StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        await link.didEnterBackground()
        clock.advance(.seconds(3601))
        await link.willEnterForeground()
        // Mosh reports roaming only after its worker resumes, later than foreground entry.
        first.emit(.roaming)
        #expect(await eventually { log.liveCount == 2 })
        await link.stop()
    }

    @Test func repeatedRoamingDoesNotRestartTheRecoveryDeadline() async throws {
        let first = FakeTransport()
        let clock = ManualClock()
        let link = TerminalLink(
            connect: ScriptedConnect([.attach(first, .et), .attach(FakeTransport(), .et)]).closure,
            path: FakePath(), clock: clock, cols: 80, rows: 24, roamAnswers: { _ in true }
        )
        let log = TerminalLinkTests.StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        first.emit(.roaming)
        #expect(await eventually { clock.pending == [TerminalLink.roamCheck] })
        clock.advance(.seconds(30))
        first.emit(.roaming)
        try await Task.sleep(for: .milliseconds(30))
        #expect(clock.pending == [.seconds(30)])
        clock.advance(.seconds(30))
        #expect(await eventually { log.liveCount == 2 })
        await link.stop()
    }
}
