// Compiled out of Release, like everything under Debug.
#if DEBUG
import Foundation
@preconcurrency import LocalAuthentication
import TogoKit

/// Lets the native end-to-end UI tests drive a real helper on the Mac running the simulator.
/// Launch argument `-e2e-host`, with these environment variables:
///
/// - `TOGO_E2E_KEY`: an OpenSSH private key, base64, authorised on the Mac for loopback only.
/// - `TOGO_E2E_HOSTKEYS`: the Mac's host key fingerprints (`SHA256:...`), comma separated,
///   trusted up front because nothing answers a trust prompt.
/// - `TOGO_E2E_USER`, `TOGO_E2E_ADDRESS` (default 127.0.0.1), `TOGO_E2E_PORT` (default 22).
///
/// The simulator has no passcode or enrolled face, so keys are stored without user presence and
/// signing evaluates nothing. Every launch replaces the one machine it saves, under a fixed id.
enum E2EHost {
    static let machineID = UUID(uuidString: "E2E00000-0000-4000-8000-000000000001")!

    static var enabled: Bool { ProcessInfo.processInfo.arguments.contains("-e2e-host") }

    private static var env: [String: String] { ProcessInfo.processInfo.environment }

    /// Keychain storage that never asks for user presence.
    struct PlainSecrets: SecretStore {
        let base = KeychainSecretStore()
        func set(_ data: Data, for account: String, requireUserPresence: Bool) throws {
            try base.set(data, for: account, requireUserPresence: false)
        }
        func get(_ account: String, context: LAContext?) throws -> Data? { try base.get(account, context: nil) }
        func delete(_ account: String) throws { try base.delete(account) }
    }

    /// Passes every evaluation: nothing on the simulator could answer it.
    struct Evaluator: BiometricEvaluator {
        func evaluate(_ context: LAContext, reason: String) async throws {}
    }

    /// Imports the key, trusts the host keys and saves the machine, before the monitors start.
    static func seed(services: LiveServices, keys: KeychainKeyStore, hostKeys: any HostKeyStore) async {
        guard enabled, let encoded = env["TOGO_E2E_KEY"], let data = Data(base64Encoded: encoded),
              let user = env["TOGO_E2E_USER"] else { return }
        UserDefaults.standard.set(true, forKey: TutorialModel.Keys.seen)
        let address = env["TOGO_E2E_ADDRESS"] ?? "127.0.0.1"
        let port = Int(env["TOGO_E2E_PORT"] ?? "") ?? 22
        let endpoint = HostEndpoint(host: address, port: port, user: user)
        for print in (env["TOGO_E2E_HOSTKEYS"] ?? "").split(separator: ",") {
            try? await hostKeys.trust(String(print), for: HostKeyTrust.hostID(endpoint))
        }
        do {
            let key = try await keys.importKey(
                openSSH: String(decoding: data, as: UTF8.self), passphrase: nil, label: "E2E key"
            )
            let profile = HostProfile(
                id: machineID, name: "E2E Mac", endpoint: endpoint, jumps: [], auth: .key(id: key.id),
                transport: .ssh, moshServerPath: nil, etAvailable: false
            )
            try await services.saveHost(HostRecord(id: machineID, profile: profile, symbol: HostSymbol.laptop.rawValue))
        } catch {
            NSLog("E2EHost: seeding failed: %@", String(describing: error))
        }
    }
}
#endif
