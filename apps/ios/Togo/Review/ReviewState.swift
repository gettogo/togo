import Foundation
import TogoKit

/// The session a review screen looks at, and what its machine's helper lets the screens do.
struct ReviewSubject: Hashable, Sendable {
    var hostID: HostID
    var sessionID: String
    var bib: String?
    var title: String
    var cwd: String
    var managed: Bool
    var capabilities: Set<String>

    var canRead: Bool { capabilities.contains(Capability.git) }
    /// Writes need a session Togo started: the helper refuses them on any other.
    var canWrite: Bool { capabilities.contains(Capability.gitWrite) && managed }
    var canPorts: Bool { capabilities.contains(Capability.ports) }
}

enum ReviewTab: String, CaseIterable, Sendable {
    case changes, history, files, ports
}

struct ForwardInfo: Hashable, Sendable {
    var remote: ListeningPort
    var localPort: Int
    var url: URL
}

enum ForwardState: Hashable, Sendable {
    case idle, opening
    case open(ForwardInfo)
    case failed(String)
}

extension AppStore {
    /// Opens the review sheet for a session on the tab the user chose.
    func review(_ row: SessionRow, tab: ReviewTab) {
        let capabilities = controlCenter.machines.first { $0.id == row.hostID }?.capabilities ?? []
        let subject = ReviewSubject(
            hostID: row.hostID, sessionID: row.id, bib: row.bib, title: row.name, cwd: row.cwd,
            managed: row.managed, capabilities: capabilities
        )
        reviewEnded = nil
        reviewHasDraft = false
        sheet = .review(subject, tab)
    }
}
