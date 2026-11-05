import Foundation

public struct GestureBindings: Codable, Sendable, Equatable {
    /// Sequences use the key bar's escape grammar.
    public var doubleTap: String = "\t"
    public var tripleTap: String = "\u{1b}"
    public var twoFingerSwipe = true
    public var edgeSwipe = true
    public var pinchZoom = true
    public var touchMouse = false

    public static let `default` = GestureBindings()

    public init() {}

    private enum CodingKeys: String, CodingKey {
        case doubleTap, tripleTap, twoFingerSwipe, edgeSwipe, pinchZoom, touchMouse
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let d = GestureBindings.default
        doubleTap = try c.decodeIfPresent(String.self, forKey: .doubleTap) ?? d.doubleTap
        tripleTap = try c.decodeIfPresent(String.self, forKey: .tripleTap) ?? d.tripleTap
        twoFingerSwipe = try c.decodeIfPresent(Bool.self, forKey: .twoFingerSwipe) ?? d.twoFingerSwipe
        edgeSwipe = try c.decodeIfPresent(Bool.self, forKey: .edgeSwipe) ?? d.edgeSwipe
        pinchZoom = try c.decodeIfPresent(Bool.self, forKey: .pinchZoom) ?? d.pinchZoom
        touchMouse = try c.decodeIfPresent(Bool.self, forKey: .touchMouse) ?? d.touchMouse
    }
}

/// App-wide preferences. Fields missing from older or newer payloads fall back
/// to their defaults.
public struct AppSettings: Codable, Sendable, Equatable {
    public var themeID: String = "beacon"
    public var fontName: String = "SF Mono"
    public var fontSize: Double = 13
    public var cursor: String = "block"
    /// Encoded key bar layout. TogoTerminal owns the type; this package only
    /// carries the bytes so it does not depend on the terminal.
    public var keyBar: Data?
    /// Encoded terminal themes the user imported, carried as bytes for the same reason.
    public var importedThemes: Data?
    public var gestures: GestureBindings = .default
    /// This device's sync choice. It is never encoded: `SettingsStore` fills it in from
    /// `SyncSwitch`, so a record from another device cannot change it.
    public var syncEnabled = true
    public var resumeLock = true
    public var transport: TransportPreference = .auto
    /// The relay pushes go through; nil uses the hosted one.
    public var relayURL: URL?
    /// Machines, by id, whose Live Activity pushes may carry command text. Live Activities
    /// cannot be encrypted, so this is off for every machine until the user turns it on.
    /// This device's choice: never encoded, `SettingsStore` fills it in from `DeviceSettings`.
    public var liveActivityCommandMachines: Set<UUID> = []
    /// Lock-screen Approve works without unlocking the iPhone. Deny never asks to unlock.
    /// This device's choice: never encoded, so turning it on here cannot turn it on elsewhere.
    public var approveWithoutUnlock = false
    public var liveActivities = true

    public init() {}

    public func liveActivityCommand(for machine: UUID) -> Bool {
        liveActivityCommandMachines.contains(machine)
    }

    private enum CodingKeys: String, CodingKey {
        case themeID, fontName, fontSize, cursor, keyBar, importedThemes, gestures, resumeLock, transport
        case relayURL, liveActivities
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let d = AppSettings()
        themeID = try c.decodeIfPresent(String.self, forKey: .themeID) ?? d.themeID
        fontName = try c.decodeIfPresent(String.self, forKey: .fontName) ?? d.fontName
        fontSize = try c.decodeIfPresent(Double.self, forKey: .fontSize) ?? d.fontSize
        cursor = try c.decodeIfPresent(String.self, forKey: .cursor) ?? d.cursor
        keyBar = try? c.decodeIfPresent(Data.self, forKey: .keyBar)
        importedThemes = try? c.decodeIfPresent(Data.self, forKey: .importedThemes)
        gestures = try c.decodeIfPresent(GestureBindings.self, forKey: .gestures) ?? d.gestures
        resumeLock = try c.decodeIfPresent(Bool.self, forKey: .resumeLock) ?? d.resumeLock
        transport = try c.decodeIfPresent(TransportPreference.self, forKey: .transport) ?? d.transport
        relayURL = try? c.decodeIfPresent(URL.self, forKey: .relayURL)
        liveActivities = try c.decodeIfPresent(Bool.self, forKey: .liveActivities) ?? d.liveActivities
    }
}

/// Whether this device syncs, kept on the device only. Settings from another device
/// must never turn sync back on here, so the choice lives outside the synced record
/// and is read before anything is merged.
public struct SyncSwitch: Sendable {
    private static let key = "sync.enabled"
    private let backend: any KeyValueBackend

    public init(backend: any KeyValueBackend) {
        self.backend = backend
    }

    /// On until the user turns it off.
    public var isOn: Bool {
        backend.data(forKey: Self.key).map { $0 != Data([0]) } ?? true
    }

    public func set(_ on: Bool) {
        backend.set(Data([on ? 1 : 0]), forKey: Self.key)
    }
}

/// Settings that weaken this device's protection, kept on the device only: a record synced
/// from another device must never carry them here.
public struct DeviceSettings: Sendable {
    private static let approveKey = "push.approveWithoutUnlock"
    private static let commandsKey = "push.liveActivityCommandMachines"
    private let backend: any KeyValueBackend

    public init(backend: any KeyValueBackend) {
        self.backend = backend
    }

    /// Copies this device's values into `settings`.
    public func apply(to settings: inout AppSettings) {
        settings.approveWithoutUnlock = backend.data(forKey: Self.approveKey) == Data([1])
        settings.liveActivityCommandMachines = backend.data(forKey: Self.commandsKey)
            .flatMap { try? JSONDecoder().decode(Set<UUID>.self, from: $0) } ?? []
    }

    public func save(_ settings: AppSettings) {
        backend.set(Data([settings.approveWithoutUnlock ? 1 : 0]), forKey: Self.approveKey)
        backend.set(try? JSONEncoder().encode(settings.liveActivityCommandMachines.sorted { $0.uuidString < $1.uuidString }),
                    forKey: Self.commandsKey)
    }
}

/// Preferences as one synced record. The sync switch is a setting on screen but lives in
/// `SyncSwitch`; saving applies it to the underlying store.
public actor SettingsStore {
    public nonisolated let changes: AsyncStream<AppSettings>

    private static let recordID = "app"
    private let records: SyncedRecordStore<AppSettings>
    private let syncSwitch: SyncSwitch
    private let device: DeviceSettings
    private let pump: Task<Void, Never>

    public init(
        records: SyncedRecordStore<AppSettings>, syncSwitch: SyncSwitch,
        device: DeviceSettings = DeviceSettings(backend: MemoryBackend())
    ) {
        self.records = records
        self.syncSwitch = syncSwitch
        self.device = device
        let (stream, continuation) = AsyncStream<AppSettings>.makeStream()
        changes = stream
        let incoming = records.changes
        pump = Task {
            for await all in incoming {
                if var settings = all[Self.recordID] {
                    settings.syncEnabled = syncSwitch.isOn
                    device.apply(to: &settings)
                    continuation.yield(settings)
                }
            }
            continuation.finish()
        }
    }

    deinit {
        pump.cancel()
    }

    public func load() async -> AppSettings {
        var settings = await records.all()[Self.recordID] ?? AppSettings()
        settings.syncEnabled = syncSwitch.isOn
        device.apply(to: &settings)
        await records.setSyncEnabled(settings.syncEnabled)
        return settings
    }

    public func save(_ settings: AppSettings) async {
        syncSwitch.set(settings.syncEnabled)
        device.save(settings)
        await records.setSyncEnabled(settings.syncEnabled)
        // The stored record always carries the default, so flipping the switch is not an edit.
        var shared = settings
        shared.syncEnabled = AppSettings().syncEnabled
        await records.put(Self.recordID, shared)
    }
}
