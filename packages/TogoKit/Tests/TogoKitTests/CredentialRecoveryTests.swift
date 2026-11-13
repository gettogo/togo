import Foundation
@preconcurrency import LocalAuthentication
import Security
import Testing
@testable import TogoKit

struct HostCredentialRecoveryTests {
    @Test(arguments: [errSecNotAvailable, errSecInteractionNotAllowed])
    func temporaryKeychainFailureRecoversWithoutRefresh(_ status: Int32) async {
        await recovers(after: SecretStoreError(status: status))
    }

    @Test(arguments: [LAError.Code.systemCancel, .appCancel, .notInteractive, .invalidContext])
    func interruptedAuthenticationRecoversWithoutRefresh(_ code: LAError.Code) async {
        await recovers(after: LAError(code))
    }

    @Test(arguments: [LAError.Code.authenticationFailed, .userCancel])
    func rejectedOrCancelledAuthenticationStillNeedsSignIn(_ code: LAError.Code) async {
        let factory = ScriptedFactory([.fail(LAError(code))])
        let clock = ManualClock()
        let monitor = MonitorFixtures.monitor(factory, clock: clock)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .authNeeded })
        clock.advance(.seconds(60))
        await monitor.setForeground(true)
        #expect(clock.pending.isEmpty)
        #expect(factory.calls == 1)
        await monitor.stop()
    }

    private func recovers(after error: any Error) async {
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [MonitorFixtures.session("s1")], approvals: [])])
        let factory = ScriptedFactory([.fail(error), .source(source)])
        let clock = ManualClock()
        let monitor = MonitorFixtures.monitor(factory, clock: clock)
        let log = SnapshotLog(monitor)
        await monitor.start()
        let retry = await clock.runSleep(.milliseconds(500))
        #expect(retry, "temporary credential access must not require a manual refresh")
        if retry { #expect(await eventually { log.reach == .online && log.sessionIDs == ["s1"] }) }
        #expect(log.reach != .authNeeded)
        await monitor.stop()
    }
}

struct TerminalCredentialRecoveryTests {
    @Test(arguments: [errSecNotAvailable, errSecInteractionNotAllowed])
    func temporaryKeychainFailureDoesNotEndReconnectingTerminal(_ status: Int32) async {
        await recovers(after: SecretStoreError(status: status))
    }

    @Test(arguments: [LAError.Code.systemCancel, .appCancel, .notInteractive, .invalidContext])
    func interruptedAuthenticationDoesNotEndReconnectingTerminal(_ code: LAError.Code) async {
        await recovers(after: LAError(code))
    }

    @Test(arguments: [LAError.Code.authenticationFailed, .userCancel])
    func rejectedOrCancelledAuthenticationStopsTerminalRetries(_ code: LAError.Code) async {
        let script = ScriptedConnect([.fail(LAError(code)), .attach(FakeTransport(), .ssh)])
        let link = TerminalLinkTests.link(script)
        let log = TerminalLinkTests.StateLog(link)
        await link.start()
        #expect(await eventually { if case .failed = log.all.last { true } else { false } })
        #expect(await stays { script.sizes.count > 1 })
        await link.stop()
    }

    private func recovers(after error: any Error) async {
        let first = FakeTransport()
        let script = ScriptedConnect([.attach(first, .ssh), .fail(error), .attach(FakeTransport(), .ssh)])
        let link = TerminalLinkTests.link(script)
        let log = TerminalLinkTests.StateLog(link)
        await link.start()
        #expect(await eventually { log.liveCount == 1 })
        first.emit(.closed(.networkLost))
        #expect(await eventually { log.liveCount == 2 })
        #expect(script.sizes.count == 3)
        await link.stop()
    }
}

private struct InterruptedCredentialEvaluator: BiometricEvaluator {
    let code: LAError.Code
    func evaluate(_ context: LAContext, reason: String) async throws { throw LAError(code) }
}

struct SSHCredentialInterruptionTests {
    @Test(arguments: [LAError.Code.systemCancel, .appCancel, .notInteractive, .invalidContext])
    func systemInterruptionIsNotReportedAsUserCancellation(_ code: LAError.Code) async {
        let connection = connection(code: code)
        do {
            try await connection.connect()
            Issue.record("an interrupted credential evaluation unexpectedly connected")
        } catch let error as LAError {
            #expect(error.code == code)
        } catch {
            Issue.record("a system interruption was changed into a different failure")
        }
        await connection.close()
    }

    @Test func explicitUserCancellationIsStillReported() async {
        let connection = connection(code: .userCancel)
        await #expect(throws: SSHError.cancelled) { try await connection.connect() }
        await connection.close()
    }

    private func connection(code: LAError.Code) -> SSHConnection {
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession(evaluator: InterruptedCredentialEvaluator(code: code))
        return SSHConnection(
            profile: HostProfile(id: UUID(), name: "Test", endpoint: HostEndpoint(host: "127.0.0.1", port: 1, user: "test"), auth: .key(id: "test-key")),
            keys: KeychainKeyStore(secrets: secrets, biometrics: biometrics), secrets: secrets, biometrics: biometrics,
            hostKeys: FileHostKeyStore(url: FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)),
            prompter: ScriptedPrompter(trust: false)
        )
    }
}
