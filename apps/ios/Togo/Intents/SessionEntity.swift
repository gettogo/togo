import AppIntents
import CoreSpotlight
import Foundation

/// A session by bib, name and machine, as the app last saw it. Spotlight holds those three
/// and nothing else: no folder, no agent output.
struct SessionEntity: AppEntity, IndexedEntity {
    static let typeDisplayRepresentation = TypeDisplayRepresentation(
        name: LocalizedStringResource("intent.session.type", defaultValue: "Session")
    )
    static let defaultQuery = SessionEntityQuery()

    var id: String
    var hostID: HostID
    var machine: String
    var bib: String
    var name: String

    init(_ session: IntentSnapshot.Session) {
        id = session.id
        hostID = session.hostID
        machine = session.machine
        bib = session.bib
        name = session.name
    }

    /// The link a notification for this session carries. Bibs are reused, so a session saved
    /// in a shortcut opens whichever session holds the bib on that machine now.
    var deepLink: DeepLink { .session(bib: bib, host: machine) }

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(
            title: "\(bib) \(name)",
            subtitle: LocalizedStringResource("intent.session.subtitle", defaultValue: "On \(machine)")
        )
    }

    var attributeSet: CSSearchableItemAttributeSet {
        let set = CSSearchableItemAttributeSet()
        set.displayName = "\(bib) \(name)"
        set.contentDescription = machine
        set.keywords = [bib, name, machine]
        return set
    }
}

/// Sessions from the snapshot the app keeps. A match on the bib, the session's name or its
/// machine's name counts, ignoring case and accents.
struct SessionEntityQuery: EntityStringQuery {
    func entities(for identifiers: [String]) async throws -> [SessionEntity] {
        let sessions = await Self.sessions()
        return identifiers.compactMap { id in sessions.first { $0.id == id } }
    }

    func entities(matching string: String) async throws -> [SessionEntity] {
        let text = string.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return await Self.sessions() }
        return await Self.sessions().filter { session in
            [session.bib, session.name, session.machine].contains { $0.localizedStandardContains(text) }
        }
    }

    func suggestedEntities() async throws -> [SessionEntity] {
        await Self.sessions()
    }

    @MainActor private static func sessions() -> [SessionEntity] {
        IntentHub.current.snapshot().sessions.map(SessionEntity.init)
    }
}
