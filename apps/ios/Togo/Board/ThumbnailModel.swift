import Foundation
import Observation
import TogoKit

/// The last lines of each session's screen for the board's cards. Only cards on screen fetch,
/// and only while the board is on screen in an active scene: every 3 s a card, at most 4 at a
/// time, with a card whose capture failed waiting 15 s. Nothing it fetches is logged.
@MainActor @Observable final class ThumbnailModel {
    static let lines = 12
    static let interval: TimeInterval = 3
    static let backoff: TimeInterval = 15
    static let maxInFlight = 4

    private var texts: [TabID: String] = [:]
    @ObservationIgnored private var visible: Set<TabID> = []
    @ObservationIgnored private var active = false
    @ObservationIgnored private var due: [TabID: Date] = [:]
    @ObservationIgnored private var tasks: [TabID: Running] = [:]
    @ObservationIgnored private var started = 0
    /// Cancelled captures that have not returned. They keep their slot: a request the machine is
    /// still answering counts, whether or not anyone wants the answer.
    @ObservationIgnored private var draining: Set<Int> = []
    @ObservationIgnored private let capture: @MainActor (TabID) async throws -> String
    @ObservationIgnored private let canCapture: @MainActor (TabID) -> Bool
    @ObservationIgnored private let now: @MainActor () -> Date

    /// `canCapture` says whether a session's machine can be asked now; cards of one that cannot
    /// show the agent's current action instead.
    init(
        capture: @escaping @MainActor (TabID) async throws -> String,
        canCapture: @escaping @MainActor (TabID) -> Bool,
        now: @escaping @MainActor () -> Date = { Date() }
    ) {
        self.capture = capture
        self.canCapture = canCapture
        self.now = now
    }

    /// Captures through the store's services, from machines that are online with `capture.v1`.
    convenience init(store: AppStore) {
        self.init(
            capture: { [weak store] id in
                guard let store else { throw CancellationError() }
                return try await store.services.capture(hostID: id.hostID, sessionID: id.sessionID, lines: Self.lines)
            },
            canCapture: { [weak store] id in
                guard let machine = store?.machine(for: id.hostID) else { return false }
                return machine.reachability == .online && machine.capabilities.contains(Capability.capture)
            }
        )
    }

    func text(for id: TabID) -> String? {
        texts[id]
    }

    func supportsCapture(_ id: TabID) -> Bool {
        canCapture(id)
    }

    /// Tabs whose capture is running.
    var inFlight: Set<TabID> {
        Set(tasks.keys)
    }

    /// The cards on screen. A card that left stops fetching at once.
    func setVisible(_ keys: Set<TabID>) {
        visible = keys
        for id in tasks.keys where !keys.contains(id) {
            cancel(id)
        }
    }

    /// The board is on screen in an active scene. While it is not, nothing runs.
    func setActive(_ isActive: Bool) {
        active = isActive
        guard !isActive else { return }
        for id in tasks.keys {
            cancel(id)
        }
    }

    /// Starts the captures that are due, oldest first, up to the limit in flight.
    func pump(at time: Date) {
        guard active else { return }
        let ready = visible
            .filter { tasks[$0] == nil && (due[$0] ?? .distantPast) <= time && canCapture($0) }
            .sorted { lhs, rhs in
                let (a, b) = (due[lhs] ?? .distantPast, due[rhs] ?? .distantPast)
                return a == b ? (lhs.hostID.uuidString, lhs.sessionID) < (rhs.hostID.uuidString, rhs.sessionID) : a < b
            }
        for id in ready.prefix(max(0, Self.maxInFlight - tasks.count - draining.count)) {
            start(id)
        }
    }

    /// Waits for the captures in flight to finish.
    func settle() async {
        while let running = tasks.values.first {
            await running.task.value
        }
    }

    #if DEBUG
    /// Sets a card's text directly, for previews and snapshots.
    func show(_ text: String, for id: TabID) {
        texts[id] = ThumbnailText.clean(text, lines: Self.lines)
    }
    #endif

    // MARK: Private

    private struct Running {
        let token: Int
        let task: Task<Void, Never>
    }

    private func start(_ id: TabID) {
        let capture = self.capture
        started += 1
        let token = started
        let task = Task { [weak self] in
            let result: Result<String, any Error>
            do {
                result = .success(try await capture(id))
            } catch {
                result = .failure(error)
            }
            self?.finish(id, token: token, result)
        }
        tasks[id] = Running(token: token, task: task)
    }

    private func finish(_ id: TabID, token: Int, _ result: Result<String, any Error>) {
        // A cancelled capture was already let go of; one started after it may hold the tab.
        if draining.remove(token) != nil { return }
        guard tasks[id]?.token == token else { return }
        tasks[id] = nil
        switch result {
        case .success(let raw):
            let text = ThumbnailText.clean(raw, lines: Self.lines)
            if texts[id] != text { texts[id] = text }
            due[id] = now().addingTimeInterval(Self.interval)
        case .failure:
            // The reason is not kept: an error from the machine can quote what was on screen.
            due[id] = now().addingTimeInterval(Self.backoff)
        }
    }

    private func cancel(_ id: TabID) {
        guard let running = tasks.removeValue(forKey: id) else { return }
        running.task.cancel()
        draining.insert(running.token)
    }
}
