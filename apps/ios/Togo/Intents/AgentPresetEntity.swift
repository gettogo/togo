import AppIntents
import Foundation
import TogoKit

/// A saved agent preset, by name. The flags stay in the app: the shortcut only says which
/// preset the new-session sheet starts with, and the sheet shows them before anything starts.
struct AgentPresetEntity: AppEntity {
    static let typeDisplayRepresentation = TypeDisplayRepresentation(
        name: LocalizedStringResource("intent.agent.type", defaultValue: "Agent")
    )
    static let defaultQuery = AgentPresetEntityQuery()

    var id: UUID
    var name: String

    init(_ preset: AgentPreset) {
        id = preset.id
        name = preset.name
    }

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(name)")
    }
}

/// Presets from the library on this phone.
struct AgentPresetEntityQuery: EntityStringQuery {
    func entities(for identifiers: [UUID]) async throws -> [AgentPresetEntity] {
        let presets = await Self.presets()
        return identifiers.compactMap { id in presets.first { $0.id == id } }
    }

    func entities(matching string: String) async throws -> [AgentPresetEntity] {
        let text = string.trimmingCharacters(in: .whitespacesAndNewlines)
        return await Self.presets().filter { text.isEmpty || $0.name.localizedStandardContains(text) }
    }

    func suggestedEntities() async throws -> [AgentPresetEntity] {
        await Self.presets()
    }

    @MainActor private static func presets() async -> [AgentPresetEntity] {
        await IntentHub.current.presets().map(AgentPresetEntity.init)
    }
}
