import Foundation

public enum HelperError: Error, Equatable, Sendable, LocalizedError {
    /// The helper answered with a JSON-RPC error object.
    case rpc(RPCErrorObject)
    /// No response arrived within the client's call timeout. A late response is dropped.
    case timeout(method: String)
    /// The channel ended, sent an oversized frame or sent bytes that are not protocol v1.
    /// Every pending and later call fails with this.
    case closed
    /// A call other than `hello` was made before `hello` succeeded.
    case notReady
    case unsupported(capability: String)
    /// The response's `result` did not have the shape the method promises.
    case malformedResponse

    /// The helper asked for has no tmux session with this id: it was killed or ended.
    public var isSessionGone: Bool {
        if case .rpc(let error) = self { error.code == RPCErrorCode.notFound } else { false }
    }

    public var errorDescription: String? {
        switch self {
        case .rpc(let error): Self.describe(error)
        case .timeout: "The Togo helper on the host did not answer in time."
        case .closed: "The connection to the Togo helper on the host closed."
        case .notReady: "The Togo helper on the host is not ready yet."
        case .unsupported: "The Togo helper on the host is too old for this. Update it on the host."
        case .malformedResponse: "The Togo helper on the host sent a reply that could not be read."
        }
    }

    private static func describe(_ error: RPCErrorObject) -> String {
        switch error.code {
        case RPCErrorCode.notFound: "This session has ended."
        case RPCErrorCode.tmuxUnavailable: "The host needs tmux 3.2 or later."
        case RPCErrorCode.notReady: "The Togo helper on the host was asked before it was ready."
        case RPCErrorCode.invalidPath: "That folder does not exist on the host."
        case RPCErrorCode.unsupportedVersion: "The Togo helper on the host speaks a different protocol version. Update the app or the helper."
        case RPCErrorCode.conflict: "That request was already answered."
        case RPCErrorCode.notManaged: "That session was not started from Togo, so it cannot be changed from here."
        default: "The Togo helper on the host could not do that."
        }
    }
}
