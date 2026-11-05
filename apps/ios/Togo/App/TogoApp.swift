import SwiftUI
import TogoKit
import TogoMosh
import UIKit

@main
struct TogoApp: App {
    @Environment(\.scenePhase) private var scenePhase
    @State private var composition: AppComposition
    @State private var resumeLock: ResumeLockController

    init() {
        let composition = Self.makeComposition()
        _composition = State(initialValue: composition)
        let store = composition.store
        _resumeLock = State(initialValue: ResumeLockController(biometrics: .app) { store.services.settings.resumeLock })
    }

    private static func makeComposition() -> AppComposition {
        #if DEBUG
        let launch = UITestLaunch.current
        if launch.fakes {
            // Without configuring TipKit no tip shows, so no popover covers what a test taps.
            if launch.skipIntro { LaunchGate.app.skip() } else { TogoTips.configure() }
            return AppComposition(uiTest: launch)
        }
        #endif
        TogoTips.configure()
        return AppComposition(isolated: AppComposition.isRunningTests)
    }

    var body: some Scene {
        WindowGroup {
            RootView(
                store: composition.store, terminals: composition.terminals, onboarding: composition.onboarding,
                tutorial: composition.tutorial
            )
                .overlay {
                    if resumeLock.locked {
                        ResumeLockScreen { resumeLock.unlock() }
                    }
                }
                .privacyCover()
                .onReceive(NotificationCenter.default.publisher(for: UIApplication.protectedDataWillBecomeUnavailableNotification)) { _ in
                    resumeLock.protectedDataBecameUnavailable()
                }
                .onOpenURL { url in
                    if let link = DeepLink(url: url) { composition.store.open(link) }
                }
                .task { composition.start() }
        }
        .commands { AppCommands() }
        .onChange(of: scenePhase) { _, phase in
            resumeLock.scenePhaseChanged(to: phase)
            switch phase {
            case .active: composition.live?.setForeground(true)
            case .background: composition.live?.setForeground(false)
            default: break
            }
        }
    }
}

/// Builds the one set of stores, connections and services the app runs on.
@MainActor final class AppComposition {
    /// Nil when the app runs on fake services for UI tests.
    let live: LiveServices?
    let store: AppStore
    let onboarding: any OnboardingServices
    let terminals = TerminalPool()
    let tutorial: TutorialModel
    private var started = false
    #if DEBUG
    private var uiTestMachines: UITestMachines?
    #endif

    private static let deviceIDDefault = "sync.deviceID"

    /// Under unit tests the app host must not reach the keychain's machines or iCloud, so
    /// `isolated` keeps every record in memory.
    init(isolated: Bool) {
        let secrets = KeychainSecretStore()
        let biometrics = BiometricSession.app
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
            prompter: NoPrompts(), connector: Connector(factories: [mosh, .ssh])
        )
        let deviceID = Self.deviceID()
        let signedIn = SignedInConnections()
        // Each store listens to its own backend: a backend's change stream has one consumer.
        func backend() -> any KeyValueBackend { isolated ? MemoryBackend() : DefaultsBackend() }
        func remote() -> (any KeyValueBackend)? { isolated ? nil : UbiquitousBackend() }
        let syncSwitch = SyncSwitch(backend: isolated ? MemoryBackend() : DefaultsBackend(namespace: "device."))
        let hostRecords = SyncedRecordStore<HostRecord>(
            prefix: "host.", local: backend(), remote: remote(), deviceID: deviceID, clock: { Date() },
            syncEnabled: syncSwitch.isOn
        )
        let settingsRecords = SyncedRecordStore<AppSettings>(
            prefix: "settings.", local: backend(), remote: remote(), deviceID: deviceID, clock: { Date() },
            syncEnabled: syncSwitch.isOn
        )
        let services = LiveServices(
            hostRecords: hostRecords, settingsRecords: settingsRecords, syncSwitch: syncSwitch,
            dependencies: LiveServices.Dependencies(
                watch: { record in
                    HostMonitor(
                        record: record,
                        factory: { record -> any SessionSource in
                            if let connection = await signedIn.take(record.id) {
                                return record.noHelper
                                    ? PlainTmuxSource(record: record, connected: connection)
                                    : HelperSource(record: record, connected: connection)
                            }
                            return record.noHelper ? PlainTmuxSource(record: record, env: env) : HelperSource(record: record, env: env)
                        },
                        path: SystemPathSource()
                    )
                },
                helpers: HelperChannels(env: env),
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
                forgetPassword: { id in try? secrets.delete(SSHConnection.passwordAccount(id)) }
            )
        )
        live = services
        tutorial = TutorialModel()
        store = AppStore(services: services)
        onboarding = LiveOnboardingServices(keys: keys, secrets: secrets, biometrics: biometrics, hostKeys: hostKeys)
        let store = self.store, terminals = self.terminals
        services.onMachineRemoved = { id in
            store.forgetMachine(id)
            terminals.sync(with: store)
            Task { await signedIn.discard(id) }
        }
        // A connection signed in with the old address or credentials must not serve the new ones.
        services.onMachineChanged = { id in
            Task { await signedIn.discard(id) }
            terminals.restartTabs(on: id, store: store)
        }
    }

    #if DEBUG
    init(uiTest launch: UITestLaunch) {
        let machines = UITestMachines(firstRun: launch.firstRun)
        uiTestMachines = machines
        live = nil
        tutorial = TutorialModel(defaults: launch.defaults())
        store = AppStore(services: machines.services)
        onboarding = FakeOnboardingServices(outcomes: Array(repeating: .ready(OnboardingSamples.ready), count: 4))
    }
    #endif

    /// Called by each window as it appears; only the first call starts anything.
    func start() {
        guard !started else { return }
        started = true
        let store = self.store
        if let live { Task { await live.start() } }
        #if DEBUG
        uiTestMachines?.start()
        #endif
        Task { await store.run() }
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
