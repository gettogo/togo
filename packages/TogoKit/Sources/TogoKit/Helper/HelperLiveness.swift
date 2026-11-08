import Foundation

/// Tells a helper connection that stopped carrying traffic from one that is only quiet.
///
/// A Mac that sleeps, a NAT entry that expires or Wi-Fi that stays associated but passes
/// nothing leave TCP open for minutes, so nothing ever reports the connection closed. A ping
/// is sent every `interval`; a ping that times out is followed at once by another, and
/// `misses` timed-out pings in a row mean the connection is dead.
public enum HelperLiveness {
    public static let interval: Duration = .seconds(20)
    public static let misses = 2

    public enum Outcome: Sendable, Equatable {
        /// Pings timed out `misses` times in a row: open, but carrying nothing.
        case dead
        /// The connection closed under the pings; whoever closed it knows.
        case closed
        /// The helper cannot be pinged (an old helper without `events.v1`); nothing is known.
        case unsupported
    }

    /// Runs until the connection is found dead, cannot be pinged, or the task is cancelled
    /// (nil). Only a timeout or a close counts against the connection: an error the helper
    /// sent back proves it is alive.
    public static func watch(
        clock: any LinkClock, interval: Duration = interval, misses: Int = misses,
        ping: @Sendable () async throws -> Void
    ) async -> Outcome? {
        var missed = 0
        while !Task.isCancelled {
            if missed == 0 {
                do { try await clock.sleep(for: interval) } catch { return nil }
            }
            switch await check(ping) {
            case .alive: missed = 0
            case .closed: return Task.isCancelled ? nil : .closed
            case .unsupported: return .unsupported
            case .timedOut:
                missed += 1
                if missed >= misses { return Task.isCancelled ? nil : .dead }
            }
        }
        return nil
    }

    /// After a call timed out: one more ping says whether the connection is dead (true) or
    /// only slow to answer that call (false).
    public static func isDead(ping: @Sendable () async throws -> Void) async -> Bool {
        switch await check(ping) {
        case .closed, .timedOut: true
        case .alive, .unsupported: false
        }
    }

    enum Ping: Equatable { case alive, timedOut, closed, unsupported }

    static func check(_ ping: @Sendable () async throws -> Void) async -> Ping {
        do {
            try await ping()
            return .alive
        } catch HelperError.closed {
            return .closed
        } catch HelperError.timeout {
            return .timedOut
        } catch HelperError.unsupported, HelperError.notReady {
            return .unsupported
        } catch is SSHError {
            return .closed
        } catch {
            return .alive
        }
    }
}
