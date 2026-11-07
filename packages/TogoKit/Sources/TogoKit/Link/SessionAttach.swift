import Foundation

public struct SessionEnvironment: Sendable {
    public var keys: KeychainKeyStore
    public var secrets: any SecretStore
    public var biometrics: BiometricSession
    public var hostKeys: any HostKeyStore
    public var prompter: any ConnectionPrompter
    public var connector: Connector

    public init(
        keys: KeychainKeyStore, secrets: any SecretStore, biometrics: BiometricSession,
        hostKeys: any HostKeyStore, prompter: any ConnectionPrompter, connector: Connector
    ) {
        self.keys = keys
        self.secrets = secrets
        self.biometrics = biometrics
        self.hostKeys = hostKeys
        self.prompter = prompter
        self.connector = connector
    }

    public func connection(for profile: HostProfile) -> SSHConnection {
        SSHConnection(
            profile: profile, keys: keys, secrets: secrets, biometrics: biometrics,
            hostKeys: hostKeys, prompter: prompter
        )
    }
}

public enum SessionAttach {
    static let client = ClientInfo(name: "togo-ios", version: "0.1.0")

    /// Every call builds a new SSH connection, so a reconnect never reuses one that the
    /// network change or sleep left half dead. Nothing from a failed attempt stays open.
    /// `helperPath` is the helper binary the host record names, if any.
    public static func connect(
        profile: HostProfile, sessionID: String, env: SessionEnvironment, helperPath: String? = nil
    ) -> LinkConnect {
        let command = helperPath.map { shellQuote([$0, "serve", "--stdio"]) } ?? HelperCommand.serveStdio
        return connect(profile: profile, env: env) { connection in
            try await attachArgv(sessionID: sessionID, command: command, over: connection)
        }
    }

    /// Attaches to a tmux session on a host without the helper, by the id `PlainTmuxSource` gave it.
    public static func connectPlainTmux(profile: HostProfile, sessionID: String, env: SessionEnvironment) -> LinkConnect {
        connect(profile: profile, env: env) { _ in
            guard let name = PlainTmux.name(forID: sessionID) else {
                throw HelperError.rpc(RPCErrorObject(code: RPCErrorCode.notFound, message: "no such session"))
            }
            return PlainTmux.attachArgv(name: name)
        }
    }

    private static func connect(
        profile: HostProfile, env: SessionEnvironment,
        argv: @escaping @Sendable (SSHConnection) async throws -> [String]
    ) -> LinkConnect {
        { cols, rows, onWaking in
            // Any route answering is enough: the connection races them all next.
            let probe = profile.jumps.first ?? profile.endpoint
            let hosts = profile.jumps.isEmpty ? await RouteBook.shared.candidates(for: profile) : [probe.host]
            try await WakeProbe.reach(
                host: profile.jumps.isEmpty ? profile.name : probe.host, port: probe.port, clock: SystemClock(),
                onWaking: onWaking,
                attempt: { await WakeProbe.anyReachable(hosts: hosts, port: probe.port, timeout: .seconds(2)) }
            )
            let connection = env.connection(for: profile)
            do {
                try await connection.connect()
                let request = AttachRequest(
                    profile: profile, connection: connection, argv: try await argv(connection), cols: cols, rows: rows
                )
                return try await env.connector.attach(request)
            } catch {
                await connection.close()
                throw error
            }
        }
    }

    private static func attachArgv(sessionID: String, command: String, over connection: SSHConnection) async throws -> [String] {
        let helper = HelperClient(channel: try await connection.openExec(command))
        do {
            _ = try await helper.hello(client: client)
            let argv = try await helper.attachCommand(id: sessionID)
            await helper.close()
            return argv
        } catch {
            await helper.close()
            throw error
        }
    }
}
