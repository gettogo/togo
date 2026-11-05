import ActivityKit
import CryptoKit
import Foundation
import os
import TogoPush

/// One running Live Activity, as the controller sees it.
@MainActor protocol LiveActivityHandle: AnyObject, Sendable {
    var id: String { get }
    var hostId: String { get }
    /// The content shown now; nil when the activity is gone.
    var state: TogoActivityAttributes.ContentState? { get }
    /// Each token the system issues for updating this activity through push.
    var pushTokens: AsyncStream<Data> { get }
    func end() async
}

/// The part of ActivityKit the controller uses, so tests can stand in for it.
@MainActor protocol LiveActivitySystem: AnyObject {
    /// Push-to-start tokens, which let the helper start an activity while the app is closed.
    var startTokens: AsyncStream<Data> { get }
    /// Activities as they start, including those started by push.
    var created: AsyncStream<any LiveActivityHandle> { get }
    /// Activities that are active or stale.
    var running: [any LiveActivityHandle] { get }
    func request(
        hostId: String, state: TogoActivityAttributes.ContentState, staleDate: Date?
    ) throws -> any LiveActivityHandle
}

/// Collects the Live Activity tokens the helper needs and hands them to pairing, starts an
/// activity from the app when requests wait and none is live for their machine, and ends
/// activities whose request is over or whose machine was removed. Tokens are never logged; only a 6-character
/// hash of the start token is.
@MainActor final class LiveActivityController {
    private let pairings: PairingStore
    private let system: any LiveActivitySystem
    private let onTokens: (String?, String?) async -> Void
    private let clock: () -> Date
    private let isRetired: (String) -> Bool
    private var watched: Set<String> = []
    private var updateTokens: [String: String] = [:]
    private var started = false
    private static let log = Logger(subsystem: "app.togo", category: "live-activity")

    private(set) var startToken: String?

    /// The token that updates the machine's activity. Each machine has its own activity, so
    /// each gets its own token.
    func updateToken(hostId: String) -> String? { updateTokens[hostId] }

    /// `onTokens(update, start)` runs each time either token changes, with lowercase hex; the
    /// update token is the one of the activity that changed. `isRetired` tells machines that
    /// were removed but whose helper may still push.
    init(
        pairings: PairingStore, system: (any LiveActivitySystem)? = nil, clock: @escaping () -> Date = { Date() },
        isRetired: @escaping (String) -> Bool = { _ in false },
        onTokens: @escaping (String?, String?) async -> Void
    ) {
        self.pairings = pairings
        self.system = system ?? ActivityKitSystem()
        self.clock = clock
        self.isRetired = isRetired
        self.onTokens = onTokens
    }

    /// Follows push-to-start tokens, new activities and each activity's update tokens. Returns
    /// only when the system's streams end, so run it in its own task.
    func start() async {
        guard !started else { return }
        started = true
        await withDiscardingTaskGroup { group in
            for activity in system.running {
                group.addTask { await self.watch(activity) }
            }
            let startTokens = system.startTokens
            group.addTask {
                for await token in startTokens {
                    await self.received(startToken: token)
                }
            }
            for await activity in system.created {
                group.addTask { await self.watch(activity) }
            }
        }
    }

    /// Starts one activity per machine that has open requests and no live activity, so its
    /// update token reaches the helper over SSH on the next pairing. Uses push updates only.
    /// The activity goes stale when the oldest request's answer window closes.
    func startForPending(_ requests: [PendingRequest]) {
        let open = WaitingSummary.open(requests, now: clock())
        let live = Set(system.running.map(\.hostId))
        var seen: Set<String> = []
        for request in open where !live.contains(request.hostId) && seen.insert(request.hostId).inserted {
            let waiting = open.filter { $0.hostId == request.hostId }
            do {
                _ = try system.request(
                    hostId: request.hostId, state: state(oldest: request, waiting: waiting.count),
                    staleDate: request.receivedAt.addingTimeInterval(WaitingSummary.openFor)
                )
            } catch {
                Self.log.error("Live Activity not started: \(error.localizedDescription, privacy: .public)")
            }
        }
    }

    /// The app came forward with `requests` in the shared store: activities for requests that
    /// are over end first, then machines still waiting get one if `startingActivities`.
    func appOpened(_ requests: [PendingRequest], startingActivities: Bool) async {
        await reconcile(requests)
        if startingActivities { startForPending(requests) }
    }

    /// Ends activities that ask for a request no longer open: one in `cleared`, one the store
    /// holds as answered or past its window, or one it never saw that began longer ago than the
    /// window. A machine that still has open requests keeps its activity for the helper to
    /// update. Activities of removed machines end too.
    func reconcile(_ requests: [PendingRequest], cleared: Set<String> = []) async {
        let now = clock()
        let openHosts = Set(WaitingSummary.open(requests, now: now).map(\.hostId))
        let known = Dictionary(requests.map { ($0.requestId, $0) }, uniquingKeysWith: { first, _ in first })
        for activity in system.running {
            if isRetired(activity.hostId) {
                await activity.end()
                continue
            }
            guard let state = activity.state, LiveStatus(state.status) == .needsYou,
                  !openHosts.contains(activity.hostId)
            else { continue }
            let started = Date(timeIntervalSince1970: TimeInterval(state.since))
            let over = state.requestId.map { cleared.contains($0) || known[$0] != nil } == true
                || now.timeIntervalSince(started) >= WaitingSummary.openFor
            if over { await activity.end() }
        }
    }

    /// Ends the machine's activity at once and forgets its token.
    func machineRemoved(hostId: String) async {
        for activity in system.running where activity.hostId == hostId {
            await activity.end()
        }
        updateTokens[hostId] = nil
    }

    private func state(oldest: PendingRequest, waiting: Int) -> TogoActivityAttributes.ContentState {
        let optedIn = pairings.pairing(hostId: oldest.hostId)?.liveActivityCommand == true
        let command = optedIn && oldest.subject?.kind == "command" ? oldest.subject?.text : nil
        return TogoActivityAttributes.ContentState(
            bib: oldest.bib, agent: oldest.agent ?? "", status: "needs_you", waiting: waiting,
            since: Int(oldest.receivedAt.timeIntervalSince1970), requestId: oldest.requestId,
            command: command.map { PromptText.clean($0, maxLines: 2) }
        )
    }

    private func watch(_ activity: any LiveActivityHandle) async {
        guard watched.insert(activity.id).inserted else { return }
        // A removed machine's helper pushes until it is told to stop; what it starts goes.
        if isRetired(activity.hostId) {
            await activity.end()
            return
        }
        for await token in activity.pushTokens {
            await received(updateToken: token, hostId: activity.hostId)
        }
    }

    private func received(startToken data: Data) async {
        let hex = Self.hex(data)
        guard hex != startToken else { return }
        startToken = hex
        Self.log.info("Live Activity start token \(Self.hash(hex), privacy: .public)")
        await onTokens(nil, startToken)
    }

    private func received(updateToken data: Data, hostId: String) async {
        let hex = Self.hex(data)
        guard updateTokens[hostId] != hex else { return }
        updateTokens[hostId] = hex
        await onTokens(hex, startToken)
    }

    static func hex(_ data: Data) -> String {
        data.map { String(format: "%02x", $0) }.joined()
    }

    /// The first 6 hex characters of SHA-256: enough to tell tokens apart in a log.
    static func hash(_ token: String) -> String {
        String(hex(Data(SHA256.hash(data: Data(token.utf8)))).prefix(6))
    }
}

/// ActivityKit itself. Activities the app starts take push updates, so the helper can keep
/// them current while the app is closed.
@MainActor final class ActivityKitSystem: LiveActivitySystem {
    var startTokens: AsyncStream<Data> {
        AsyncStream { continuation in
            let task = Task {
                for await token in Activity<TogoActivityAttributes>.pushToStartTokenUpdates {
                    continuation.yield(token)
                }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    var created: AsyncStream<any LiveActivityHandle> {
        AsyncStream { continuation in
            let task = Task { @MainActor in
                for await activity in Activity<TogoActivityAttributes>.activityUpdates {
                    continuation.yield(SystemActivity(activity))
                }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    var running: [any LiveActivityHandle] {
        Activity<TogoActivityAttributes>.activities
            .filter { $0.activityState == .active || $0.activityState == .stale }
            .map(SystemActivity.init)
    }

    func request(
        hostId: String, state: TogoActivityAttributes.ContentState, staleDate: Date?
    ) throws -> any LiveActivityHandle {
        let activity = try Activity.request(
            attributes: TogoActivityAttributes(hostId: hostId),
            content: ActivityContent(state: state, staleDate: staleDate),
            pushType: .token
        )
        return SystemActivity(activity)
    }
}

/// Holds only the activity's id and looks the activity up where it is used: `Activity` is not
/// `Sendable`, so it never crosses into the token task.
@MainActor private final class SystemActivity: LiveActivityHandle {
    let id: String
    let hostId: String

    init(_ activity: Activity<TogoActivityAttributes>) {
        id = activity.id
        hostId = activity.attributes.hostId
    }

    var pushTokens: AsyncStream<Data> {
        let id = id
        return AsyncStream { continuation in
            let task = Task.detached { await Self.forward(tokensOf: id, to: continuation) }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    var state: TogoActivityAttributes.ContentState? { Self.state(id) }

    func end() async {
        await Self.end(id: id)
    }

    private nonisolated static func state(_ id: String) -> TogoActivityAttributes.ContentState? {
        activity(id)?.content.state
    }

    private nonisolated static func activity(_ id: String) -> Activity<TogoActivityAttributes>? {
        Activity<TogoActivityAttributes>.activities.first { $0.id == id }
    }

    private nonisolated static func forward(tokensOf id: String, to continuation: AsyncStream<Data>.Continuation) async {
        if let activity = activity(id) {
            for await token in activity.pushTokenUpdates {
                continuation.yield(token)
            }
        }
        continuation.finish()
    }

    private nonisolated static func end(id: String) async {
        await activity(id)?.end(nil, dismissalPolicy: .immediate)
    }
}
