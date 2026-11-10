import Foundation
import TogoKit

public enum MoshFactory {
    /// Starts mosh-server over `connection` and returns a transport aimed at the address the
    /// server listens on. That is normally the address the connection reached (`-s`); a
    /// link-local IPv6 reach is swapped for another of the machine's addresses first, since
    /// datagrams to an unscoped fe80:: address go nowhere.
    public static func make(
        connection: SSHConnection, profile: HostProfile, argv: [String], cols: Int, rows: Int
    ) async throws -> MoshTransport {
        guard let target = await connection.moshTarget() else {
            throw TransportAddressError.linkLocalOnly(host: await connection.route ?? profile.endpoint.host)
        }
        let command = MoshBootstrap.command(
            argv: argv, serverPath: profile.moshServerPath, ports: profile.moshPorts, bind: target.bind
        )
        let reply = try await connection.run(command, timeout: .seconds(10))
        let endpoint = try MoshBootstrap.parse(stdout: reply.stdout, stderr: reply.stderr, status: reply.status, host: target.host)
        // A server the client never heard from would otherwise wait out its network timeout.
        var stopServer: (@Sendable () async -> Void)?
        if let pid = endpoint.serverPID {
            let command = MoshBootstrap.stopCommand(pid: pid)
            stopServer = { _ = try? await connection.run(command, timeout: .seconds(3)) }
        }
        return MoshTransport(endpoint: endpoint, cols: cols, rows: rows, stopServer: stopServer)
    }
}
