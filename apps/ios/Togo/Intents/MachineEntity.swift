import AppIntents
import CoreSpotlight
import Foundation

/// A saved machine, by name. Spotlight holds the name and nothing else about it.
struct MachineEntity: AppEntity, IndexedEntity {
    static let typeDisplayRepresentation = TypeDisplayRepresentation(
        name: LocalizedStringResource("intent.machine.type", defaultValue: "Machine")
    )
    static let defaultQuery = MachineEntityQuery()

    var id: HostID
    var name: String

    init(id: HostID, name: String) {
        self.id = id
        self.name = name
    }

    init(_ machine: IntentSnapshot.Machine) {
        self.init(id: machine.id, name: machine.name)
    }

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(name)")
    }

    var attributeSet: CSSearchableItemAttributeSet {
        let set = CSSearchableItemAttributeSet()
        set.displayName = name
        set.keywords = [name]
        return set
    }
}

/// Machines from the snapshot the app keeps, so asking never waits on the network.
struct MachineEntityQuery: EntityStringQuery {
    func entities(for identifiers: [HostID]) async throws -> [MachineEntity] {
        let machines = await Self.machines()
        return identifiers.compactMap { id in machines.first { $0.id == id } }
    }

    func entities(matching string: String) async throws -> [MachineEntity] {
        let text = string.trimmingCharacters(in: .whitespacesAndNewlines)
        return await Self.machines().filter { text.isEmpty || $0.name.localizedStandardContains(text) }
    }

    func suggestedEntities() async throws -> [MachineEntity] {
        await Self.machines()
    }

    @MainActor private static func machines() -> [MachineEntity] {
        IntentHub.current.snapshot().machines.map(MachineEntity.init)
    }
}
