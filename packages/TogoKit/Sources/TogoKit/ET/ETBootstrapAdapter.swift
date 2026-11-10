import ETBootstrap
import ETSession
import Foundation

/// Runs the etterminal handshake over an existing SSH connection.
///
/// etterminal daemonizes once it has printed its credentials, so its pid never reaches us.
/// It is started through a link named after `tag` instead, which its command line keeps,
/// so a start whose client never connects can stop exactly that etterminal.
struct SSHBootstrapExecutor: ETBootstrapExecutor {
    let connection: SSHConnection
    let tag: String

    func run(command: String) async throws -> String {
        let reply = try await connection.run(ETFactory.launchCommand(command, tag: tag), timeout: .seconds(10))
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
    static let locateETTerminal =
        "$(command -v etterminal || ls /opt/homebrew/bin/etterminal /usr/local/bin/etterminal 2>/dev/null | head -n 1)"

    /// `launchCommand` sets `togo_et` to the tagged link, or to etterminal itself.
    static let bootstrapOptions = ETBootstrapOptions(etterminalPath: #""$togo_et""#)

    /// Wraps ETBootstrap's command so etterminal runs as `$TMPDIR/togo-et-<tag>`. The link
    /// goes once etterminal has started; its command line keeps the name. `/bin/sh` runs
    /// it whatever the login shell is.
    static func launchCommand(_ command: String, tag: String) -> String {
        let link = "\"${TMPDIR:-/tmp}/togo-et-\(tag)\""
        return shellCommand(
            "togo_real=\"\(locateETTerminal)\"; togo_et=\(link); "
                + "ln -s \"$togo_real\" \"$togo_et\" 2>/dev/null || togo_et=\"$togo_real\"; "
                + "\(command); rm -f \(link)"
        )
    }

    /// Stops the etterminal a failed start left running, found by its tagged name. The
    /// bracket keeps this command's own shell from matching the pattern.
    static func stopCommand(tag: String) -> String {
        let pattern = "togo-et-[\(tag.prefix(1))]\(tag.dropFirst())"
        return shellCommand("pkill -f '\(pattern)' 2>/dev/null; exit 0")
    }

    static func newTag() -> String {
        String(UUID().uuidString.replacingOccurrences(of: "-", with: "").prefix(16)).lowercased()
    }

    /// The keystrokes that replace the remote login shell with `argv`.
    static func attachInput(_ argv: [String]) -> Data {
        Data("exec \(shellQuote(argv))\n".utf8)
    }

    /// Starts etterminal over the request's SSH connection and returns a transport aimed
    /// at the address that connection reached. etserver listens on every address, so a
    /// link-local reach is swapped for another of the machine's addresses (or scoped to its
    /// interface) the same way Mosh's is, and no bind needs to follow it.
    public static func make(_ r: AttachRequest) async throws -> ETTransport {
        guard let target = await r.connection.moshTarget() else {
            throw TransportAddressError.linkLocalOnly(host: await r.connection.route ?? r.profile.endpoint.host)
        }
        let tag = newTag()
        let credentials: ETCredentials
        do {
            credentials = try await ETBootstrap(options: bootstrapOptions)
                .run(using: SSHBootstrapExecutor(connection: r.connection, tag: tag))
        } catch is ETBootstrapError {
            throw ETBootstrapFailure.notStarted
        }
        let endpoint = ETEndpoint(host: target.host, port: port, clientID: credentials.clientID, passkey: credentials.passkey)
        let connection = r.connection
        let stop = stopCommand(tag: tag)
        return try ETTransport(endpoint: endpoint, argv: r.argv, cols: r.cols, rows: r.rows) {
            _ = try? await connection.run(stop, timeout: .seconds(3))
        }
    }

    public static let factory = TransportFactory(kind: .et, unavailableReason: TransportFactory.etUnavailableReason) { request in
        try await make(request)
    }
}
