import Foundation
import Synchronization

/// Tries a machine's routes the happy-eyeballs way: the first route starts at once, each
/// next one `stagger` later, or at once when every attempt so far has failed. The first
/// attempt to succeed wins and the rest are cancelled; a success that arrives after the
/// winner is handed to `discard`, so nothing it opened stays open.
///
/// An error `isFatal` calls fatal, such as a host key that does not match the pinned one,
/// ends the whole race at once with that error: it is a warning, never a reason to try the
/// next route.
public enum RouteRace {
    public static let stagger: Duration = .milliseconds(250)

    /// An attempt that stopped because another route already won.
    public struct Lost: Error, Equatable {
        public init() {}
    }

    public struct Win<T: Sendable>: Sendable {
        public var index: Int
        public var route: String
        public var value: T
    }

    public static func run<T: Sendable>(
        _ routes: [String],
        stagger: Duration = stagger,
        isFatal: @escaping @Sendable (any Error) -> Bool,
        isNetwork: @escaping @Sendable (any Error) -> Bool = { _ in false },
        attempt: @escaping @Sendable (_ index: Int, _ route: String) async throws -> T,
        discard: @escaping @Sendable (T) async -> Void
    ) async throws -> Win<T> {
        guard !routes.isEmpty else { throw CancellationError() }
        let race = Race<T>(routes: routes, isFatal: isFatal, isNetwork: isNetwork, attempt: attempt, discard: discard)
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                race.begin(continuation, stagger: stagger)
            }
        } onCancel: {
            race.finish(.failure(CancellationError()))
        }
    }
}

private final class Race<T: Sendable>: Sendable {
    private struct State {
        var continuation: CheckedContinuation<RouteRace.Win<T>, any Error>?
        var done = false
        var next = 0
        var inFlight = 0
        var errors: [any Error] = []
        var tasks: [Task<Void, Never>] = []
    }

    let routes: [String]
    let isFatal: @Sendable (any Error) -> Bool
    let isNetwork: @Sendable (any Error) -> Bool
    let attempt: @Sendable (Int, String) async throws -> T
    let discard: @Sendable (T) async -> Void
    private let state = Mutex(State())

    init(
        routes: [String], isFatal: @escaping @Sendable (any Error) -> Bool, isNetwork: @escaping @Sendable (any Error) -> Bool,
        attempt: @escaping @Sendable (Int, String) async throws -> T, discard: @escaping @Sendable (T) async -> Void
    ) {
        self.routes = routes
        self.isFatal = isFatal
        self.isNetwork = isNetwork
        self.attempt = attempt
        self.discard = discard
    }

    func begin(_ continuation: CheckedContinuation<RouteRace.Win<T>, any Error>, stagger: Duration) {
        let cancelledEarly = state.withLock { state -> Bool in
            if state.done { return true }
            state.continuation = continuation
            return false
        }
        if cancelledEarly {
            continuation.resume(throwing: CancellationError())
            return
        }
        launchNext()
        let pacer = Task { [weak self] in
            while true {
                try? await Task.sleep(for: stagger)
                guard let self, !Task.isCancelled, self.launchNext() else { return }
            }
        }
        state.withLock { $0.tasks.append(pacer) }
    }

    /// Starts the next route; false when none is left or the race is over.
    @discardableResult
    private func launchNext() -> Bool {
        state.withLock { state -> Bool in
            guard !state.done, state.next < routes.count else { return false }
            let index = state.next
            state.next += 1
            state.inFlight += 1
            let route = routes[index]
            state.tasks.append(Task { await self.run(index, route) })
            return true
        }
    }

    private func run(_ index: Int, _ route: String) async {
        do {
            let value = try await attempt(index, route)
            if !finish(.success(RouteRace.Win(index: index, route: route, value: value))) {
                await discard(value)
            }
        } catch {
            failed(error)
        }
    }

    private func failed(_ error: any Error) {
        if isFatal(error) {
            finish(.failure(error))
            return
        }
        let (startNext, exhausted) = state.withLock { state -> (Bool, Bool) in
            state.inFlight -= 1
            if !(error is RouteRace.Lost) { state.errors.append(error) }
            let idle = state.inFlight == 0
            return (idle && state.next < routes.count, idle && state.next >= routes.count)
        }
        if startNext {
            launchNext()
        } else if exhausted {
            finish(.failure(chosenError()))
        }
    }

    /// When every route failed: an error about the machine itself, such as a refused login,
    /// says more than "could not reach" from routes that are simply not on this network.
    private func chosenError() -> any Error {
        let errors = state.withLock { $0.errors }
        return errors.first { !isNetwork($0) } ?? errors.last ?? RouteRace.Lost()
    }

    /// Ends the race once; returns false when it was already over.
    @discardableResult
    func finish(_ result: Result<RouteRace.Win<T>, any Error>) -> Bool {
        let taken = state.withLock { state -> (CheckedContinuation<RouteRace.Win<T>, any Error>?, [Task<Void, Never>])? in
            guard !state.done else { return nil }
            state.done = true
            defer { state.continuation = nil; state.tasks = [] }
            return (state.continuation, state.tasks)
        }
        guard let (continuation, tasks) = taken else { return false }
        for task in tasks { task.cancel() }
        switch result {
        case .success(let win): continuation?.resume(returning: win)
        case .failure(let error): continuation?.resume(throwing: error)
        }
        return true
    }
}
