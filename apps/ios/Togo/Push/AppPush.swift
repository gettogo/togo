import Foundation
import Network
import Synchronization
import TogoKit
import TogoPush
import UIKit
import UserNotifications
import WidgetKit

/// Notification permission, the APNs token, pairing, lock-screen answers and what a
/// notification opens, for the app process.
@MainActor final class AppPush {
    let registrar: PushRegistrar
    let pairing: PairingCoordinator
    let router = NotificationRouter()
    /// Nil where no Watch can pair, such as iPad.
    let watch = WatchBridge.live()
    let live: LiveActivityController
    private let pairings: PairingStore
    private let pending: PendingStore
    private let keys: any DeviceKeyStore
    private let source = SettingsSource()
    private var actions: NotificationActionHandler?
    private var clear: ClearHandler?
    private var sharedApproveWithoutUnlock: Bool?
    private let path = NWPathMonitor()
    private var watchFeed: Task<Void, Never>?
    private var pendingStart: (@MainActor () -> Void)?

    /// Reads settings from the services, which are built after this.
    private final class SettingsSource {
        weak var services: LiveServices?
    }

    /// Lets the Live Activity controller, built first, reach the coordinator built after it.
    @MainActor private final class PairingLink {
        weak var pairing: PairingCoordinator?
    }

    private init(directory: URL) {
        // Without an access group the keychain uses the only one entitled, the group shared
        // with the notification service.
        keys = KeychainDeviceKeys(accessGroup: nil)
        pairings = PairingStore(directory: directory)
        pending = PendingStore(directory: directory, clock: { Date() }) {
            WidgetCenter.shared.reloadTimelines(ofKind: WidgetKinds.waiting)
        }
        registrar = PushRegistrar()
        let source = self.source
        let watch = self.watch
        let unpairs = PendingUnpairStore(directory: directory)
        let link = PairingLink()
        let live = LiveActivityController(
            pairings: pairings, isRetired: { id in unpairs.all().contains { $0.id == id } }
        ) { _, _ in
            await link.pairing?.tokensChanged()
        }
        self.live = live
        pairing = PairingCoordinator(
            keys: keys, store: pairings, unpairs: unpairs, registrar: registrar,
            settings: { source.services?.settings ?? AppSettings() },
            liveTokens: { hostId in (hostId.flatMap(live.updateToken(hostId:)), live.startToken) },
            watch: { watch?.devices ?? [] },
            keyRemovals: PendingKeyRemovalStore(file: directory.appending(path: "pending-key-removals.json"))
        )
        let pairing = self.pairing
        link.pairing = pairing
        // Covers removal while the machine is out of reach too: the pairing is retired at once.
        pairing.onRetired = { hostId in Task { await live.machineRemoved(hostId: hostId) } }
        registrar.onTokenChange = { await pairing.tokensChanged() }
    }

    /// Where settings that never sync live: this device's app group, beside the copy the
    /// notification service reads.
    static func deviceBackend() -> any KeyValueBackend {
        DefaultsBackend(defaults: SharedSettings.defaults ?? .standard, namespace: "device.")
    }

    /// Nil when the process lacks the app group.
    /// Makes the objects only: nothing reaches the notification center, the Watch or the
    /// network until `start()`.
    static func make() -> AppPush? {
        guard let directory = SharedGroup.storeDirectory(SharedGroup.ios) else { return nil }
        return AppPush(directory: directory)
    }

    /// `activeStore` is the window last in front; taps, replies and the Watch's list follow it.
    /// `quietTools` reach a machine only over a channel that opens without asking the user
    /// anything; replies from the Watch use them.
    func connect(
        services: LiveServices, tools: any MachineTools, quietTools: any MachineTools,
        activeStore: @escaping @MainActor () -> AppStore?
    ) {
        source.services = services
        let notifications = SystemNotifications()
        let replies = ReplySender(tools: tools, label: { Self.label($0, in: activeStore()) })
        let actions = NotificationActionHandler(dependencies: .init(
            pairings: pairings, pending: pending,
            relay: RelayAnswerSender(keys: keys, pending: pending, clock: { Date() }),
            live: services, notifications: notifications,
            runAgain: { host, session in
                Task { await activeStore()?.runAgain(hostID: host, sessionID: session, tools: tools) }
            },
            reply: { reply, session in
                Task {
                    // Before any window has a store the words still go; only the notice is lost.
                    if let store = activeStore() {
                        await store.deliverReply(reply, session: session, via: replies)
                    } else {
                        _ = await replies.send(reply)
                    }
                }
            }
        ))
        self.actions = actions
        let live = self.live, pending = self.pending
        clear = ClearHandler(dependencies: .init(
            core: NotificationServiceCore(keys: keys, pairings: pairings, pending: pending, clock: { Date() }),
            pending: pending, notifications: notifications,
            cleared: { [watch] id in
                // A request answered anywhere takes its Live Activity with it.
                Task { await live.reconcile(pending.all(), cleared: [id]) }
                watch?.cleared(id)
            }
        ))
        AppDelegate.registrar = registrar
        AppDelegate.clear = clear
        router.deliver(to: activeStore)
        router.onAction = { response in
            let link = await actions.handle(
                actionIdentifier: response.actionIdentifier, userInfo: response.userInfo, target: response.link,
                text: response.userText
            )
            if let link { activeStore()?.open(link) }
        }
        // The system hands a notification tap that launched the app only to a delegate set
        // during launch, so this one line cannot wait for the first screen.
        UNUserNotificationCenter.current().delegate = router
        pendingStart = { [weak self, weak services] in
            guard let self, let services else { return }
            // Both talk to system daemons and are safe off the main thread, so they do not
            // take a frame from the first screen.
            Task.detached(priority: .utility) {
                NotificationCategories.register()
            }
            watchNetwork(services)
            Task { await live.start() }
        }
        // The Watch link starts at launch, not with the first screen: a reply from the Watch
        // can launch the app in the background, where no window ever appears.
        connectWatch(services: services, quietTools: quietTools, activeStore: activeStore)
        if watch != nil {
            Task.detached(priority: .utility) { WatchBridge.activate() }
        }
    }

    /// Registers the lock-screen actions, wakes the Watch session and starts following the
    /// network and Live Activities. Runs once the first screen is up; later calls do nothing.
    func start() {
        let work = pendingStart
        pendingStart = nil
        work?()
    }

    /// The Watch gets the pairings to answer with and the sessions to list; a Watch that
    /// reports new keys or a new token has every online machine paired again.
    private func connectWatch(
        services: LiveServices, quietTools: any MachineTools, activeStore: @escaping @MainActor () -> AppStore?
    ) {
        guard let watch else { return }
        // The Watch's words go only over this phone's own connection, and only when it opens
        // without asking; otherwise the Watch hears it needs the phone.
        let quiet = ReplySender(tools: quietTools, quiet: true, label: { Self.label($0, in: activeStore()) })
        let pairings = self.pairings, pending = self.pending
        // Checked against the window in front. With no window (the Watch launched the app in
        // the background) an answer to a request the push announced still goes; anything that
        // needs the sessions tells the Watch it needs the phone.
        watch.onReply = { message in
            guard let store = activeStore() else {
                return await WatchReplyGate.windowless(pairings: pairings, pending: pending, sender: quiet)(message)
            }
            return await WatchReplyGate.handler(store: store, pairings: pairings, pending: pending, sender: quiet)(message)
        }
        // Run again tapped on the Watch starts the session the same quiet way.
        watch.onRunAgain = { message in
            guard let store = activeStore() else { return .needsPhone }
            return await WatchRunAgainGate.handler(store: store, pairings: pairings, tools: quietTools)(message)
        }
        watch.onDevicesChanged = { [weak services] in services?.pairOnlineMachines() }
        watch.currentRequests = { [pending = self.pending] in pending.all() }
        pairing.onPairingsChanged = { [weak watch] in watch?.sendPairings($0) }
        watch.sendPairings(pairings.all())
        watchFeed = Task { [weak watch] in
            let feed = Observations { WatchFeed(activeStore()?.controlCenter ?? ControlCenterState()) }
            // A change of status or requests goes at once; a session's activity line alone at
            // most every few seconds, so a busy agent does not keep the Watch link awake.
            let gate = WatchFeedGate()
            var trailing: Task<Void, Never>?
            @MainActor func send(_ next: WatchFeed) {
                gate.didSend(next, at: .now)
                watch?.publish(sessions: next.sessions, requests: pending.all(), textRequests: next.textRequests)
            }
            for await next in feed {
                switch gate.offer(next, now: .now) {
                case .skip:
                    break
                case .send:
                    trailing?.cancel()
                    send(next)
                case .hold(let wait):
                    trailing?.cancel()
                    trailing = Task {
                        try? await Task.sleep(for: wait)
                        guard !Task.isCancelled, gate.offer(next, now: .now) == .send else { return }
                        send(next)
                    }
                }
            }
            trailing?.cancel()
        }
    }

    /// The name a reply goes under, from the window in front when there is one.
    private static func label(_ reply: Reply, in store: AppStore?) -> String {
        store?.replyLabel(reply) ?? ReplyCopy.theAgent
    }

    /// The app came forward: Live Activities for requests that are over end, and a machine
    /// with waiting requests and no activity gets one when Live Activities are on.
    func becameActive() {
        let requests = pending.all()
        let enabled = source.services?.settings.liveActivities ?? true
        let live = self.live
        Task { await live.appOpened(requests, startingActivities: enabled) }
        WidgetCenter.shared.reloadTimelines(ofKind: WidgetKinds.waiting)
    }

    /// A removed machine that was out of reach may be reachable once the network is back.
    private func watchNetwork(_ services: LiveServices) {
        let online = PathState()
        path.pathUpdateHandler = { [weak services] path in
            let returned = online.satisfied.withLock { was in
                defer { was = path.status == .satisfied }
                return !was && path.status == .satisfied
            }
            guard returned else { return }
            Task { @MainActor in services?.retryUnpairs() }
        }
        path.start(queue: DispatchQueue(label: "app.togo.push.path"))
    }

    /// The notification service runs in its own process and reads this from the app group.
    /// The lock-screen actions are registered again whenever it changes.
    func share(_ settings: AppSettings) {
        SharedSettings.writeDoneSilent(settings.doneSilent, to: SharedSettings.defaults)
        watch?.sendDoneSilent(settings.doneSilent)
        guard settings.approveWithoutUnlock != sharedApproveWithoutUnlock else { return }
        sharedApproveWithoutUnlock = settings.approveWithoutUnlock
        NotificationCategories.setApproveWithoutUnlock(settings.approveWithoutUnlock)
    }

    /// A machine reported after the app came forward; notifications and Live Activities for
    /// requests it no longer lists are removed.
    func reported(_ id: HostID, open: Set<String>) {
        let hostIds = Set(pairings.all().filter { $0.appHostID == id }.map(\.hostId))
        guard !hostIds.isEmpty, let clear else { return }
        Task { await clear.didEnterForeground(open: open, reported: hostIds) }
    }
}

private final class PathState: Sendable {
    let satisfied = Mutex(true)
}

extension NotificationResponse {
    /// The `togo` dictionary the action handler reads, rebuilt from the values kept.
    var userInfo: [AnyHashable: Any] {
        var info: [String: Any] = [:]
        if let requestId { info["requestId"] = requestId }
        if let hostId { info["hostId"] = hostId }
        if let sessionId { info["sessionId"] = sessionId }
        return [NotificationServiceCore.infoKey: info]
    }
}

extension LiveServices: LiveAnswering {
    func hasLiveChannel(_ pairing: Pairing) -> Bool {
        pairing.appHostID.map(isOnline) ?? false
    }

    func answer(_ pairing: Pairing, requestId: String, decision: Decision) async throws {
        guard let id = pairing.appHostID else { throw LiveServiceError.unknownMachine }
        try await answer(hostID: id, requestID: requestId, decision: decision)
    }
}
