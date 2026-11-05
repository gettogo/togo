import SwiftUI
import TogoKit
import TogoMosh
import UIKit

@main
struct TogoApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
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
                .defaultAppStorage(composition.appStorage)
        }
        .commands { AppCommands() }
        .onChange(of: scenePhase) { _, phase in
            resumeLock.scenePhaseChanged(to: phase)
            switch phase {
            case .active:
                composition.live?.setForeground(true)
                composition.review?.setForeground(true)
                composition.push?.becameActive()
            case .background:
                // Forwards are marked paused first; the helper connections close once they stopped.
                composition.review?.setForeground(false)
                composition.live?.setForeground(false)
            default: break
            }
        }
    }
}

/// Builds the one set of stores, connections and services the app runs on.
@MainActor final class AppComposition {
    /// Nil when the app runs on fake services for UI tests.
    let live: LiveServices?
    /// Nil when the app runs on fake services for UI tests.
    let review: LiveReviewServices?
    let store: AppStore
    let onboarding: any OnboardingServices
    let terminals = TerminalPool()
    let tutorial: TutorialModel
    /// Nil under unit and UI tests and in a process without the app group.
    let push: AppPush?
    /// Where views keep `@AppStorage` settings: the UI tests' own suite under `-ui-fakes`, so
    /// a choice one test makes never reaches the next.
    var appStorage: UserDefaults = .standard
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
        let helpers = HelperChannels(env: env)
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
        let push = isolated ? nil : AppPush.make()
        self.push = push
        let services = LiveServices(
            hostRecords: hostRecords, settingsRecords: settingsRecords, syncSwitch: syncSwitch,
            device: DeviceSettings(backend: isolated ? MemoryBackend() : AppPush.deviceBackend()),
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
                reported: { id, open in push?.reported(id, open: open) }
            )
        )
        live = services
        let review = LiveReviewServices(
            channels: helpers,
            records: { [weak services] in services?.hostRecord($0) },
            opener: { try await helpers.connection(for: $0) },
            prompter: { [weak services] in services?.userPrompter($0) }
        )
        self.review = review
        services.beforeDroppingHelpers = { [weak review] in await review?.settle() }
        tutorial = TutorialModel()
        store = AppStore(services: services, reviewServices: review)
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
        push?.connect(services: services, store: store)
    }

    #if DEBUG
    init(uiTest launch: UITestLaunch) {
        let machines = UITestMachines(firstRun: launch.firstRun)
        uiTestMachines = machines
        live = nil
        review = nil
        push = nil
        let defaults = launch.defaults()
        appStorage = defaults
        tutorial = TutorialModel(defaults: defaults)
        store = AppStore(services: machines.services, reviewServices: ReviewFakes.uiTestServices())
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
        // Never asks: permission comes up when a machine first can send pushes, or from Settings.
        if let registrar = push?.registrar {
            Task { await registrar.refresh() }
        }
        #if DEBUG
        if let push { PushE2E.start(push) }
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
