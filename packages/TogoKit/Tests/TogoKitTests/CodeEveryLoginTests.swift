@preconcurrency import Citadel
import Foundation
import NIOCore
@preconcurrency import NIOSSH
import Synchronization
import Testing
@testable import TogoKit

/// Asks for a one-time code on every login and accepts only "123456".
private final class CodeEveryLogin: NIOSSHServerKeyboardInteractiveDelegate, Sendable {
    let logins = Mutex(0)

    var supportedAuthenticationMethods: NIOSSHAvailableUserAuthenticationMethods { .keyboardInteractive }

    func requestReceived(
        request: NIOSSHUserAuthenticationRequest, responsePromise: EventLoopPromise<NIOSSHUserAuthenticationOutcome>
    ) {
        responsePromise.succeed(.failure)
    }

    func keyboardInteractiveReceived(
        username: String, responses: [String]?, nextStepPromise: EventLoopPromise<NIOSSHKeyboardInteractiveStep>
    ) {
        switch responses {
        case nil:
            nextStepPromise.succeed(.challenge(.init(
                name: "", instruction: "", prompts: [.init(text: "Verification code:", echo: false)]
            )))
        case ["123456"]?:
            logins.withLock { $0 += 1 }
            nextStepPromise.succeed(.outcome(.success))
        default:
            nextStepPromise.succeed(.outcome(.failure))
        }
    }
}

/// Answers every code prompt with `code`, or declines when it is nil, as a background
/// connection does.
private final class CodePrompter: ConnectionPrompter {
    let code: String?
    let asked = Mutex(0)

    init(code: String?) {
        self.code = code
    }

    /// The profile signs in with a password, which the connection asks for before the server
    /// says what it wants; only the code rounds are counted.
    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? {
        guard case .keyboardInteractive = prompt else { return .secret("unused") }
        asked.withLock { $0 += 1 }
        return code.map { .responses([$0]) }
    }

    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool { true }
}

/// A connection the user signed in on, kept for the monitor's next attempt.
private final class SignedIn: Sendable {
    let connection = Mutex<SSHConnection?>(nil)
}

/// A machine that wants a code at every login: the background monitor never asks and waits for
/// sign-in, then runs on the connection the user signed in on; a terminal the user opens asks
/// and attaches, and one whose question is declined fails without retrying.
struct CodeEveryLoginTests {
    private static func startServer(_ delegate: CodeEveryLogin) async throws -> (SSHServer, Int) {
        for _ in 0..<20 {
            let port = Int.random(in: 20_000...60_000)
            if let server = try? await SSHServer.host(
                host: "127.0.0.1", port: port, hostKeys: [try SSHServerTests.hostKey(9)], authenticationDelegate: delegate
            ) {
                return (server, port)
            }
        }
        throw SSHError.unreachable("no free port")
    }

    private static func connection(_ profile: HostProfile, hostKeys: FileHostKeyStore, prompter: any ConnectionPrompter) -> SSHConnection {
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession(evaluator: CountingEvaluator())
        return SSHConnection(
            profile: profile, keys: KeychainKeyStore(secrets: secrets, biometrics: biometrics), secrets: secrets,
            biometrics: biometrics, hostKeys: hostKeys, prompter: prompter
        )
    }

    @Test(.timeLimit(.minutes(1)))
    func monitorRecoversAfterSignInAndTabAttachesAfterAnswering() async throws {
        let server = CodeEveryLogin()
        let (host, port) = try await Self.startServer(server)
        let hostKeys = FileHostKeyStore(url: SSHServerTests.scratchURL())
        let profile = HostProfile(
            id: UUID(), name: "2fa", endpoint: HostEndpoint(host: "127.0.0.1", port: port, user: "u"), auth: .password
        )
        let record = HostRecord(id: profile.id, profile: profile, symbol: "laptop")
        let signedIn = SignedIn()
        let silent = CodePrompter(code: nil)

        let monitor = HostMonitor(
            record: record,
            factory: { record in
                if let connection = signedIn.connection.withLock({ $0.take() }) {
                    #expect(await connection.isConnected)
                    return FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [], approvals: [])])
                }
                let connection = Self.connection(record.profile, hostKeys: hostKeys, prompter: silent)
                do {
                    try await connection.connect()
                } catch {
                    await connection.close()
                    throw error
                }
                return FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [], approvals: [])])
            },
            path: FakePath()
        )
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually(.seconds(10)) { log.reach == .authNeeded })
        #expect(server.logins.withLock { $0 } == 0)

        let answering = CodePrompter(code: "123456")
        let user = Self.connection(profile, hostKeys: hostKeys, prompter: answering)
        try await user.connect()
        signedIn.connection.withLock { $0 = user }
        await monitor.refresh()
        #expect(await eventually(.seconds(10)) { log.reach == .online })
        #expect(answering.asked.withLock { $0 } == 1)

        let tab = TerminalLink(connect: { _, _, _ in
            let connection = Self.connection(profile, hostKeys: hostKeys, prompter: answering)
            try await connection.connect()
            return Attachment(transport: FakeTransport(), kind: .ssh, notes: [], connection: connection)
        }, path: FakePath(), cols: 80, rows: 24)
        await tab.start()
        var states = tab.states.makeAsyncIterator()
        #expect(await states.next() == .connecting)
        guard case .live? = await states.next() else {
            Issue.record("the tab did not attach after the code was answered")
            return
        }
        #expect(answering.asked.withLock { $0 } == 2)
        await tab.stop()

        let declined = TerminalLink(connect: { _, _, _ in
            let connection = Self.connection(profile, hostKeys: hostKeys, prompter: silent)
            do {
                try await connection.connect()
            } catch {
                await connection.close()
                throw error
            }
            return Attachment(transport: FakeTransport(), kind: .ssh, notes: [], connection: connection)
        }, path: FakePath(), cols: 80, rows: 24)
        await declined.start()
        var declinedStates = declined.states.makeAsyncIterator()
        #expect(await declinedStates.next() == .connecting)
        guard case .failed? = await declinedStates.next() else {
            Issue.record("a declined code should fail the tab")
            return
        }
        await declined.stop()
        await monitor.stop()
        try await host.close()
    }
}
