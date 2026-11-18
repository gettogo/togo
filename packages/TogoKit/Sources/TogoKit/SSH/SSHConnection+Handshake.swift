@preconcurrency import Citadel
import Foundation
import NIOCore

extension SSHConnection {
    func handshake(
        _ endpoint: HostEndpoint, via parent: SSHClient?,
        validator: SnapshotHostKeyValidator, delegate: PreparedAuthDelegate
    ) async throws -> (SSHClient, any Channel) {
        let known = await routeBook.routes(for: profile)
        let kind = known.first { $0.id == endpoint.host.lowercased() }?.kind ?? HostRoute.kind(of: endpoint.host)
        let timeout = await routeBook.connectionTimeout(for: profile, host: endpoint.host, direct: parent == nil, override: connectTimeout)
        let span = ConnectionAttemptSpan(hostID: profile.id, stage: .route(parent == nil ? kind : .other), purpose: purpose, report: attemptObserver)
        do {
            let result = try await performHandshake(endpoint, via: parent, validator: validator, delegate: delegate, timeout: timeout)
            if parent == nil { await routeBook.recordConnected(endpoint.host, for: profile.id) }
            span.end(.succeeded)
            return result
        } catch {
            span.end(ConnectionAttemptSpan.failure(error))
            throw error
        }
    }

    private func performHandshake(
        _ endpoint: HostEndpoint, via parent: SSHClient?,
        validator: SnapshotHostKeyValidator, delegate: PreparedAuthDelegate, timeout: Duration
    ) async throws -> (SSHClient, any Channel) {
        var settings = SSHClientSettings(
            host: endpoint.host, port: endpoint.port,
            authenticationMethod: { .custom(delegate) }, hostKeyValidator: .custom(validator)
        )
        let parts = timeout.components
        settings.connectTimeout = .milliseconds(parts.seconds * 1000 + parts.attoseconds / 1_000_000_000_000_000)
        settings.loginTimeout = KeyboardInteractiveResponder.loginTimeout
        let client: SSHClient
        if let parent {
            client = try await parent.jump(to: settings)
        } else {
            client = try await SSHClient.connect(to: settings)
        }
        // Session channels with a PTY and exec are opened on the root channel directly,
        // because Citadel opens a PTY only with a login shell.
        let channel = client.channel
        if parent == nil { await Self.keepAlive(channel) }
        // Mosh needs the address SSH reached, not a name that may resolve elsewhere later.
        if parent == nil, profile.jumps.isEmpty {
            remoteAddress = channel.remoteAddress?.ipAddress
            remoteInterface = Self.scopeInterface(channel.remoteAddress)
        }
        return (client, channel)
    }

}
