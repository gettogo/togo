import Foundation

/// The loopback address and port on the machine that a forwarded connection reaches.
///
/// Built only from a port the machine reported as listening. A server bound to every
/// address is reached over loopback; one bound only to another address is not offered,
/// so a forward can never reach past the machine itself.
public struct ForwardTarget: Hashable, Sendable {
    public private(set) var host: String
    public private(set) var port: Int

    /// Nil when the port is out of range or the server listens on no loopback address.
    public init?(_ port: ListeningPort) {
        let host = Self.host(for: port.address)
        guard Self.isAllowed(host: host, port: port.port) else { return nil }
        self.host = host
        self.port = port.port
    }

    static func host(for address: String) -> String {
        switch address {
        case "0.0.0.0", "*", "": "127.0.0.1"
        case "::", "[::]", "[::1]": "::1"
        default: address
        }
    }

    /// The only destinations a direct-tcpip channel may name.
    static func isAllowed(host: String, port: Int) -> Bool {
        ["127.0.0.1", "::1", "localhost"].contains(host) && (1...65_535).contains(port)
    }
}
