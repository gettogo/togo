import Foundation
import Synchronization
import Testing
@testable import TogoKit

struct TerminalLinkTests {
    final class StateLog: Sendable {
        private let states = Mutex<[LinkState]>([])
        init(_ link: TerminalLink) {
            let stream = link.states
            Task { [self] in for await state in stream { states.withLock { $0.append(state) } } }
        }
        var all: [LinkState] { states.withLock { $0 } }
        var liveCount: Int { all.count { if case .live = $0 { true } else { false } } }
        var reconnects: [LinkState] { all.filter { if case .reconnecting = $0 { true } else { false } } }
    }

    static func link(_ script: ScriptedConnect, clock: any LinkClock = TestClock(), path: FakePath = FakePath()) -> TerminalLink {
        TerminalLink(connect: script.closure, path: path, clock: clock, cols: 80, rows: 24)
    }

    @Test func reconnectBackoffSchedule() async throws {
        let transports = (0..<5).map { _ in FakeTransport() }
        let clock = TestClock()
        let link = Self.link(ScriptedConnect(transports.map { .attach($0, .ssh) }), clock: clock)
        let log = StateLog(link)
        await link.start()
        for (index, transport) in transports.dropLast().enumerated() {
            #expect(await eventually { log.liveCount == index + 1 })
            transport.emit(.closed(.networkLost))
        }
        #expect(await eventually { log.liveCount == 5 })
        #expect(clock.sleeps == [.zero, .milliseconds(500), .seconds(1), .seconds(2)])
        #expect(log.reconnects == (1...4).map { LinkState.reconnecting(attempt: $0) })
        #expect(log.all.last == .live(.ssh, notes: []))
        await link.stop()
    }

    @Test func liveThenExitBacksOffUntilStable() async throws {
        let transports = (0..<9).map { _ in FakeTransport() }
        let clock = TestClock()
        let link = Self.link(ScriptedConnect(transports.map { .attach($0, .ssh) }), clock: clock)
        let log = StateLog(link)
        await link.start()
        // An attach that goes live and exits at once must not loop without waiting.
        for (index, transport) in transports.prefix(7).enumerated() {
            #expect(await eventually { log.liveCount == index + 1 })
            transport.emit(.closed(.remoteExited(status: 1)))
        }
        #expect(await eventually { log.liveCount == 8 })
        let capped: [Duration] = [.zero, .milliseconds(500), .seconds(1), .seconds(2), .seconds(4), .seconds(8), .seconds(8)]
        #expect(clock.sleeps == capped)
        // Live for the stable period: the next drop starts again from zero.
        clock.advance(.seconds(10))
        transports[7].emit(.closed(.networkLost))
        #expect(await eventually { log.liveCount == 9 })
        #expect(clock.sleeps == capped + [.zero])
        await link.stop()
    }

    @Test func transientFailuresRetryAndDefinitiveOnesStop() async throws {
        let first = FakeTransport(), second = FakeTransport()
        let script = ScriptedConnect([
            .attach(first, .ssh), .fail(SSHError.channelClosed(exitStatus: nil)), .fail(SSHError.timeout), .attach(second, .ssh),
        ])
        let link = Self.link(script)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        first.emit(.closed(.networkLost))
        #expect(await eventually { log.liveCount == 2 })
        await link.stop()

        let third = FakeTransport()
        let blocked = ScriptedConnect([.attach(third, .ssh), .fail(SSHError.authFailed(tried: ["publickey"])), .attach(FakeTransport(), .ssh)])
        let link2 = Self.link(blocked)
        let log2 = StateLog(link2)
        await link2.start()
        #expect(await eventually { log2.liveCount == 1 })
        third.emit(.closed(.networkLost))
        #expect(await eventually { if case .failed = log2.all.last { true } else { false } })
        try? await Task.sleep(for: .milliseconds(50))
        #expect(blocked.sizes.count == 2)
        await link2.stop()
    }

    @Test func stopDuringAdoptResizeClosesAttachment() async throws {
        let connectGate = Gate(), resizeGate = Gate()
        let transport = FakeTransport(resizeGate: resizeGate)
        let script = ScriptedConnect([.attach(transport, .ssh)], during: { await connectGate.passIfOpen() })
        let link = Self.link(script)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { script.sizes.count == 1 })
        // The size changes while connecting, so adopt awaits a resize before going live.
        await link.resize(cols: 100, rows: 30)
        connectGate.open()
        #expect(await eventually { await transport.resizes.count == 1 })
        await link.stop()
        resizeGate.open()
        #expect(await eventually { await transport.isClosed })
        #expect(log.liveCount == 0)
    }

    @Test func connectCalledFreshEachAttempt() async throws {
        let transports = (0..<4).map { _ in FakeTransport() }
        let script = ScriptedConnect(transports.map { .attach($0, .ssh) })
        let link = Self.link(script)
        let log = StateLog(link)
        await link.start()
        for (index, transport) in transports.dropLast().enumerated() {
            #expect(await eventually { log.liveCount == index + 1 })
            transport.emit(.closed(.networkLost))
        }
        #expect(await eventually { log.liveCount == 4 })
        #expect(script.sizes.count == 4)
        for transport in transports.dropLast() {
            #expect(await eventually { await transport.isClosed })
        }
        #expect(await transports[3].isClosed == false)
        await link.stop()
    }

    @Test func reconnectUsesLatestSize() async throws {
        let first = FakeTransport(), second = FakeTransport()
        let gate = Gate()
        let script = ScriptedConnect([.attach(first, .ssh), .attach(second, .ssh)], during: { await gate.passIfOpen() })
        let link = Self.link(script)
        let log = StateLog(link)
        gate.open()
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        gate.close()
        first.emit(.closed(.networkLost))
        #expect(await eventually { script.sizes.count == 2 })
        // The second connect started at 80x24; the window changes while it runs.
        await link.resize(cols: 100, rows: 30)
        await second.output("new")
        gate.open()
        #expect(await eventually { log.liveCount == 2 })
        #expect(await second.calls.first { $0 != .connect } == .resize(100, 30))
        #expect(await link.read() == Data("new".utf8))

        let third = FakeTransport()
        let next = ScriptedConnect([.attach(FakeTransport(), .ssh), .attach(third, .ssh)])
        let path = FakePath()
        let link2 = Self.link(next, path: path)
        let log2 = StateLog(link2)
        await link2.start()
        #expect(await eventually { log2.liveCount == 1 })
        await link2.resize(cols: 100, rows: 30)
        path.change()
        #expect(await eventually { log2.liveCount == 2 })
        #expect(next.sizes.last! == (100, 30))
        await link.stop()
        await link2.stop()
    }

    @Test func inputDroppedWhileNotLive() async throws {
        let first = FakeTransport(), second = FakeTransport()
        let gate = Gate()
        let script = ScriptedConnect([.attach(first, .ssh), .attach(second, .ssh)], during: { await gate.passIfOpen() })
        let link = Self.link(script)
        let log = StateLog(link)
        gate.open()
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        gate.close()
        first.emit(.closed(.networkLost))
        #expect(await eventually { !log.reconnects.isEmpty })
        #expect(await link.send(Data("x\r".utf8)) == false)
        gate.open()
        #expect(await eventually { log.liveCount == 2 })
        #expect(await link.send(Data("y".utf8)))
        #expect(await first.sent.isEmpty)
        #expect(await second.sent == [Data("y".utf8)])
        await link.stop()
    }

    @Test func staleTransportOutputDropped() async throws {
        let first = FakeTransport(), second = FakeTransport()
        let script = ScriptedConnect([.attach(first, .ssh), .attach(second, .ssh)])
        let link = Self.link(script)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        await first.output("before")
        #expect(await link.read() == Data("before".utf8))
        // Read by the link but not yet by the screen when the drop happens.
        await first.output("queued")
        try await Task.sleep(for: .milliseconds(50))
        first.emit(.closed(.networkLost))
        #expect(await eventually { log.liveCount == 2 })
        await first.output("old")
        await second.output("new1")
        await second.output("new2")
        var received = Data()
        while received.count < 8, let chunk = await link.read() { received += chunk }
        #expect(String(decoding: received, as: UTF8.self) == "new1new2")
        await link.stop()
    }

    @Test func pathChangeReconnectsSshOnly() async throws {
        let path = FakePath()
        let script = ScriptedConnect([.attach(FakeTransport(), .ssh), .attach(FakeTransport(), .ssh)])
        let link = Self.link(script, path: path)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        path.change()
        #expect(await eventually { log.liveCount == 2 })
        #expect(script.sizes.count == 2)
        await link.stop()

        let moshPath = FakePath()
        let moshScript = ScriptedConnect([.attach(FakeTransport(), .mosh), .attach(FakeTransport(), .mosh)])
        let moshLink = Self.link(moshScript, path: moshPath)
        let moshLog = StateLog(moshLink)
        await moshLink.start()
        #expect(await eventually { moshLog.liveCount == 1 })
        moshPath.change()
        try await Task.sleep(for: .milliseconds(100))
        #expect(moshScript.sizes.count == 1)
        await moshLink.stop()
    }

    @Test func resizesCoalesced() async throws {
        let transport = FakeTransport()
        let link = Self.link(ScriptedConnect([.attach(transport, .ssh)]), clock: SystemClock())
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        await link.resize(cols: 80, rows: 24)
        await link.resize(cols: 90, rows: 30)
        await link.resize(cols: 100, rows: 30)
        // Wait for the flush instead of guessing how long a loaded machine needs.
        #expect(await eventually(.seconds(20)) { await !transport.resizes.isEmpty })
        try await Task.sleep(for: .milliseconds(150))
        #expect(await transport.resizes == [.resize(100, 30)])
        #expect(await link.resizesSent == 1)
        await link.stop()
    }

    @Test func foregroundAfterLongBackground() async throws {
        for (kind, away, expectedConnects) in [(TransportKind.ssh, 6, 2), (.ssh, 2, 1), (.mosh, 60, 1)] {
            let clock = TestClock()
            let script = ScriptedConnect([.attach(FakeTransport(), kind), .attach(FakeTransport(), kind)])
            let link = Self.link(script, clock: clock)
            let log = StateLog(link)
            await link.start()
            #expect(await eventually { log.liveCount == 1 })
            await link.didEnterBackground()
            clock.advance(.seconds(away))
            await link.willEnterForeground()
            if expectedConnects == 2 {
                #expect(await eventually { log.liveCount == 2 })
            } else {
                try await Task.sleep(for: .milliseconds(100))
            }
            #expect(script.sizes.count == expectedConnects, "\(kind) after \(away) s")
            await link.stop()
        }
    }

    @Test func roamingMirrorsTransport() async throws {
        let transport = FakeTransport()
        let link = Self.link(ScriptedConnect([.attach(transport, .mosh)]))
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        transport.emit(.roaming)
        #expect(await eventually { log.all.last == .roaming })
        transport.emit(.connected)
        #expect(await eventually { log.liveCount == 2 })
        await link.stop()
    }

    /// The Mac restarted under a Mosh tab: SSH answers while Mosh hears nothing, so after a
    /// while the link attaches again instead of roaming forever.
    @Test func longRoamingWithTheMachineAnsweringAttachesAgain() async throws {
        let first = FakeTransport(), second = FakeTransport()
        let clock = ManualClock()
        let link = TerminalLink(
            connect: ScriptedConnect([.attach(first, .mosh), .attach(second, .mosh)]).closure, path: FakePath(),
            clock: clock, cols: 80, rows: 24, roamAnswers: { _ in true }
        )
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        first.emit(.roaming)
        #expect(await eventually { log.all.last == .roaming })
        #expect(await clock.runSleep(TerminalLink.roamCheck))
        #expect(await eventually { log.liveCount == 2 })
        await link.stop()
    }

    @Test func roamingWithTheMachineSilentKeepsRoaming() async throws {
        let first = FakeTransport()
        let clock = ManualClock()
        let link = TerminalLink(
            connect: ScriptedConnect([.attach(first, .mosh), .attach(FakeTransport(), .mosh)]).closure, path: FakePath(),
            clock: clock, cols: 80, rows: 24, roamAnswers: { _ in false }
        )
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        first.emit(.roaming)
        #expect(await clock.runSleep(TerminalLink.roamCheck))
        #expect(await eventually { clock.pending == [TerminalLink.roamCheck] })
        #expect(log.all.last == .roaming)
        await link.stop()
    }

    @Test func inputAcceptedWhileRoaming() async throws {
        let transport = FakeTransport()
        let link = Self.link(ScriptedConnect([.attach(transport, .mosh)]))
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        transport.emit(.roaming)
        #expect(await eventually { log.all.last == .roaming })
        // mosh queues and predicts keystrokes itself while the network is out.
        await link.send(Data("ls".utf8))
        #expect(await transport.sent == [Data("ls".utf8)])
        await link.stop()
    }

    static let sessionGone = HelperError.rpc(RPCErrorObject(code: RPCErrorCode.notFound, message: "not_found"))

    @Test func goneSessionEndsWithoutRetrying() async throws {
        let first = FakeTransport()
        let script = ScriptedConnect([.attach(first, .ssh), .fail(Self.sessionGone), .attach(FakeTransport(), .ssh)])
        let link = Self.link(script)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        // tmux exits when its session is killed; the helper then no longer knows the id.
        first.emit(.closed(.remoteExited(status: 0)))
        #expect(await eventually { log.all.last == .ended })
        try await Task.sleep(for: .milliseconds(50))
        #expect(script.sizes.count == 2)
        #expect(log.all.last == .ended)
        await link.stop()
    }

    @Test func goneSessionOnFirstAttachEnds() async throws {
        let script = ScriptedConnect([.fail(Self.sessionGone)])
        let link = Self.link(script)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.all.last == .ended })
        await link.stop()
    }

    /// The first attach to a machine that answers, but whose helper is restarting, retries
    /// with backoff rather than giving up.
    @Test func firstAttachRetriesWhileHelperIsBack() async throws {
        let transport = FakeTransport()
        let clock = TestClock()
        let script = ScriptedConnect([
            .fail(HelperError.closed), .fail(SSHError.channelClosed(exitStatus: 1)), .attach(transport, .ssh),
        ])
        let link = Self.link(script, clock: clock)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        #expect(log.reconnects == [.reconnecting(attempt: 1), .reconnecting(attempt: 2)])
        #expect(!log.all.contains { if case .failed = $0 { true } else { false } })
        await link.stop()
    }

    @Test func firstAttachToSilentMachineFails() async throws {
        let script = ScriptedConnect([.fail(SSHError.unreachable("h did not answer")), .attach(FakeTransport(), .ssh)])
        let link = Self.link(script)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { if case .failed = log.all.last { true } else { false } })
        try await Task.sleep(for: .milliseconds(50))
        #expect(script.sizes.count == 1)
        await link.stop()
    }

    @Test func keychainRefusalStopsRetrying() async throws {
        let first = FakeTransport()
        let script = ScriptedConnect([.attach(first, .ssh), .fail(SecretStoreError(status: -25308)), .attach(FakeTransport(), .ssh)])
        let link = Self.link(script)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        first.emit(.closed(.networkLost))
        #expect(await eventually { if case .failed = log.all.last { true } else { false } })
        try await Task.sleep(for: .milliseconds(50))
        #expect(script.sizes.count == 2)
        await link.stop()
    }

    @Test func explicitMoshFailureSaysWhy() async throws {
        let script = ScriptedConnect([.fail(MoshBootstrapError.serverMissing("sh: mosh-server: not found"))])
        let link = Self.link(script)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { if case .failed = log.all.last { true } else { false } })
        guard case .failed(let reason) = log.all.last else { return }
        #expect(reason.contains("mosh-server was not found"))
        await link.stop()
    }

    /// "Connecting to X" must not walk forever: a first attach that keeps failing says why.
    @Test func firstAttachGivesUpAfterThreeTries() async throws {
        let script = ScriptedConnect([
            .fail(HelperError.closed), .fail(HelperError.closed), .fail(HelperError.timeout(method: "hello")),
            .attach(FakeTransport(), .ssh),
        ])
        let link = Self.link(script)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { if case .failed = log.all.last { true } else { false } })
        try await Task.sleep(for: .milliseconds(50))
        #expect(script.sizes.count == TerminalLink.firstAttachLimit)
        await link.stop()
    }

    @Test func explicitMoshWithoutUDPStopsAtOnce() async throws {
        let script = ScriptedConnect([.fail(MoshConnectError.noUDPReply(address: "10.0.0.2")), .attach(FakeTransport(), .ssh)])
        let link = Self.link(script)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { if case .failed = log.all.last { true } else { false } })
        try await Task.sleep(for: .milliseconds(50))
        #expect(script.sizes.count == 1)
        await link.stop()
    }

    /// The Mac slept mid-session: after a few unanswered attempts the link reports it, and a
    /// network change starts it again on its own.
    @Test func reconnectGivesUpOnASilentMachineAndRestartsOnAPathChange() async throws {
        let first = FakeTransport(), second = FakeTransport()
        let silent = SSHError.unreachable("work-mac did not answer for 30 s; it may be asleep")
        let script = ScriptedConnect([.attach(first, .ssh), .fail(silent), .fail(silent), .fail(silent), .attach(second, .ssh)])
        let path = FakePath()
        let link = Self.link(script, path: path)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        first.emit(.closed(.networkLost))
        #expect(await eventually { if case .failed = log.all.last { true } else { false } })
        #expect(script.sizes.count == 4)
        path.change()
        #expect(await eventually { log.liveCount == 2 })
        await link.stop()
    }

    @Test func linkThatGaveUpOnASilentMachineRetriesInTheForeground() async throws {
        let script = ScriptedConnect([.fail(SSHError.unreachable("work-mac did not answer")), .attach(FakeTransport(), .ssh)])
        let link = Self.link(script)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { if case .failed = log.all.last { true } else { false } })
        await link.willEnterForeground()
        #expect(await eventually { log.liveCount == 1 })
        await link.stop()
    }

    /// Needing the user is not retried on a path change.
    @Test func permanentFailureIsNotRestartedByAPathChange() async throws {
        let script = ScriptedConnect([.fail(SSHError.authFailed(tried: ["publickey"])), .attach(FakeTransport(), .ssh)])
        let path = FakePath()
        let link = Self.link(script, path: path)
        let log = StateLog(link)
        await link.start()
        #expect(await eventually { if case .failed = log.all.last { true } else { false } })
        path.change()
        try await Task.sleep(for: .milliseconds(50))
        #expect(script.sizes.count == 1)
        await link.stop()
    }

    @Test func readReturnsNilAfterStop() async {
        let link = Self.link(ScriptedConnect([.attach(FakeTransport(), .ssh)]))
        await link.start()
        let reader = Task { await link.read() }
        try? await Task.sleep(for: .milliseconds(50))
        await link.stop()
        #expect(await reader.value == nil)
    }
}

/// Holds connect calls until the test opens it.
final class Gate: Sendable {
    private let isOpen = Mutex(false)
    func open() { isOpen.withLock { $0 = true } }
    func close() { isOpen.withLock { $0 = false } }
    func passIfOpen() async {
        while !isOpen.withLock({ $0 }) { try? await Task.sleep(for: .milliseconds(2)) }
    }
}
