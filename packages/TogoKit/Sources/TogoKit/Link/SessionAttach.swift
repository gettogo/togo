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
    public static func connect(profile: HostProfile, sessionID: String, env: SessionEnvironment) -> LinkConnect {
        { cols, rows, onWaking in
            let probe = profile.jumps.first ?? profile.endpoint
            try await WakeProbe.reach(
                host: probe.host, port: probe.port, clock: SystemClock(), onWaking: onWaking,
                attempt: { await WakeProbe.tcpConnect(host: probe.host, port: probe.port, timeout: .seconds(2)) }
            )
            let connection = env.connection(for: profile)
            do {
                try await connection.connect()
                let argv = try await attachArgv(sessionID: sessionID, over: connection)
                let request = AttachRequest(profile: profile, connection: connection, argv: argv, cols: cols, rows: rows)
                return try await env.connector.attach(request)
            } catch {
                await connection.close()
                throw error
            }
        }
    }

    private static func attachArgv(sessionID: String, over connection: SSHConnection) async throws -> [String] {
        let helper = HelperClient(channel: try await connection.openExec(HelperCommand.serveStdio))
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
