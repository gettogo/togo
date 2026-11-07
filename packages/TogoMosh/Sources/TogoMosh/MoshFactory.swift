import Foundation
import TogoKit

public enum MoshFactory {
    /// Starts mosh-server over `connection` and returns a transport aimed at the address
    /// that connection reached, since the server binds to that address (`-s`).
    public static func make(
        connection: SSHConnection, profile: HostProfile, argv: [String], cols: Int, rows: Int
    ) async throws -> MoshTransport {
        let command = MoshBootstrap.command(argv: argv, serverPath: profile.moshServerPath, ports: profile.moshPorts)
        let reply = try await connection.run(command, timeout: .seconds(10))
        let reached = await connection.remoteAddress
        let route = await connection.route
        let host = reached ?? route ?? profile.endpoint.host
        let endpoint = try MoshBootstrap.parse(stdout: reply.stdout, stderr: reply.stderr, status: reply.status, host: host)
        return MoshTransport(endpoint: endpoint, cols: cols, rows: rows)
    }
}
