import Foundation
import TogoKit

/// The routes part of the machine form.
extension MachineDraft {
    /// The routes to save: the typed address as a manual route in place of the one the machine
    /// was saved under, then the rest as they were.
    static func routes(_ routes: [HostRoute], saved: String?, address: String) -> [HostRoute] {
        var kept = routes
        if let saved, saved.lowercased() != address.lowercased() {
            kept.removeAll { $0.id == saved.lowercased() && $0.manual }
        }
        kept.removeAll { $0.id == address.lowercased() }
        return [.manual(address)] + kept
    }

    /// The address typed for a new route, when it is one: a host name or IP address with no
    /// user, path or spaces, and not already listed.
    var newRouteHost: String? {
        let host = newRoute.trimmingCharacters(in: .whitespaces)
        guard !host.isEmpty, !host.hasPrefix("-"), !host.contains(where: { $0.isWhitespace || $0 == "@" || $0 == "/" }),
              !routes.contains(where: { $0.id == host.lowercased() }),
              host.lowercased() != address.trimmingCharacters(in: .whitespaces).lowercased() else { return nil }
        return host
    }

    mutating func addRoute() {
        guard let host = newRouteHost else { return }
        routes.append(.manual(host))
        newRoute = ""
    }

    /// Only an address the user added can go; the saved address is edited in its own field,
    /// and the helper's routes come back on the next connect anyway.
    func canRemove(_ route: HostRoute) -> Bool {
        route.manual && route.id != address.trimmingCharacters(in: .whitespaces).lowercased()
    }

    mutating func removeRoute(_ route: HostRoute) {
        guard canRemove(route) else { return }
        routes.removeAll { $0.id == route.id }
        if lastRoute?.lowercased() == route.id { lastRoute = nil }
    }
}
