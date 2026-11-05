@preconcurrency import Citadel
import Crypto
import Foundation
import NIOCore
import NIOPosix
@preconcurrency import NIOSSH
import Testing
@testable import TogoKit

/// Accepts exactly the password "pw".
private final class PasswordOnly: NIOSSHServerUserAuthenticationDelegate, Sendable {
    var supportedAuthenticationMethods: NIOSSHAvailableUserAuthenticationMethods { .password }

    func requestReceived(
        request: NIOSSHUserAuthenticationRequest, responsePromise: EventLoopPromise<NIOSSHUserAuthenticationOutcome>
    ) {
        if case .password(let password) = request.request, password.password == "pw" {
            responsePromise.succeed(.success)
        } else {
            responsePromise.succeed(.failure)
        }
    }
}

/// Answers every exec with "<name>: <command>" so a test can tell which server ran it.
private final class NamedExec: ExecDelegate {
    private struct Context: ExecCommandContext {
        func terminate() async throws {}
    }

    let name: String

    init(name: String) {
        self.name = name
    }

    func start(command: String, outputHandler: ExecOutputHandler) async throws -> any ExecCommandContext {
        let output = outputHandler.stdoutPipe.fileHandleForWriting
        output.write(Data("\(name): \(command)\n".utf8))
        try output.close()
        // Citadel relays the pipe on another thread and sends exit-status at once, so an
        // immediate exit can close the channel before the output is relayed.
        try await Task.sleep(for: .milliseconds(200))
        outputHandler.succeed(exitCode: 0)
        return Context()
    }

    func setEnvironmentValue(_ value: String, forKey key: String) async throws {}
}

/// Forwards direct-tcpip to the requested port. Citadel's server already puts a buffer codec
/// on the child channel and its own forwarder in 0.12.1 adds a second one, which traps.
private struct Forwarder: DirectTCPIPDelegate {
    func initializeDirectTCPIPChannel(
        _ channel: any Channel, request: SSHChannelType.DirectTCPIP, context: SSHContext
    ) -> EventLoopFuture<Void> {
        ClientBootstrap(group: channel.eventLoop)
            .channelInitializer { remote in
                remote.pipeline.addHandler(Pump { data in
                    channel.writeAndFlush(data, promise: nil)
                })
            }
            .connect(host: request.targetHost, port: request.targetPort)
            .flatMap { remote in
                channel.closeFuture.whenComplete { _ in remote.close(promise: nil) }
                remote.closeFuture.whenComplete { _ in channel.close(promise: nil) }
                return channel.pipeline.addHandler(Pump { data in remote.writeAndFlush(data, promise: nil) })
            }
    }
}

private final class Pump: ChannelInboundHandler, Sendable {
    typealias InboundIn = ByteBuffer
    let forward: @Sendable (ByteBuffer) -> Void

    init(forward: @escaping @Sendable (ByteBuffer) -> Void) {
        self.forward = forward
    }

    func channelRead(context: ChannelHandlerContext, data: NIOAny) {
        forward(unwrapInboundIn(data))
    }
}

private final class Echo: ChannelInboundHandler, Sendable {
    typealias InboundIn = ByteBuffer
    typealias OutboundOut = ByteBuffer

    func channelRead(context: ChannelHandlerContext, data: NIOAny) {
        context.writeAndFlush(data, promise: nil)
    }
}

struct SSHServerTests {
    /// A fixed host key: tests never accept arbitrary keys.
    static func scratchURL() -> URL {
        FileManager.default.temporaryDirectory.appendingPathComponent("togo-hosts-\(UUID().uuidString).json")
    }

    static func hostKey(_ seed: UInt8) throws -> NIOSSHPrivateKey {
        NIOSSHPrivateKey(ed25519Key: try Curve25519.Signing.PrivateKey(rawRepresentation: Data(repeating: seed, count: 32)))
    }

    static func startServer(seed: UInt8, name: String, forwards: Bool = false, exec: (any ExecDelegate)? = nil) async throws -> (SSHServer, Int) {
        for _ in 0..<20 {
            let port = Int.random(in: 20_000...60_000)
            guard let server = try? await SSHServer.host(
                host: "127.0.0.1", port: port, hostKeys: [try hostKey(seed)], authenticationDelegate: PasswordOnly()
            ) else { continue }
            server.enableExec(withDelegate: exec ?? NamedExec(name: name))
            if forwards { server.enableDirectTCPIP(withDelegate: Forwarder()) }
            return (server, port)
        }
        throw SSHError.unreachable("no free port")
    }

    static func connection(
        _ endpoint: HostEndpoint, jumps: [HostEndpoint] = [], password: String = "pw",
        trust: Bool = true, hostKeys: (any HostKeyStore)? = nil
    ) -> (SSHConnection, ScriptedPrompter) {
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession(evaluator: CountingEvaluator())
        let prompter = ScriptedPrompter(trust: trust, password: password)
        let profile = HostProfile(id: UUID(), name: "test", endpoint: endpoint, jumps: jumps, auth: .password)
        let connection = SSHConnection(
            profile: profile, keys: KeychainKeyStore(secrets: secrets, biometrics: biometrics), secrets: secrets,
            biometrics: biometrics, hostKeys: hostKeys ?? FileHostKeyStore(url: scratchURL()),
            prompter: prompter
        )
        return (connection, prompter)
    }

    @Test func passwordAuthAgainstInProcessServer() async throws {
        let (server, port) = try await Self.startServer(seed: 1, name: "a")
        let endpoint = HostEndpoint(host: "127.0.0.1", port: port, user: "u")

        let (good, _) = Self.connection(endpoint, password: "pw")
        try await good.connect()
        let result = try await good.run("echo ok", timeout: .seconds(10))
        #expect(String(decoding: result.stdout, as: UTF8.self) == "a: echo ok\n")
        #expect(result.status == 0)
        await good.close()

        let (bad, _) = Self.connection(endpoint, password: "bad")
        await #expect(throws: SSHError.authFailed(tried: ["password"])) { try await bad.connect() }
        try await server.close()
    }

    @Test func jumpHostReachesSecondServer() async throws {
        let (jump, jumpPort) = try await Self.startServer(seed: 2, name: "jump", forwards: true)
        let (target, targetPort) = try await Self.startServer(seed: 3, name: "target")
        let (connection, prompter) = Self.connection(
            HostEndpoint(host: "127.0.0.1", port: targetPort, user: "u"),
            jumps: [HostEndpoint(host: "127.0.0.1", port: jumpPort, user: "u")],
            password: "pw"
        )
        try await connection.connect()
        let result = try await connection.run("hostname", timeout: .seconds(10))
        #expect(String(decoding: result.stdout, as: UTF8.self) == "target: hostname\n")
        // Each hop's key is checked on its own.
        #expect(prompter.trustCalls.withLock { $0 } == 2)
        #expect(await connection.remoteAddress == nil)
        await connection.close()
        try await jump.close()
        try await target.close()
    }

    @Test func forwardsThroughTestSSHServer() async throws {
        let echo = try await ServerBootstrap(group: MultiThreadedEventLoopGroup.singleton)
            .childChannelInitializer { $0.pipeline.addHandler(Echo()) }
            .bind(host: "127.0.0.1", port: 0).get()
        let echoPort = try #require(echo.localAddress?.port)
        let (server, port) = try await Self.startServer(seed: 4, name: "forward", forwards: true)
        let (connection, _) = Self.connection(HostEndpoint(host: "127.0.0.1", port: port, user: "u"))
        try await connection.connect()

        let forward = LocalForward(
            opener: connection, target: ForwardTarget(ListeningPort(port: echoPort, address: "*", pid: 1, command: "echo"))!,
            preferredLocalPort: nil
        )
        let local = try await forward.start()
        let socket = try #require(LoopbackSocket.connect(port: local))
        socket.send("ping")
        #expect(socket.read(count: 4) == Data("ping".utf8))
        let payload = String(repeating: "0123456789abcdef", count: 64 * 1024)
        socket.send(payload)
        #expect(socket.read(count: payload.utf8.count) == Data(payload.utf8))

        let closedPort = LoopbackSocket.freePort()
        let refused = LocalForward(
            opener: connection,
            target: ForwardTarget(ListeningPort(port: closedPort, address: "127.0.0.1", pid: 1, command: "gone"))!,
            preferredLocalPort: nil
        )
        let probe = try #require(LoopbackSocket.connect(port: try await refused.start()))
        var events = refused.events.makeAsyncIterator()
        #expect(await events.next() == .upstreamRefused)
        #expect(probe.readsEOF())
        await refused.stop()

        // Dropping the SSH connection closes every piped connection and the listener.
        await connection.close()
        #expect(socket.readsEOF())
        #expect(await eventually { LoopbackSocket.connect(port: local) == nil })
        await forward.stop()
        try await echo.close()
        try await server.close()
    }
}
