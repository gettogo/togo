import Foundation
import Observation
import TogoKit
import TogoTerminal
import UIKit

/// The app's services against real machines: the synced host and settings stores, one monitor
/// per machine, and helper and terminal connections made on demand.
@MainActor @Observable final class LiveServices: AppServices {
    struct Dependencies {
        var watch: (HostRecord) -> any HostWatch
        var helpers: any HelperCalling
        /// How a terminal attaches to a session with the given transport.
        var connect: (HostRecord, String, TransportPreference, any ConnectionPrompter) -> LinkConnect
        /// Connects once with the user's answers and hands the connection to the next monitor attempt.
        var signIn: (HostRecord, any ConnectionPrompter) async throws -> Void = { _, _ in }
        /// Removes a machine's saved password from this device.
        var forgetPassword: (HostID) -> Void = { _ in }
        var now: @Sendable () -> Date = { Date() }
        /// Notification permission and pairing with each machine's helper; nil leaves push out.
        var registrar: PushRegistrar?
        var pairing: PairingCoordinator?
        /// Copies the settings the notification service needs where it can read them.
        var shareSettings: (AppSettings) -> Void = { _ in }
        /// A machine reported after the app came to the foreground: the requests it still has
        /// open, so notifications for the others can go.
        var reported: (HostID, Set<String>) -> Void = { _, _ in }
        var diagnostics: DiagnosticsRecorder = .shared
        /// Whether wake targets are learned from what machines report; off while the app
        /// cannot send wake packets.
        var wakeEnabled = WakeFeature.isEnabled
    }

    private(set) var hosts: [HostRecordView] = []
    var pairedMachines: Set<HostID> = []
    var confirmedCommands: Set<HostID> = []
    var pendingCommands: Set<HostID> = []
    /// Removed machines whose helper has not yet been told to stop pushing.
    var pendingRemovals: [PendingRemoval] = []
    /// Questions from terminals the user opens; monitors never ask.
    let attachPrompts = AttachPrompts()
    /// A machine was removed, here or on another device. Its screens and terminals go with it.
    @ObservationIgnored var onMachineRemoved: (HostID) -> Void = { _ in }
    /// A machine's address or credentials changed. Its terminals reconnect with the new ones.
    @ObservationIgnored var onMachineChanged: (HostID) -> Void = { _ in }
    /// A machine a terminal waited for is loaded. Its tabs attach again, now with its record.
    @ObservationIgnored var onMachineArrived: (HostID) -> Void = { _ in }
    /// True once `start()` has read the stored machines; until then a missing one is not unknown.
    @ObservationIgnored var recordsLoaded = false
    @ObservationIgnored var pendingTerminals: [HostID: [UUID: AsyncStream<LinkPhase>.Continuation]] = [:]
    /// Every machine view as it is published, for readers that serve the whole app.
    @ObservationIgnored var onPublish: (HostViewSnapshot) -> Void = { _ in }
    /// Runs when the app goes to the background, before the helper connections close: port
    /// previews over them stop first so they can reopen on return.
    @ObservationIgnored var beforeDroppingHelpers: @MainActor () async -> Void = {}
    var settings = AppSettings() {
        didSet { settingsChanged(from: oldValue) }
    }

    @ObservationIgnored let hostStore: HostStore
    @ObservationIgnored private let hostRecords: SyncedRecordStore<HostRecord>
    @ObservationIgnored private let settingsStore: SettingsStore
    @ObservationIgnored let deps: Dependencies
    @ObservationIgnored private(set) var records: [HostID: HostRecord] = [:]
    @ObservationIgnored private(set) var watches: [HostID: any HostWatch] = [:]
    @ObservationIgnored private var feeds: [HostID: Task<Void, Never>] = [:]
    @ObservationIgnored private(set) var latest: [HostID: HostSnapshot] = [:]
    @ObservationIgnored private(set) var mapper = SnapshotMapper()
    @ObservationIgnored private var agentsRequested: Set<HostID> = []
    @ObservationIgnored private var terminals: [ObjectIdentifier: LiveTerminal] = [:]
    @ObservationIgnored private(set) var foreground = true
    /// Machines that have not reported since the app last came to the foreground.
    @ObservationIgnored private var unreported: Set<HostID> = []
    /// Set while a value from the store is applied, so it is not written straight back.
    @ObservationIgnored private var applyingStoredSettings = false
    @ObservationIgnored private var started = false
    @ObservationIgnored private var lastSave: Task<Void, Never>?
    @ObservationIgnored private var signIns: [HostID: Task<Void, any Error>] = [:]
    /// False until the stored machines are read, so a launch with machines never flashes first run.
    private(set) var hostsLoaded = false
    @ObservationIgnored private var listeners: [Task<Void, Never>] = []
    @ObservationIgnored private let hub = SnapshotHub()

    /// Both record stores must be made with `syncSwitch`'s value, so nothing merges from
    /// iCloud before the device's own choice is known. `device` keeps the settings that never sync.
    init(
        hostRecords: SyncedRecordStore<HostRecord>, settingsRecords: SyncedRecordStore<AppSettings>,
        syncSwitch: SyncSwitch, device: DeviceSettings = DeviceSettings(backend: MemoryBackend()),
        dependencies: Dependencies
    ) {
        self.hostRecords = hostRecords
        hostStore = HostStore(records: hostRecords)
        settingsStore = SettingsStore(records: settingsRecords, syncSwitch: syncSwitch, device: device)
        deps = dependencies
    }

    /// Lists the machines saved on this device straight away, so the first frame shows them.
    /// With none saved it waits for `start()`, which also merges iCloud, so a new device with
    /// machines elsewhere never flashes first run. Monitors start in `start()`.
    func showLocalHosts() {
        guard !hostsLoaded else { return }
        let local = hostStore.localHosts()
        guard !local.isEmpty else { return }
        hosts = local.map(Self.view(of:))
        hostsLoaded = true
    }

    /// Loads the stores, starts a monitor per machine and follows changes from other devices.
    func start() async {
        guard !started else { return }
        started = true
        applyStored(await settingsStore.load())
        deps.shareSettings(settings)
        await hostRecords.setSyncEnabled(settings.syncEnabled)
        reconcile(await hostStore.hosts())
        // Addresses saved with an interface zone (`fe80::…%en0`) become the machine's name when
        // it answers to one; the others keep theirs and the edit form explains. Off the launch
        // path: a lookup can take seconds, and the store's changes reconcile when it is done.
        let hostStore = self.hostStore
        Task { _ = await hostStore.tidyAddresses() }
        hostsLoaded = true
        retryUnpairs()
        if let pairing = deps.pairing {
            pushChanged()
            pairing.onChange = { [weak self] in self?.pushChanged() }
            pairing.onUnpaired = { [weak self] entry in self?.unpaired(entry) }
            pairing.onRepairNeeded = { [weak self] in self?.pairOnlineMachines() }
        }
        let hostChanges = hostStore.changes
        let settingsChanges = settingsStore.changes
        listeners = [
            Task { [weak self] in
                for await all in hostChanges { self?.reconcile(all) }
            },
            Task { [weak self] in
                for await stored in settingsChanges { self?.applyStored(stored) }
            },
        ]
    }

    /// The app moved to or from the foreground. Monitors subscribe again with the new flag and
    /// terminals reconnect or let the system suspend them.
    func setForeground(_ on: Bool) {
        guard on != foreground else { return }
        foreground = on
        attachPrompts.isForeground = on
        deps.pairing?.isForeground = on
        if on {
            unreported = Set(records.keys)
            retryUnpairs()
        }
        for watch in watches.values {
            Task { await watch.setForeground(on) }
        }
        for terminal in terminals.values {
            terminal.setForeground(on)
        }
        if !on {
            let helpers = deps.helpers
            let before = beforeDroppingHelpers
            Task {
                await before()
                await helpers.dropAll()
            }
        }
    }

    // MARK: AppServices

    func snapshots() -> AsyncStream<HostViewSnapshot> {
        hub.stream()
    }

    func refresh() async {
        await withTaskGroup(of: Void.self) { group in
            for watch in watches.values {
                group.addTask { await watch.refresh() }
            }
        }
    }

    func answer(hostID: HostID, requestID: String, decision: Decision) async throws {
        _ = try await perform(.answer(requestID, decision), on: hostID)
    }

    func kill(hostID: HostID, sessionID: String) async throws {
        _ = try await perform(.kill(sessionID), on: hostID)
    }

    func rename(hostID: HostID, sessionID: String, name: String) async throws {
        _ = try await perform(.rename(sessionID, name), on: hostID)
    }

    func setMute(hostID: HostID, sessionID: String, muted: Bool) async throws {
        _ = try await perform(.setMute(sessionID, muted), on: hostID)
    }

    func create(hostID: HostID, params: CreateSessionParams) async throws -> SessionRow {
        guard let session = try await perform(.create(params), on: hostID) else { throw HelperError.malformedResponse }
        let names = mapper.agentNames[hostID] ?? [:]
        return SessionRow(
            id: session.id, hostID: hostID, bib: session.bib, name: session.name, agent: session.agent,
            agentName: SnapshotMapper.agentName(session.agent, names: names), status: session.status,
            activity: session.activity, cwd: session.cwd, muted: session.muted, managed: session.managed,
            meta: session.meta, statusSince: deps.now(), mission: session.mission
        )
    }

    /// A machine without the helper can only start a shell in tmux.
    func agents(hostID: HostID) async throws -> [AgentInfo] {
        let record = try record(hostID)
        guard !record.noHelper else {
            return [AgentInfo(id: "shell", name: SnapshotMapper.agentName("shell", names: [:]), installed: true)]
        }
        return try await fetchAgents(record, prompter: userPrompter(record))
    }

    private func fetchAgents(_ record: HostRecord, prompter: AttachPrompter?) async throws -> [AgentInfo] {
        let agents = try await call(record, prompter: prompter) { try await $0.agents() }
        learnAgentNames(agents, for: record.id)
        return agents
    }

    /// A prompter for a helper request the user just made. Like a terminal the user opens, it
    /// may ask only in the foreground and only during this one request.
    func userPrompter(_ record: HostRecord) -> AttachPrompter {
        AttachPrompter(
            prompts: attachPrompts, hostID: record.id, machine: record.profile.name,
            address: record.profile.endpoint.host, userStarted: foreground
        )
    }

    /// Questions from `prompter` leave when the request ends, however it ends.
    private func call<T: Sendable>(
        _ record: HostRecord, prompter: AttachPrompter?, _ body: @escaping @Sendable (HelperClient) async throws -> T
    ) async throws -> T {
        let attempt = prompter?.beginAttempt()
        do {
            let result = try await deps.helpers.call(record, prompter: prompter, body)
            if let prompter, let attempt { await prompter.endAttempt(attempt) }
            return result
        } catch {
            if let prompter, let attempt { await prompter.endAttempt(attempt) }
            throw error
        }
    }

    func recentDirs(hostID: HostID) async throws -> [String] {
        let record = try record(hostID)
        guard !record.noHelper else { return [] }
        return try await call(record, prompter: userPrompter(record)) { try await $0.recentDirs() }
    }

    func forgetRecentDir(hostID: HostID, path: String) async throws {
        try await call(try helperRecord(hostID), prompter: nil) { try await $0.forgetRecentDir(path) }
    }

    func listDir(hostID: HostID, path: String) async throws -> ListDirResult {
        let record = try helperRecord(hostID)
        return try await call(record, prompter: userPrompter(record)) { try await $0.listDir(path: path, showHidden: false) }
    }

    func terminal(hostID: HostID, sessionID: String, transport: TransportPreference?, userStarted: Bool) -> TerminalHandle {
        guard let record = records[hostID] else { return awaitingMachine(hostID) }
        let preference = transport ?? (record.profile.transport == .auto ? settings.transport : record.profile.transport)
        let prompter = AttachPrompter(
            prompts: attachPrompts, hostID: hostID, machine: record.profile.name,
            address: record.profile.endpoint.host, userStarted: userStarted && foreground
        )
        let attach = deps.connect(record, sessionID, preference, prompter)
        // Every attempt gets its own id; its questions leave when it ends, however it ends.
        let connect: LinkConnect = { cols, rows, onWaking in
            let attempt = prompter.beginAttempt()
            do {
                let attachment = try await attach(cols, rows, onWaking)
                await prompter.endAttempt(attempt)
                return attachment
            } catch {
                await prompter.endAttempt(attempt)
                throw error
            }
        }
        let prompts = attachPrompts
        let terminal = LiveTerminal(
            connect: deps.diagnostics.observing(connect, host: hostID, preference: preference),
            settings: settings, wasAsked: { prompter.askedLastAttempt }, diagnostics: deps.diagnostics,
            onState: deps.diagnostics.linkStates(host: hostID, preference: preference)
        ) { [weak self] id in
            prompts.withdraw(owner: ObjectIdentifier(prompter))
            self?.terminals[id] = nil
        }
        terminals[ObjectIdentifier(terminal)] = terminal
        if !foreground { terminal.setForeground(false) }
        return terminal.handle
    }

    func capture(hostID: HostID, sessionID: String, lines: Int) async throws -> String {
        let record = try helperRecord(hostID)
        return try await call(record, prompter: userPrompter(record)) {
            try await $0.capture(id: sessionID, lines: lines, join: true).text
        }
    }

    func upload(hostID: HostID, sessionID: String, name: String, data: Data) async throws -> String {
        let record = try helperRecord(hostID)
        let params = UploadParams(sessionId: sessionID, name: name, data: data)
        return try await call(record, prompter: userPrompter(record)) { try await $0.upload(params).path }
    }

    /// A second window signing in to the same machine waits on the first instead of opening a
    /// second connection and asking a second time.
    func signIn(hostID: HostID, prompter: any ConnectionPrompter) async throws {
        if let running = signIns[hostID] {
            return try await running.value
        }
        let record = try record(hostID)
        let signIn = deps.signIn
        let task = Task { [weak self] in
            try await signIn(record, prompter)
            await self?.watches[hostID]?.refresh()
        }
        signIns[hostID] = task
        defer { signIns[hostID] = nil }
        try await task.value
    }

    func saveHost(_ record: HostRecord) async throws {
        await hostStore.save(record)
        reconcile(await hostStore.hosts())
    }

    func removeHost(_ id: HostID) async throws {
        await hostStore.remove(id)
        reconcile(await hostStore.hosts())
    }

    /// The stored record behind a machine, for editing it.
    func hostRecord(_ id: HostID) -> HostRecord? {
        records[id]
    }

    // MARK: Machines

    /// Brings monitors in line with the stored machines: new ones start, removed ones stop,
    /// and an edited one starts over with its new address or credentials.
    private func reconcile(_ all: [HostRecord]) {
        let incoming = Dictionary(all.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        let removed = records.keys.filter { incoming[$0] == nil }
        // A new wake target or route is no reason to reconnect.
        let changed = records.keys.filter { incoming[$0] != nil && incoming[$0]?.withoutLearned != records[$0]?.withoutLearned }
        let rewoken = records.keys.filter { incoming[$0] != nil && incoming[$0]?.wake != records[$0]?.wake && !changed.contains($0) }
        // A removed machine's channel and saved password stay until its helper is told to stop pushing.
        let paired = deps.pairing?.pairedOrPairing ?? []
        let unpairing = Set(removed.filter { id in records[id].map { unpair($0, reachable: latest[id]?.reach == .online) } ?? false })
        for id in removed + changed {
            stopWatching(id, dropChannel: !unpairing.contains(id))
        }
        records = incoming
        shareRoutes(all, removed: removed)
        hosts = all.map(Self.view(of:))
        for record in all where watches[record.id] == nil {
            startWatching(record)
        }
        for id in removed {
            mapper.forget(id)
            attachPrompts.withdraw(hostID: id)
            if !paired.contains(id) { deps.forgetPassword(id) }
            onMachineRemoved(id)
        }
        changed.forEach(onMachineChanged)
        for id in rewoken {
            if let snapshot = latest[id], let record = records[id] { publish(snapshot, record: record) }
        }
        settlePendingTerminals()
    }

    private func startWatching(_ record: HostRecord) {
        let id = record.id
        let watch = deps.watch(record)
        watches[id] = watch
        unreported.insert(id)
        let foreground = self.foreground
        let snapshots = watch.snapshots
        feeds[id] = Task { [weak self] in
            await watch.setForeground(foreground)
            await watch.start()
            for await snapshot in snapshots {
                self?.received(snapshot)
            }
        }
    }

    private func stopWatching(_ id: HostID, dropChannel: Bool = true) {
        feeds.removeValue(forKey: id)?.cancel()
        latest[id] = nil
        hub.forget(id)
        agentsRequested.remove(id)
        if let watch = watches.removeValue(forKey: id) {
            Task { await watch.stop() }
        }
        // The cached channel was opened with the old address and credentials.
        guard dropChannel else { return }
        let helpers = deps.helpers
        Task { await helpers.drop(id) }
    }

    private func received(_ snapshot: HostSnapshot) {
        guard let record = records[snapshot.hostID] else { return }
        latest[snapshot.hostID] = snapshot
        deps.diagnostics.reached(snapshot, record: record)
        publish(snapshot, record: record)
        learnWake(from: snapshot, record: record)
        learnRoutes(from: snapshot, record: record)
        askToOpen(from: snapshot)
        if snapshot.reach == .online, !record.noHelper {
            pair(record, capabilities: snapshot.capabilities)
            if foreground, unreported.remove(record.id) != nil {
                deps.reported(record.id, Set(snapshot.approvals.map(\.requestId)))
            }
        }
        if snapshot.reach == .online, !record.noHelper, !agentsRequested.contains(record.id) {
            agentsRequested.insert(record.id)
            Task {
                // A failed fetch is asked again the next time the machine comes online. Nobody
                // asked for it, so it never puts a question to the user.
                do { _ = try await fetchAgents(record, prompter: nil) } catch { agentsRequested.remove(record.id) }
            }
        }
    }

    private func publish(_ snapshot: HostSnapshot, record: HostRecord) {
        let view = mapper.map(snapshot, noHelper: record.noHelper, wake: record.wake, now: deps.now())
        hub.publish(view)
        onPublish(view)
    }

    /// Rows show agent names from the machine's own list once it arrives.
    private func learnAgentNames(_ agents: [AgentInfo], for hostID: HostID) {
        let names = Dictionary(agents.map { ($0.id, $0.name) }, uniquingKeysWith: { first, _ in first })
        guard mapper.agentNames[hostID] != names else { return }
        mapper.agentNames[hostID] = names
        if let snapshot = latest[hostID], let record = records[hostID] {
            publish(snapshot, record: record)
        }
    }

    private func perform(_ op: SessionOp, on hostID: HostID) async throws -> Session? {
        guard let watch = watches[hostID] else { throw LiveServiceError.unknownMachine }
        return try await watch.perform(op)
    }

    private func record(_ id: HostID) throws -> HostRecord {
        guard let record = records[id] else { throw LiveServiceError.unknownMachine }
        return record
    }

    private func helperRecord(_ id: HostID) throws -> HostRecord {
        let record = try record(id)
        guard !record.noHelper else { throw LiveServiceError.needsHelper }
        return record
    }

    // MARK: Settings

    private func applyStored(_ stored: AppSettings) {
        guard stored != settings else { return }
        applyingStoredSettings = true
        settings = stored
        applyingStoredSettings = false
    }

    /// The sync switch covers machines as well as settings; the settings store applies it to
    /// itself only. A switch flipped on another device reaches the host store here too.
    private func settingsChanged(from old: AppSettings) {
        guard settings != old else { return }
        deps.shareSettings(settings)
        if settings.relayURL != old.relayURL || settings.liveActivities != old.liveActivities
            || settings.liveActivityCommandMachines != old.liveActivityCommandMachines {
            pairOnlineMachines()
        }
        if settings.liveActivityCommandMachines != old.liveActivityCommandMachines { sendCommandChoice() }
        for terminal in terminals.values {
            terminal.apply(settings)
        }
        let value = settings
        if value.syncEnabled != old.syncEnabled {
            let hostRecords = self.hostRecords
            Task { await hostRecords.setSyncEnabled(value.syncEnabled) }
        }
        guard !applyingStoredSettings else { return }
        let settingsStore = self.settingsStore
        // Saves run one after another, so a burst such as a pinch ends on its last value.
        let previous = lastSave
        lastSave = Task {
            await previous?.value
            await settingsStore.save(value)
        }
    }
}
