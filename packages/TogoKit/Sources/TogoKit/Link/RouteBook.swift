import Foundation

/// The newest routes for each machine and the route each one last connected over, shared
/// by every connection in the process.
///
/// A monitor or terminal keeps the profile it started with, so routes the helper reported
/// since then live here: the next connect or reconnect races them without restarting
/// anything. The app feeds it from its stored machines.
public actor RouteBook {
    public static let shared = RouteBook()

    public struct Use: Equatable, Sendable {
        public var route: String
        public var at: Date
    }

    private var routes: [UUID: [HostRoute]] = [:]
    private var used: [UUID: Use] = [:]
    /// Learned LAN addresses that answered with another machine's key while a different
    /// route connected. They are left out of every race from then on.
    private var mismatched: [UUID: Set<String>] = [:]

    public init() {}

    public func setRoutes(_ list: [HostRoute], for id: UUID) {
        routes[id] = list
    }

    public func forget(_ id: UUID) {
        routes[id] = nil
        used[id] = nil
        mismatched[id] = nil
    }

    /// The routes to race for `profile`, newest knowledge first.
    public func candidates(for profile: HostProfile) -> [String] {
        let known = RoutePlan.droppingMismatched(routes[profile.id] ?? profile.routes, mismatched[profile.id] ?? [])
        return RoutePlan.candidates(profile, routes: known, last: used[profile.id]?.route)
    }

    /// Every route known for `profile`, with its kind, newest knowledge first.
    public func routes(for profile: HostProfile) -> [HostRoute] {
        RoutePlan.droppingMismatched(routes[profile.id] ?? profile.routes, mismatched[profile.id] ?? [])
    }

    /// Routes that showed another key on a connect that succeeded over a different route.
    public func recordMismatches(_ hosts: [String], for id: UUID) {
        guard !hosts.isEmpty else { return }
        mismatched[id, default: []].formUnion(hosts.map { $0.lowercased() })
    }

    public func mismatches(_ id: UUID) -> Set<String> {
        mismatched[id] ?? []
    }

    /// The helper reported these addresses as its own again, so they may be raced.
    public func clearMismatches(_ hosts: [String], for id: UUID) {
        mismatched[id]?.subtract(hosts.map { $0.lowercased() })
    }

    public func recordUse(_ route: String, for id: UUID) {
        used[id] = Use(route: route, at: Date())
    }

    /// The route the newest connection to `id` won with, if any connected in this process.
    public func lastUse(_ id: UUID) -> Use? {
        used[id]
    }
}
