#if DEBUG
import Foundation
import TogoKit
import TogoMosh

/// The one set of stores, prompter and connector the debug screens share.
@MainActor final class DebugComposition {
    static let shared = DebugComposition()

    let env: SessionEnvironment
    let hosts = DebugHostStore()

    private static let deviceKeyIDDefault = "debug.deviceKeyID"
    private static let client = ClientInfo(name: "togo-ios-debug", version: "0.1.0")

    private init() {
        let secrets = KeychainSecretStore()
        let biometrics = BiometricSession.app
        let keys = KeychainKeyStore(secrets: secrets, biometrics: biometrics)
        let support = URL.applicationSupportDirectory
        try? FileManager.default.createDirectory(at: support, withIntermediateDirectories: true)
        let hostKeys = FileHostKeyStore(url: support.appending(path: "known_hosts.json"))
        let mosh = TransportFactory(kind: .mosh, unavailableReason: TransportFactory.moshUnavailableReason) { request in
            try await MoshFactory.make(
                connection: request.connection, profile: request.profile, argv: request.argv,
                cols: request.cols, rows: request.rows
            )
        }
        env = SessionEnvironment(
            keys: keys, secrets: secrets, biometrics: biometrics, hostKeys: hostKeys,
            prompter: DebugPrompter(), connector: Connector(factories: [mosh, .ssh])
        )
    }

    /// The device key, made on first use. On a device it lives in the Secure Enclave.
    func deviceKey() async throws -> KeyInfo {
        let keys = env.keys
        if let id = UserDefaults.standard.string(forKey: Self.deviceKeyIDDefault),
           let key = try await keys.list().first(where: { $0.id == id }) {
            return key
        }
        let key = try await keys.generateDeviceKey(label: "Togo device key")
        UserDefaults.standard.set(key.id, forKey: Self.deviceKeyIDDefault)
        return key
    }

    /// Opens a fresh connection and helper channel, runs `body`, then closes both.
    func withHelper<T: Sendable>(
        _ profile: HostProfile, _ body: @escaping @Sendable (HelperClient) async throws -> T
    ) async throws -> T {
        let connection = env.connection(for: profile)
        defer { Task { await connection.close() } }
        try await connection.connect()
        let helper = HelperClient(channel: try await connection.openExec(HelperCommand.serveStdio))
        do {
            _ = try await helper.hello(client: Self.client)
            let value = try await body(helper)
            await helper.close()
            return value
        } catch {
            await helper.close()
            throw error
        }
    }
}
#endif
