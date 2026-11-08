import Foundation
@preconcurrency import LocalAuthentication

public protocol SecretStore: Sendable {
    func set(_ data: Data, for account: String, requireUserPresence: Bool) throws
    /// May block while the system shows a passcode or biometric prompt, so call it off the
    /// main actor. A context already evaluated by `BiometricSession` suppresses the prompt.
    func get(_ account: String, context: LAContext?) throws -> Data?
    func delete(_ account: String) throws
}

/// A keychain call that failed. The status is the error code, so diagnostics name the
/// actual keychain answer rather than one code for every failure.
public struct SecretStoreError: Error, Equatable, CustomNSError, LocalizedError {
    public var status: Int32

    public init(status: Int32) {
        self.status = status
    }

    public static var errorDomain: String { "TogoKit.SecretStoreError" }
    public var errorCode: Int { Int(status) }

    public var errorDescription: String? {
        String(localized: "Togo could not unlock the saved key or password on this device. Sign in again to unlock it.", bundle: .module)
    }
}
