import Foundation
import Observation
import TogoKit
import TogoMosh

/// Builds the one set of connections and services the app runs on, and a store for each
/// window over them. App-wide services live here, shared by every window; what a window shows
/// lives in its store.
@MainActor @Observable final class AppComposition {
    let services: any AppServices
    /// Nil when the app runs on fake services.
    let live: LiveServices?
    /// Nil when the app runs on fake services.
    let review: LiveReviewServices?
    let onboarding: any OnboardingServices
    /// Every window's terminals: a session has one connection however many windows list it.
    let terminals = TerminalPool()
    let tutorial: TutorialModel
    /// How features reach a machine's helper.
    let machineTools: any MachineTools
    /// Saved agent presets and the preset each folder last started with.
    let agentLibrary: AgentLibrary
    /// Nil under unit and UI tests and in a process without the app group.
    let push: AppPush?
    /// Where views keep `@AppStorage` settings: the UI tests' own suite under `-ui-fakes`, so
    /// a choice one test makes never reaches the next.
    @ObservationIgnored var appStorage: UserDefaults = .standard
    /// The store of the window that last became active. Links and notification taps open there.
    private(set) var activeStore: AppStore?
    /// Links that arrived before any window had a store, such as an intent that launched the app.
    @ObservationIgnored private var waitingLinks: [DeepLink] = []
    private let reviewServices: (any ReviewServices)?
    /// Nil when the app runs on fake services: every session shows the terminal only.
    private let harnessServices: (any HarnessServices)?
    @ObservationIgnored private var windows: [(id: WindowID, store: AppStore, run: Task<Void, Never>)] = []
    @ObservationIgnored private var started = false
    #if DEBUG
    @ObservationIgnored private var uiTestMachines: UITestMachines?
    @ObservationIgnored private var liveDemo: LiveDemo?
    #endif

    private static let deviceIDDefault = "sync.deviceID"

    /// Under unit tests the app host must not reach the keychain's machines or iCloud, so
    /// `isolated` keeps every record in memory.
    init(isolated: Bool) {
        #if DEBUG
        // The native end-to-end tests sign in on a simulator with no passcode or face.
        let secrets: any SecretStore = E2EHost.enabled ? E2EHost.PlainSecrets() : KeychainSecretStore()
        let biometrics = E2EHost.enabled ? BiometricSession(evaluator: E2EHost.Evaluator()) : BiometricSession.app
        #else
        let secrets = KeychainSecretStore()
        let biometrics = BiometricSession.app
        #endif
        let keys = KeychainKeyStore(secrets: secrets, biometrics: biometrics)
        let support = URL.applicationSupportDirectory
        try? FileManager.default.createDirectory(at: support, withIntermediateDirectories: true)
        let hostKeys = FileHostKeyStore(url: support.appending(path: "known_hosts.json"))
        let mosh = TransportFactory(kind: .mosh, unavailableReason: TransportFactory.moshUnavailableReason) { request in
            try await MoshFactory.make(
                connection: request.connection, profile: request.profile, argv: request.argv,
                cols: request.cols, rows: request.rows
            )
        }
        let env = SessionEnvironment(
            keys: keys, secrets: secrets, biometrics: biometrics, hostKeys: hostKeys,
            prompter: NoPrompts(), connector: Connector(factories: [mosh, ETFactory.factory, .ssh])
        )
        let deviceID = Self.deviceID()
        let signedIn = SignedInConnections()
        let helpers = HelperChannels(env: env)
        // Each store listens to its own backend: a backend's change stream has one consumer.
        func backend() -> any KeyValueBackend { isolated ? MemoryBackend() : DefaultsBackend() }
        func remote() -> (any KeyValueBackend)? { isolated ? nil : FirstUseBackend { UbiquitousBackend() } }
        let syncSwitch = SyncSwitch(backend: isolated ? MemoryBackend() : DefaultsBackend(namespace: "device."))
        let hostRecords = SyncedRecordStore<HostRecord>(
            prefix: "host.", local: backend(), remote: remote(), deviceID: deviceID, clock: { Date() },
            syncEnabled: syncSwitch.isOn
        )
        let settingsRecords = SyncedRecordStore<AppSettings>(
            prefix: "settings.", local: backend(), remote: remote(), deviceID: deviceID, clock: { Date() },
            syncEnabled: syncSwitch.isOn
        )
        agentLibrary = AgentLibrary(
            presets: SyncedRecordStore<AgentPreset>(
                prefix: AgentPreset.storePrefix, local: backend(), remote: remote(), deviceID: deviceID,
                clock: { Date() }, syncEnabled: syncSwitch.isOn
            ),
            launches: SyncedRecordStore<FolderLaunch>(
                prefix: FolderLaunchMemory.storePrefix, local: backend(), remote: remote(), deviceID: deviceID,
                clock: { Date() }, syncEnabled: syncSwitch.isOn
            ),
            defaults: isolated ? nil : .standard
        )
        let push = isolated ? nil : AppPush.make()
        self.push = push
        // Only the public half of the key Togo made, read from the index without unlocking it.
        push?.pairing.deviceKey = { record in
            DeviceKeyLine.of(record, deviceKeyID: LiveOnboardingServices.storedDeviceKeyID) {
                (try? keys.info(id: $0))?.publicKeyLine
            }
        }
        let relay = LiveServicesRelay()
        let services = LiveServices(
            hostRecords: hostRecords, settingsRecords: settingsRecords, syncSwitch: syncSwitch,
            device: DeviceSettings(backend: isolated ? MemoryBackend() : AppPush.deviceBackend()),
            // Never synced: which machines this device signed in to, and with which of its keys.
            deviceMachines: DeviceMachines(
                backend: isolated ? MemoryBackend() : DefaultsBackend(namespace: "device."),
                hasKey: { (try? keys.info(id: $0)) != nil },
                deviceKeyID: { LiveOnboardingServices.storedDeviceKeyID }
            ),
            dependencies: LiveServices.Dependencies(
                watch: { record in
                    HostMonitor(
                        record: record,
                        factory: { record -> any SessionSource in
                            let found: @Sendable (String) -> Void = { path in
                                Task { @MainActor in await relay.target?.helperFound(record.id, path: path) }
                            }
                            if let connection = await signedIn.take(record.id) {
                                return record.noHelper
                                    ? PlainTmuxSource(record: record, connected: connection, helperFound: found)
                                    : HelperSource(record: record, connected: connection)
                            }
                            return record.noHelper
                                ? PlainTmuxSource(record: record, env: env, helperFound: found)
                                : HelperSource(record: record, env: env)
                        },
                        path: SystemPathSource()
                    )
                },
                helpers: helpers,
                connect: { record, sessionID, transport, prompter in
                    var profile = record.profile
                    profile.transport = transport
                    var env = env
                    env.prompter = prompter
                    return record.noHelper
                        ? SessionAttach.connectPlainTmux(profile: profile, sessionID: sessionID, env: env)
                        : SessionAttach.connect(profile: profile, sessionID: sessionID, env: env, helperPath: record.helperPath)
                },
                signIn: { record, prompter in
                    var asking = env
                    asking.prompter = prompter
                    let connection = asking.connection(for: record.profile)
                    do {
                        try await connection.connect()
                    } catch {
                        await connection.close()
                        throw error
                    }
                    await signedIn.put(connection, for: record.id)
                },
                forgetPassword: { id in try? secrets.delete(SSHConnection.passwordAccount(id)) },
                registrar: push?.registrar,
                pairing: push?.pairing,
                shareSettings: { push?.share($0) },
                reported: { id, open in push?.reported(id, open: open) },
                trustedHostKeys: { (try? await hostKeys.fingerprints(for: HostKeyTrust.hostID($0))) ?? [] }
            )
        )
        relay.target = services
        live = services
        self.services = services
        let review = LiveReviewServices(
            channels: helpers,
            records: { [weak services] in services?.hostRecord($0) },
            opener: { try await helpers.connection(for: $0) },
            prompter: { [weak services] in services?.userPrompter($0) }
        )
        self.review = review
        reviewServices = review
        // Each open session's feed runs its own helper on the machine's existing connection.
        harnessServices = LiveHarnessServices(
            channels: helpers,
            records: { [weak services] in services?.hostRecord($0) },
            openFeed: { record in
                let connection = try await helpers.connection(for: record)
                return try await connection.openExec(LiveHarnessServices.serveCommand(for: record))
            }
        )
        let machineTools = LiveMachineTools(
            channels: helpers, records: { [weak services] in services?.hostRecord($0) },
            prompter: { [weak services] in services?.userPrompter($0) }
        )
        self.machineTools = machineTools
        LinkCenter.shared.tunnel = LiveCallbackTunnel(
            helpers: helpers, records: { [weak services] in services?.hostRecord($0) }, review: review
        )
        LinkCenter.shared.openOnMac = { [weak machineTools] hostID, url in
            let link = url.absoluteString
            try await machineTools?.call(hostID) { try await $0.openOnMac(url: link) }
        }
        services.beforeDroppingHelpers = { [weak review] in await review?.settle() }
        tutorial = TutorialModel()
        onboarding = LiveOnboardingServices(keys: keys, secrets: secrets, biometrics: biometrics, hostKeys: hostKeys)
        LinkCenter.shared.codeSender = LiveSignInCodeSender(
            tools: machineTools,
            answer: { [weak services] host, request, decision in
                try await services?.answer(hostID: host, requestID: request, decision: decision)
            },
            pendingSecret: { [weak self] host, session in self?.pendingSecretRequest(host, session) }
        )
        LinkCenter.shared.awaitsCode = { [weak self] host, session in self?.pendingSecretRequest(host, session) != nil }
        services.onMachineRemoved = { [weak self] id in
            self?.forgetMachine(id)
            Task { await signedIn.discard(id) }
        }
        // A connection signed in with the old address or credentials must not serve the new ones.
        services.onMachineChanged = { [weak self] id in
            Task { await signedIn.discard(id) }
            self?.terminals.restartTabs(on: id)
        }
        // A tab restored before the stored machines were read attaches once they are.
        services.onMachineArrived = { [weak self] id in self?.terminals.restartTabs(on: id) }
        // Replies from the Watch: a saved password or key signs in, nothing asks.
        let quietTools = LiveMachineTools(
            channels: helpers, records: { [weak services] in services?.hostRecord($0) },
            prompter: { _ in nil }
        )
        push?.connect(services: services, tools: machineTools, quietTools: quietTools) { [weak self] in self?.activeStore }
        if !isolated {
            ShortcutsFeed.connectIntents(library: agentLibrary) { [weak self] in self?.open($0) }
        }
        services.showLocalHosts()
        #if DEBUG
        if E2EHost.enabled { Task { await E2EHost.seed(services: services, keys: keys, hostKeys: hostKeys) } }
        #endif
    }

    /// Fake services with nothing behind them, for tests and the UI tests' launch.
    init(
        services: any AppServices, reviewServices: (any ReviewServices)?, onboarding: any OnboardingServices,
        tutorial: TutorialModel, machineTools: any MachineTools = NoMachineTools(), agentLibrary: AgentLibrary = .inMemory(),
        harnessServices: (any HarnessServices)? = nil
    ) {
        self.services = services
        self.machineTools = machineTools
        self.agentLibrary = agentLibrary
        self.reviewServices = reviewServices
        self.harnessServices = harnessServices
        self.onboarding = onboarding
        self.tutorial = tutorial
        live = nil
        review = nil
        push = nil
    }

    #if DEBUG
    convenience init(uiTest launch: UITestLaunch) {
        let replay = launch.replay.flatMap(ReplayScript.load).map(ReplayHarnessServices.init)
        let machines = UITestMachines(
            firstRun: launch.firstRun, oneMachine: launch.oneMachine, oldHelper: launch.oldHelper,
            authNeeded: launch.authNeeded, demoRoster: launch.demoRoster, replay: replay
        )
        let defaults = launch.defaults()
        self.init(
            services: machines.services, reviewServices: ReviewFakes.uiTestServices(),
            onboarding: FakeOnboardingServices(outcomes: Array(repeating: .ready(OnboardingSamples.ready), count: 4)),
            tutorial: TutorialModel(defaults: defaults), machineTools: FakeMachineTools(), harnessServices: replay
        )
        uiTestMachines = machines
        appStorage = defaults
        if launch.demoAPI {
            for agent in DemoAPIFixture.favorites { agentLibrary.toggleFavorite(agent) }
        }
        if LiveDemo.enabled, let replay {
            let first = machines.firstSession
            let demo = LiveDemo(session: first?.id ?? "", name: first?.name ?? "", agent: replay.script.agent)
            demo.prepare(appHostID: machines.services.hosts.first?.id)
            demo.onAnswered = { [weak machines] requestID, _ in machines?.answeredElsewhere(requestID) }
            replay.onPlayback = { [weak demo] in demo?.playback($0) }
            liveDemo = demo
        }
    }

    convenience init(services: any AppServices) {
        self.init(services: services, reviewServices: nil, onboarding: FakeOnboardingServices(), tutorial: TutorialModel())
    }
    #endif

    // MARK: Windows

    /// A store for a new window, with the tabs it had when the system last saved it.
    func makeStore(window: WindowID, restoring saved: WindowRestoration?) -> AppStore {
        if let existing = windows.first(where: { $0.id == window }) { return existing.store }
        let store = AppStore(services: services, reviewServices: reviewServices, harnessServices: harnessServices)
        if let saved { store.restore(saved) }
        let run = Task { await store.run() }
        windows.append((window, store, run))
        terminals.sync(window: window, with: store)
        if activeStore == nil {
            activeStore = store
            let waiting = waitingLinks
            waitingLinks = []
            waiting.forEach(store.open)
        }
        return store
    }

    /// The window came to the front.
    func activate(_ window: WindowID) {
        guard let store = windows.first(where: { $0.id == window })?.store else { return }
        activeStore = store
    }

    /// The window's scene is gone. Its terminals stay while another window lists them.
    func release(window: WindowID) {
        guard let index = windows.firstIndex(where: { $0.id == window }) else { return }
        let gone = windows.remove(at: index)
        gone.run.cancel()
        HarnessTabsRegistry.shared.release(gone.store)
        terminals.release(window: window)
        if activeStore === gone.store { activeStore = windows.last?.store }
    }

    /// A `togo://` link, from outside the app or a notification.
    func open(_ link: DeepLink) {
        if let activeStore { activeStore.open(link) } else { waitingLinks.append(link) }
    }

    /// A removed machine leaves every window at once, terminals included.
    private func forgetMachine(_ id: HostID) {
        for window in windows {
            window.store.forgetMachine(id)
            terminals.sync(window: window.id, with: window.store)
        }
    }

    /// Called by each window as it appears; only the first call starts anything. Machines
    /// already listed from this device connect once the first screen is up; with none listed
    /// the stores load now, since the first screen waits for them.
    func start() {
        guard !started else { return }
        started = true
        if let live {
            if live.hostsLoaded {
                PerfSignposts.app.afterInteractive { Task { await live.start() } }
            } else {
                Task { await live.start() }
            }
            let library = agentLibrary
            PerfSignposts.app.afterInteractive {
                Task {
                    await library.start()
                    // The presets follow the same iCloud switch as machines and settings.
                    for await on in Observations({ live.settings.syncEnabled }) {
                        await library.setSyncEnabled(on)
                    }
                }
            }
            if !Self.isRunningTests {
                PerfSignposts.app.afterInteractive { ShortcutsFeed.start(services: live) }
            }
        }
        #if DEBUG
        uiTestMachines?.start()
        #endif
        // Each step on its own run-loop turn, so the first screen keeps drawing between them.
        guard let push else { return }
        PerfSignposts.app.afterInteractive { push.start() }
        // Never asks: permission comes up when a machine first can send pushes, or from Settings.
        PerfSignposts.app.afterInteractive { [registrar = push.registrar] in Task { await registrar.refresh() } }
        #if DEBUG
        PerfSignposts.app.afterInteractive { PushE2E.start(push) }
        #endif
    }

    static var isRunningTests: Bool {
        ProcessInfo.processInfo.environment["XCTestConfigurationFilePath"] != nil
    }

    /// Ties this device's edits together when records from several devices are merged.
    private static func deviceID() -> String {
        if let id = UserDefaults.standard.string(forKey: deviceIDDefault) { return id }
        let id = UUID().uuidString
        UserDefaults.standard.set(id, forKey: deviceIDDefault)
        return id
    }
}

/// A connection the user just signed in on, waiting for its machine's monitor to take it.
private actor SignedInConnections {
    private var ready: [UUID: SSHConnection] = [:]

    func put(_ connection: SSHConnection, for id: UUID) async {
        let replaced = ready.updateValue(connection, forKey: id)
        await replaced?.close()
    }

    func take(_ id: UUID) -> SSHConnection? {
        ready.removeValue(forKey: id)
    }

    func discard(_ id: UUID) async {
        await ready.removeValue(forKey: id)?.close()
    }
}

/// Connections the app makes on its own never ask the user anything: a saved password or key
/// signs in, and anything else fails, which the control center shows as needing sign-in. A
/// machine that asks for a code at every login therefore needs Sign in after each return
/// from the background; nothing can answer a code on the user's behalf.
private struct NoPrompts: ConnectionPrompter {
    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? { nil }
    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool { false }
}

extension BiometricSession {
    /// The one session for the app. The resume lock and every key signature share it, so
    /// a device lock or a long background clears the context signing uses too.
    static let app = BiometricSession()
}

extension AppComposition {
    /// The open request in a session whose agent waits for a pasted code or key.
    func pendingSecretRequest(_ host: HostID, _ session: String) -> String? {
        activeStore?.controlCenter.requests.first {
            $0.hostID == host && $0.sessionID == session && $0.phase == .open && $0.signIn?.secret == true
        }?.id
    }
}

/// Lets the machine watches, built before `LiveServices` exists, reach it later.
@MainActor private final class LiveServicesRelay {
    weak var target: LiveServices?
}
