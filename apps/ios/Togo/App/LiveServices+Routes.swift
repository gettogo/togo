import Foundation
import os
import TogoKit

/// Keeping each machine's routes in step with what its helper reports and with the route
/// that last connected, and handing them to every connection through the route book.
extension LiveServices {
    private static let routeLog = Logger(subsystem: "app.togo", category: "routes")

    /// Gives running monitors and terminals the stored routes, so their next reconnect races
    /// them without starting over.
    func shareRoutes(_ all: [HostRecord], removed: [HostID]) {
        let book = RouteBook.shared
        let routes = all.map { ($0.id, $0.profile.routes) }
        Task {
            for (id, list) in routes { await book.setRoutes(list, for: id) }
            for id in removed { await book.forget(id) }
        }
    }

    /// Stores the routes an online `snapshot` reports, and the route that just worked, when
    /// either changed.
    func learnRoutes(from snapshot: HostSnapshot, record: HostRecord) {
        guard snapshot.reach == .online else { return }
        let reported = snapshot.hostInfo?.hostRoutes
        let id = record.id
        let book = RouteBook.shared
        let diagnostics = deps.diagnostics
        Task {
            // An address the machine reports as its own may be raced again.
            if let reported { await book.clearMismatches(reported.map(\.host), for: id) }
            let last = await book.lastUse(id)?.route
            let mismatched = await book.mismatches(id)
            await learnRoutes(id, reported: reported, last: last, mismatched: mismatched, diagnostics: diagnostics)
        }
    }

    /// Updates the machine's latest stored record, so an edit made since the report arrived
    /// is kept. Learned LAN addresses that answered with another device's key are dropped.
    func learnRoutes(
        _ id: HostID, reported: [HostRoute]?, last: String?, mismatched: Set<String> = [],
        diagnostics: DiagnosticsRecorder? = nil
    ) async {
        guard var current = await hostStore.hosts().first(where: { $0.id == id }) else { return }
        var profile = RoutePlan.learned(current.profile, reported: reported, last: last) ?? current.profile
        let kept = RoutePlan.droppingMismatched(profile.routes, mismatched)
        if kept.count != profile.routes.count {
            if let diagnostics {
                diagnostics.record(.link, "route_key_mismatch", [
                    "machine": .machine(diagnostics.log.token(for: id)),
                    "dropped": .int(profile.routes.count - kept.count),
                ])
            }
            profile.routes = kept
        }
        guard profile != current.profile else { return }
        current.profile = profile
        do {
            try await saveHost(current)
        } catch {
            Self.routeLog.error("Could not store the routes for a machine: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// How the machine was last reached in this run, for its details.
    func routeInUse(_ id: HostID) async -> String? {
        await RouteBook.shared.lastUse(id)?.route
    }
}
