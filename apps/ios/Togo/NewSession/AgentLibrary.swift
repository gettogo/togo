import Foundation
import Observation
import SwiftUI
import TogoKit

/// The user's agent presets and the preset each folder last started with. Both sync through
/// iCloud like machines do, and follow the same sync switch.
@MainActor @Observable final class AgentLibrary {
    private(set) var presets: [AgentPreset]

    @ObservationIgnored private let records: SyncedRecordStore<AgentPreset>
    @ObservationIgnored private let launches: SyncedRecordStore<FolderLaunch>
    @ObservationIgnored private let memory: FolderLaunchMemory
    @ObservationIgnored private var listener: Task<Void, Never>?
    /// The first read of the stored presets; every later start waits for it.
    @ObservationIgnored private var firstLoad: Task<Void, Never>?

    init(
        presets records: SyncedRecordStore<AgentPreset>, launches: SyncedRecordStore<FolderLaunch>,
        loaded: [AgentPreset] = []
    ) {
        self.records = records
        self.launches = launches
        memory = FolderLaunchMemory(store: launches)
        presets = Self.ordered(loaded)
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
