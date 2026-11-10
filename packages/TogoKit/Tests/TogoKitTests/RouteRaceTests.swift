import Foundation
import Synchronization
import Testing
@testable import TogoKit

private struct Unreachable: Error {}
private struct Mismatch: Error {}

/// Records which routes started and which won values were handed back.
private final class Log: Sendable {
    let started = Mutex<[String]>([])
    let discarded = Mutex<[String]>([])
}

struct RouteRaceTests {
    private static let stagger: Duration = .milliseconds(40)

    /// The stagger runs on `clock`, which only the test moves: a next route starts when the
    /// test says the stagger passed, or at once after a failure, never because a machine was slow.
    private func race(
        _ routes: [String], log: Log = Log(), clock: ManualClock = ManualClock(),
        _ outcome: @escaping @Sendable (String) async throws -> String
    ) async throws -> RouteRace.Win<String> {
        try await RouteRace.run(
            routes, stagger: Self.stagger, clock: clock,
            // As the connection races: a mismatch only drops its route.
            isFatal: { _ in false },
            isNetwork: { $0 is Unreachable },
            attempt: { _, route in
                log.started.withLock { $0.append(route) }
                return try await outcome(route)
            },
            discard: { value in log.discarded.withLock { $0.append(value) } }
        )
    }

    @Test func firstSuccessWinsEvenIfALaterRouteIsFaster() async throws {
        let log = Log()
        // The stagger never passes, so the faster route is never even started.
        let win = try await race(["lan", "tailscale"], log: log) { route in route }
        #expect(win.route == "lan")
        #expect(win.index == 0)
        #expect(log.started.withLock { $0 } == ["lan"])
    }

    @Test func slowRouteLosesToTheNextStaggeredOne() async throws {
        let log = Log()
        let clock = ManualClock()
        let racing = Task {
            try await race(["lan", "tailscale"], log: log, clock: clock) { route in
                // Silent until the race is over and cancels it.
                if route == "lan" { try await Task.sleep(for: .seconds(60)) }
                return route
            }
        }
        #expect(await clock.runSleep(Self.stagger))
        let win = try await racing.value
        #expect(win.route == "tailscale")
        #expect(log.started.withLock { $0 } == ["lan", "tailscale"])
    }

    @Test func failureStartsTheNextRouteAtOnce() async throws {
        let clock = ManualClock()
        // The clock never moves: only failures can start "b" and "c".
        let win = try await race(["a", "b", "c"], clock: clock) { route in
            if route != "c" { throw Unreachable() }
            return route
        }
        #expect(win.route == "c")
    }

    @Test func lateWinnerIsDiscarded() async throws {
        let log = Log()
        let clock = ManualClock()
        let gate = Gate()
        let racing = Task {
            try await race(["a", "b"], log: log, clock: clock) { route in
                // "a" answers only after "b" has won, and does not stop when cancelled.
                if route == "a" { await gate.passIfOpen() }
                return route
            }
        }
        #expect(await clock.runSleep(Self.stagger))
        let win = try await racing.value
        #expect(win.route == "b")
        gate.open()
        #expect(await eventually { log.discarded.withLock { $0 } == ["a"] })
    }

    @Test func mismatchedRoutePlusGoodRouteConnects() async throws {
        let log = Log()
        let win = try await race(["stale-lan", "tailscale"], log: log) { route in
            if route == "stale-lan" { throw Mismatch() }
            return route
        }
        #expect(win.route == "tailscale")
        #expect(log.started.withLock { $0 } == ["stale-lan", "tailscale"])
    }

    @Test func goodRouteStillWinsWhenTheMismatchComesLater() async throws {
        let log = Log()
        let clock = ManualClock()
        let gate = Gate()
        let racing = Task {
            try await race(["lan", "stale"], log: log, clock: clock) { route in
                if route == "stale" { throw Mismatch() }
                await gate.passIfOpen()
                return route
            }
        }
        #expect(await clock.runSleep(Self.stagger))
        #expect(await eventually { log.started.withLock { $0 } == ["lan", "stale"] })
        gate.open()
        let win = try await racing.value
        #expect(win.route == "lan")
    }

    @Test func allMismatchedGivesTheWarning() async throws {
        await #expect(throws: Mismatch.self) {
            try await race(["a", "b"]) { _ in throw Mismatch() }
        }
    }

    @Test func mismatchOutranksUnreachableWhenNothingConnects() async throws {
        await #expect(throws: Mismatch.self) {
            try await race(["gone", "stale"]) { route in
                if route == "gone" { throw Unreachable() }
                throw Mismatch()
            }
        }
    }

    @Test func mismatchedLANAddressesAreDroppedButTypedOnesStay() {
        let routes = [
            HostRoute(host: "192.168.1.20", kind: .lan), HostRoute.manual("192.168.1.30"),
            HostRoute(host: "studio.local", kind: .local), HostRoute(host: "100.101.102.103", kind: .tailscale),
        ]
        let kept = RoutePlan.droppingMismatched(routes, ["192.168.1.20", "192.168.1.30", "studio.local", "100.101.102.103"])
        #expect(kept.map(\.host) == ["192.168.1.30", "studio.local", "100.101.102.103"])
    }

    @Test func raceSkipsMismatchedRoutesAfterASuccess() async {
        let book = RouteBook()
        let profile = HostProfile(
            id: UUID(), name: "s", endpoint: HostEndpoint(host: "studio.local", user: "sam"), auth: .password,
            routes: [.manual("studio.local"), HostRoute(host: "192.168.1.20", kind: .lan)]
        )
        await book.recordMismatches(["192.168.1.20"], for: profile.id)
        #expect(await book.candidates(for: profile) == ["studio.local"])
        await book.clearMismatches(["192.168.1.20"], for: profile.id)
        #expect(await book.candidates(for: profile) == ["studio.local", "192.168.1.20"])
    }

    @Test func allFailingPrefersAMachineErrorOverUnreachable() async throws {
        struct Refused: Error {}
        await #expect(throws: Refused.self) {
            try await RouteRace.run(
                ["a", "b"], stagger: Self.stagger, isFatal: { _ in false }, isNetwork: { $0 is Unreachable },
                attempt: { index, _ -> String in
                    if index == 0 { throw Refused() }
                    throw Unreachable()
                },
                discard: { _ in }
            )
        }
        await #expect(throws: Unreachable.self) {
            try await race(["a", "b"]) { _ in throw Unreachable() }
        }
    }

    @Test func lostAttemptsDoNotCountAsErrors() async throws {
        let win = try await race(["a", "b"]) { route in
            if route == "a" { throw RouteRace.Lost() }
            return route
        }
        #expect(win.route == "b")
    }

    // MARK: Gate

    @Test func gateAdmitsOnlyThePinnedKeyAndOnlyOnce() {
        let gate = RouteGate()
        let pinned = ["SHA256:good"]
        #expect(gate.admit("SHA256:evil", pinned: pinned, token: 0) == .mismatch)
        #expect(gate.admit("SHA256:good", pinned: pinned, token: 1) == .accept)
        #expect(gate.admit("SHA256:good", pinned: pinned, token: 1) == .accept)
        #expect(gate.admit("SHA256:good", pinned: pinned, token: 2) == .lost)
        // A mismatch is reported even after a route has won.
        #expect(gate.admit("SHA256:evil", pinned: pinned, token: 3) == .mismatch)
    }

    @Test func gateRemembersThatThePinnedKeyWasSeen() {
        let gate = RouteGate()
        #expect(gate.admit("evil", pinned: ["good"], token: 0) == .mismatch)
        #expect(!gate.sawPinnedKey)
        #expect(gate.admit("good", pinned: ["good"], token: 1) == .accept)
        gate.release(1)
        #expect(gate.sawPinnedKey)
    }

    @Test func gateNeverAcceptsWithoutAPin() {
        #expect(RouteGate().admit("SHA256:any", pinned: [], token: 0) == .mismatch)
    }

    @Test func releasedGateLetsAnotherRouteWin() {
        let gate = RouteGate()
        #expect(gate.admit("k", pinned: ["k"], token: 0) == .accept)
        gate.release(1)
        #expect(gate.admit("k", pinned: ["k"], token: 1) == .lost)
        gate.release(0)
        #expect(gate.admit("k", pinned: ["k"], token: 1) == .accept)
    }

    @Test func connectionDropsAMismatchedRouteAndStopsOnARefusedLogin() {
        let changed = SSHError.hostKeyChanged(host: "[studio.local]:22", expected: ["a"], presented: "b")
        #expect(!SSHConnection.isFatalRouteError(changed))
        #expect(!SSHConnection.isNetworkRouteError(changed))
        #expect(SSHConnection.isFatalRouteError(SSHError.authFailed(tried: ["key"])))
        #expect(!SSHConnection.isFatalRouteError(SSHError.timeout))
        #expect(!SSHConnection.isFatalRouteError(SSHError.unreachable("no")))
        #expect(!SSHConnection.isFatalRouteError(RouteRace.Lost()))
    }
}
