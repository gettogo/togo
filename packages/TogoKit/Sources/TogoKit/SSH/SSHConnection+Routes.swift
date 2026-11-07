@preconcurrency import Citadel
import Foundation
import NIOCore
import Synchronization

/// Connecting by racing every route a machine is known by. Only a machine whose host key is
/// already pinned is raced, and only a route that shows that same key may sign in. A route
/// that shows another key, such as an old home address now used by a different device, is
/// dropped from the race before any credential is sent, and the others go on. The key-changed
/// warning comes only when no route showed the pinned key and at least one showed another.
/// A first connect, with nothing pinned yet, goes to the saved address alone and asks about
/// the key as before.
extension SSHConnection {
    /// One route's connection, signed in.
    struct RouteWin: @unchecked Sendable {
        var client: SSHClient
        var channel: any Channel
        var credentials: Credentials
    }

    /// The routes to race, or nil to connect the usual way: through a jump host, with one
    /// route only, or before any key is pinned for the saved address.
    func racedRoutes() async throws -> [String]? {
        guard profile.jumps.isEmpty else { return nil }
        let routes = await routeBook.candidates(for: profile)
        if routes.count < 2, routes.first?.lowercased() == profile.endpoint.host.lowercased() { return nil }
        let pinned = try await hostKeys.fingerprints(for: HostKeyTrust.hostID(profile.endpoint))
        return pinned.isEmpty ? nil : routes
    }

    func connectRacing(_ routes: [String], credentials: inout Credentials) async throws {
        let anchor = HostKeyTrust.hostID(profile.endpoint)
        let gate = RouteGate()
        let autofill = PasswordAutofill()
        let start = credentials
        let mismatched = MismatchedRoutes()
        let win: RouteRace.Win<RouteWin>
        do {
            win = try await RouteRace.run(
                routes,
                isFatal: Self.isFatalRouteError,
                isNetwork: Self.isNetworkRouteError,
                attempt: { index, host in
                    do {
                        return try await self.attemptRoute(
                            host, token: index, anchor: anchor, gate: gate, credentials: start, autofill: autofill
                        )
                    } catch SSHError.hostKeyChanged(let id, let expected, let presented) {
                        mismatched.add(host)
                        throw SSHError.hostKeyChanged(host: id, expected: expected, presented: presented)
                    }
                },
                discard: { win in try? await win.client.close() }
            )
        } catch SSHError.hostKeyChanged where gate.sawPinnedKey {
            // The machine answered with its own key somewhere; that route just failed later.
            throw SSHError.unreachable("The machine could not be reached. Check the address and your network")
        }
        await routeBook.recordMismatches(mismatched.hosts, for: profile.id)
        clients = [win.value.client]
        root = win.value.channel
        credentials = win.value.credentials
        remoteAddress = win.value.channel.remoteAddress?.ipAddress
        route = win.route
        await routeBook.recordUse(win.route, for: profile.id)
    }

    private func attemptRoute(
        _ host: String, token: Int, anchor: String, gate: RouteGate, credentials: Credentials, autofill: PasswordAutofill
    ) async throws -> RouteWin {
        var credentials = credentials
        var endpoint = profile.endpoint
        endpoint.host = host
        do {
            let (client, channel) = try await connectHop(
                endpoint, via: nil, credentials: &credentials, mayAskPassword: true, autofill: autofill,
                pinnedAs: anchor, gate: gate, token: token
            )
            return RouteWin(client: client, channel: channel, credentials: credentials)
        } catch {
            if Self.isNetworkRouteError(error) { gate.release(token) }
            throw error
        }
    }

    /// A refused login or a cancelled prompt ends the race: neither is about the route. A
    /// route that cannot be reached, or that shows a key other than the pinned one, only
    /// drops out of it.
    static func isFatalRouteError(_ error: any Error) -> Bool {
        if case SSHError.hostKeyChanged = error { return false }
        return !(error is RouteRace.Lost) && !isNetworkRouteError(error)
    }

    static func isNetworkRouteError(_ error: any Error) -> Bool {
        switch error {
        case SSHError.unreachable, SSHError.timeout, SSHError.channelClosed: true
        default: false
        }
    }
}

/// Routes that showed a key other than the pinned one during one race.
private final class MismatchedRoutes: Sendable {
    private let list = Mutex<[String]>([])

    func add(_ host: String) { list.withLock { $0.append(host) } }

    var hosts: [String] { list.withLock { $0 } }
}
