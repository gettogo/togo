import ETBootstrap
import ETSession
import Foundation

/// Runs the etterminal handshake over an existing SSH connection.
struct SSHBootstrapExecutor: ETBootstrapExecutor {
    let connection: SSHConnection

    func run(command: String) async throws -> String {
        let reply = try await connection.run(command, timeout: .seconds(10))
        return String(decoding: reply.stdout + reply.stderr, as: UTF8.self)
    }
}

public enum ETBootstrapFailure: Error, Equatable, LocalizedError {
    case notStarted

    public var errorDescription: String? {
        String(
            localized: "etterminal did not start on the machine. Check that Eternal Terminal is installed and etserver is running.",
            bundle: .module
        )
    }
}

public enum ETFactory {
    public static let port: UInt16 = 2022

    /// Non-interactive SSH sessions often lack Homebrew on PATH, so its install
    /// locations are tried after PATH.
    static let bootstrapOptions = ETBootstrapOptions(
        etterminalPath: #""$(command -v etterminal || ls /opt/homebrew/bin/etterminal /usr/local/bin/etterminal 2>/dev/null | head -n 1)""#
    )

    /// The keystrokes that replace the remote login shell with `argv`.
    static func attachInput(_ argv: [String]) -> Data {
        Data("exec \(shellQuote(argv))\n".utf8)
    }

    /// Starts etterminal over the request's SSH connection and returns a transport aimed
    /// at the address that connection reached.
    public static func make(_ r: AttachRequest) async throws -> ETTransport {
        let credentials: ETCredentials
        do {
            credentials = try await ETBootstrap(options: bootstrapOptions).run(using: SSHBootstrapExecutor(connection: r.connection))
        } catch is ETBootstrapError {
            throw ETBootstrapFailure.notStarted
        }
        let host = await r.connection.remoteAddress ?? r.profile.endpoint.host
        let endpoint = ETEndpoint(host: host, port: port, clientID: credentials.clientID, passkey: credentials.passkey)
        return try ETTransport(endpoint: endpoint, argv: r.argv, cols: r.cols, rows: r.rows)
    }

    public static let factory = TransportFactory(kind: .et, unavailableReason: TransportFactory.etUnavailableReason) { request in
        try await make(request)
    }
}
