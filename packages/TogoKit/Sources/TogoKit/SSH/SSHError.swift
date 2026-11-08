import Foundation

public enum SSHError: Error, Equatable, LocalizedError {
    /// The host presented a key other than the one trusted for it. Only forgetting the
    /// stored key gets past this, so a man in the middle cannot be clicked through.
    case hostKeyChanged(host: String, expected: [String], presented: String)
    case hostKeyRejected
    case authFailed(tried: [String])
    case cancelled
    case unreachable(String)
    /// The machine answered but turned the SSH port away: on a Mac, Remote Login is off.
    case refused
    case timeout
    case channelClosed(exitStatus: Int32?)

    public var errorDescription: String? {
        switch self {
        case .hostKeyChanged(let host, let expected, let presented):
            String(localized: "The key for \(Self.displayHost(host)) is not the one you trusted. Trusted: \(expected.joined(separator: ", ")). Sent now: \(presented). If nobody changed or reinstalled this machine, someone may be intercepting the connection. Togo will not connect until the old key is removed.", bundle: .module)
        case .hostKeyRejected:
            String(localized: "The machine's key wasn't trusted, so the connection was closed.", bundle: .module)
        case .authFailed(let tried) where tried.isEmpty:
            String(localized: "The machine didn't accept the login.", bundle: .module)
        case .authFailed(let tried):
            String(localized: "The machine did not accept the login (tried \(tried.map(Self.methodName).joined(separator: ", "))).", bundle: .module)
        case .cancelled:
            String(localized: "The connection was cancelled.", bundle: .module)
        case .unreachable(let detail):
            detail.hasSuffix(".") ? detail : detail + "."
        case .refused:
            String(localized: "The machine turned the connection away: nothing takes SSH on it. On a Mac, turn on System Settings › General › Sharing › Remote Login; elsewhere, start the SSH server. togo doctor on the machine checks this.", bundle: .module)
        case .timeout:
            String(localized: "The machine took too long to answer.", bundle: .module)
        case .channelClosed(nil):
            String(localized: "The connection closed unexpectedly.", bundle: .module)
        case .channelClosed:
            String(localized: "The command on the machine stopped.", bundle: .module)
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
        case "publickey": String(localized: "key", bundle: .module)
        case "keyboard-interactive": String(localized: "password prompt", bundle: .module)
        default: method
        }
    }
}
