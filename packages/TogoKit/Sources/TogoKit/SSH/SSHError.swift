import Foundation

public enum SSHError: Error, Equatable, LocalizedError {
    /// The host presented a key other than the one trusted for it. Only forgetting the
    /// stored key gets past this, so a man in the middle cannot be clicked through.
    case hostKeyChanged(host: String, expected: [String], presented: String)
    case hostKeyRejected
    case authFailed(tried: [String])
    case cancelled
    case unreachable(String)
    case timeout
    case channelClosed(exitStatus: Int32?)

    public var errorDescription: String? {
        switch self {
        case .hostKeyChanged(let host, let expected, let presented):
            "The key for \(Self.displayHost(host)) is not the one you trusted. "
                + "Trusted: \(expected.joined(separator: ", ")). Sent now: \(presented). "
                + "If nobody changed or reinstalled this machine, someone may be intercepting the connection. "
                + "Togo will not connect until the old key is removed."
        case .hostKeyRejected:
            "The machine's key was not trusted, so the connection was closed."
        case .authFailed(let tried) where tried.isEmpty:
            "The machine did not accept the login."
        case .authFailed(let tried):
            "The machine did not accept the login (tried \(tried.map(Self.methodName).joined(separator: ", ")))."
        case .cancelled:
            "The connection was cancelled."
        case .unreachable(let detail):
            detail.hasSuffix(".") ? detail : detail + "."
        case .timeout:
            "The machine took too long to answer."
        case .channelClosed(nil):
            "The connection closed unexpectedly."
        case .channelClosed:
            "The command on the machine stopped."
        }
    }

    /// Host ids are stored as `[name]:port`; people know the name, and the port only when it is unusual.
    static func displayHost(_ hostID: String) -> String {
        guard hostID.hasPrefix("["), let close = hostID.lastIndex(of: "]") else { return hostID }
        let name = String(hostID[hostID.index(after: hostID.startIndex)..<close])
        let port = hostID[close...].dropFirst(2)
        return port.isEmpty || port == "22" ? name : "\(name) port \(port)"
    }

    private static func methodName(_ method: String) -> String {
        switch method {
        case "publickey": "key"
        case "keyboard-interactive": "password prompt"
        default: method
        }
    }
}
