import Foundation
import Observation
import SwiftUI
import TogoKit

/// The user's agent presets and the preset each folder last started with. Both sync through
/// iCloud like machines do, and follow the same sync switch. Favorite and recently started
/// agents stay on this device.
@MainActor @Observable final class AgentLibrary {
    private(set) var presets: [AgentPreset]
    /// Starred agent ids, in the order they were starred.
    private(set) var favorites: [String]
    /// Agent ids started from this device, newest first.
    private(set) var recentAgents: [String]

    @ObservationIgnored private let records: SyncedRecordStore<AgentPreset>
    @ObservationIgnored private let launches: SyncedRecordStore<FolderLaunch>
    @ObservationIgnored private let memory: FolderLaunchMemory
    /// Where favorites and recent agents are kept; nil keeps them in memory.
    @ObservationIgnored private let defaults: UserDefaults?
    @ObservationIgnored private var listener: Task<Void, Never>?
    /// The first read of the stored presets; every later start waits for it.
    @ObservationIgnored private var firstLoad: Task<Void, Never>?

    static let favoritesKey = "agents.favorites"
    static let recentKey = "agents.recent"

    init(
        presets records: SyncedRecordStore<AgentPreset>, launches: SyncedRecordStore<FolderLaunch>,
        loaded: [AgentPreset] = [], defaults: UserDefaults? = nil
    ) {
        self.records = records
        self.launches = launches
        self.defaults = defaults
        memory = FolderLaunchMemory(store: launches)
        presets = Self.ordered(loaded)
        favorites = AgentOrder.ids(from: defaults?.object(forKey: Self.favoritesKey))
        recentAgents = AgentOrder.ids(from: defaults?.object(forKey: Self.recentKey))
    }

    /// A library kept in memory, already holding `presets`: for previews, tests and views
    /// built without the app.
    static func inMemory(_ presets: [AgentPreset] = []) -> AgentLibrary {
        let records = SyncedRecordStore<AgentPreset>(
            prefix: AgentPreset.storePrefix, local: MemoryBackend(), remote: nil, deviceID: "memory", clock: { Date() }
        )
        let launches = SyncedRecordStore<FolderLaunch>(
            prefix: FolderLaunchMemory.storePrefix, local: MemoryBackend(), remote: nil, deviceID: "memory", clock: { Date() }
        )
        return AgentLibrary(presets: records, launches: launches, loaded: presets)
    }

    /// Reads the stored presets and follows changes from other devices. A second call, such as
    /// a Shortcuts query on a cold launch, returns once the first read is in.
    func start() async {
        if let firstLoad {
            await firstLoad.value
            return
        }
        let changes = records.changes
        listener = Task { [weak self] in
            for await all in changes {
                self?.presets = Self.ordered(Array(all.values))
            }
        }
        let records = self.records
        let load = Task { [weak self] in
            let stored = await records.all()
            self?.presets = Self.ordered(Array(stored.values))
        }
        firstLoad = load
        await load.value
    }

    func preset(_ id: UUID?) -> AgentPreset? {
        presets.first { $0.id == id }
    }

    func save(_ preset: AgentPreset) async {
        presets = Self.ordered(presets.filter { $0.id != preset.id } + [preset])
        await records.put(preset.id.uuidString, preset)
    }

    func delete(_ id: UUID) async {
        presets.removeAll { $0.id == id }
        await records.delete(id.uuidString)
        await memory.forget(preset: id)
    }

    func rememberedPreset(host: HostID, cwd: String) async -> UUID? {
        await memory.preset(host: host, cwd: cwd)
    }

    func remember(_ id: UUID?, host: HostID, cwd: String) async {
        await memory.remember(id, host: host, cwd: cwd)
    }

    func isFavorite(_ agent: String) -> Bool { favorites.contains(agent) }

    func toggleFavorite(_ agent: String) {
        favorites = AgentOrder.toggled(agent, in: favorites)
        defaults?.set(favorites, forKey: Self.favoritesKey)
    }

    /// Records a start. Lists no longer rank by it; it stays so the stored value keeps its shape.
    func used(_ agent: String) {
        recentAgents = AgentOrder.used(agent, in: recentAgents)
        defaults?.set(recentAgents, forKey: Self.recentKey)
    }

    func setSyncEnabled(_ on: Bool) async {
        await records.setSyncEnabled(on)
        await launches.setSyncEnabled(on)
    }

    private nonisolated static func ordered(_ presets: [AgentPreset]) -> [AgentPreset] {
        presets.sorted {
            let order = $0.name.localizedCaseInsensitiveCompare($1.name)
            return order == .orderedSame ? $0.id.uuidString < $1.id.uuidString : order == .orderedAscending
        }
    }
}

extension EnvironmentValues {
    /// Nil in views built without the app, such as previews of a single screen.
    @Entry var agentLibrary: AgentLibrary?
}
