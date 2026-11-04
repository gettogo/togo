import Foundation
@preconcurrency import LocalAuthentication

public protocol SecretStore: Sendable {
    func set(_ data: Data, for account: String, requireUserPresence: Bool) throws
    /// May block while the system shows a passcode or biometric prompt, so call it off the
    /// main actor. A context already evaluated by `BiometricSession` suppresses the prompt.
    func get(_ account: String, context: LAContext?) throws -> Data?
    func delete(_ account: String) throws
}

public struct SecretStoreError: Error, Equatable {
    public var status: Int32

    public init(status: Int32) {
        self.status = status
    }
}
