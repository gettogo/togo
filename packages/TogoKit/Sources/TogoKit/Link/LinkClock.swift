import Foundation

/// Time as the link sees it, so reconnect backoff and coalescing can run on a test clock.
public protocol LinkClock: Sendable {
    func sleep(for duration: Duration) async throws
    var now: ContinuousClock.Instant { get }
}

public struct SystemClock: LinkClock {
    public init() {}

    public func sleep(for duration: Duration) async throws {
        try await Task.sleep(for: duration)
    }

    public var now: ContinuousClock.Instant { .now }
}
