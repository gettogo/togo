import Foundation

extension HostMonitor {
    typealias Probe = @Sendable (HostRecord, any LinkClock, @escaping @Sendable () -> Void) async throws -> Void

    /// Any known route answering is enough, as for terminals: a Mac saved by its LAN address
    /// is still reached over Tailscale away from home, since the connection races them all.
    static let wakeProbe: Probe = { record, clock, onWaking in
        let target = record.profile.jumps.first ?? record.profile.endpoint
        let hosts = await probeHosts(record.profile, book: .shared)
        try await WakeProbe.reach(
            host: record.profile.jumps.isEmpty ? record.profile.name : target.host, port: target.port, clock: clock,
            onWaking: { _ in onWaking() },
            attempt: { await WakeProbe.anyReachable(hosts: hosts, port: target.port, timeout: .seconds(2)) }
        )
    }

    /// The hosts the wake probe tries: every route for a direct machine, the first jump otherwise.
    static func probeHosts(_ profile: HostProfile, book: RouteBook) async -> [String] {
        guard profile.jumps.isEmpty else { return [profile.jumps[0].host] }
        let routes = await book.candidates(for: profile)
        return routes.isEmpty ? [profile.endpoint.host] : routes
    }

}
