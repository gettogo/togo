import SwiftUI
import TogoKit

/// VoiceOver rotors over the session list: every session, the ones that need the user, and
/// the machines. Rows and headings carry `.id(Target)` so a rotor entry can reach them.
enum SessionRotors {
    enum Target: Hashable, Sendable {
        case row(TabID)
        case machine(HostID)
        case card(String)
    }

    struct Entry: Equatable, Sendable {
        var target: Target
        var label: String
    }

    /// Every row in list order, read "04, Claude Code, Needs you".
    static func sessions(_ machines: [MachineSection]) -> [Entry] {
        machines.flatMap { machine in
            machine.sessions.map(entry)
        }
    }

    /// Questions without a session row of their own, then every row that needs the user.
    static func needsYou(_ machines: [MachineSection], requests: [RequestCard]) -> [Entry] {
        let cards = requests.filter { $0.sessionID == nil && $0.phase == .open }.map { card in
            Entry(target: .card(card.id), label: [card.agentName, card.title].joined(separator: ", "))
        }
        return cards + sessions(machines.map { machine in
            var machine = machine
            machine.sessions = machine.sessions.filter { $0.status == .needsYou }
            return machine
        })
    }

    static func machines(_ machines: [MachineSection]) -> [Entry] {
        machines.map { Entry(target: .machine($0.id), label: $0.name) }
    }

    private static func entry(_ row: SessionRow) -> Entry {
        let name = row.bib ?? (row.name.isEmpty ? row.agentName : row.name)
        return Entry(
            target: .row(TabID(hostID: row.hostID, sessionID: row.id)),
            label: [name, row.agentName, StatusWord.label(row.status)].joined(separator: ", ")
        )
    }
}

enum SessionRotorCopy {
    static let sessions = String(localized: "sidebar.rotor.sessions", defaultValue: "Sessions")
    static let needsYou = String(localized: "sidebar.rotor.needsYou", defaultValue: "Needs you")
    static let machines = String(localized: "sidebar.rotor.machines", defaultValue: "Machines")
}

extension View {
    /// The "Sessions", "Needs you" and "Machines" rotors for a list of machine sections.
    func sessionRotors(_ machines: [MachineSection], requests: [RequestCard]) -> some View {
        accessibilityRotor(SessionRotorCopy.sessions) {
            ForEach(SessionRotors.sessions(machines), id: \.target) { entry in
                AccessibilityRotorEntry(entry.label, id: entry.target)
            }
        }
        .accessibilityRotor(SessionRotorCopy.needsYou) {
            ForEach(SessionRotors.needsYou(machines, requests: requests), id: \.target) { entry in
                AccessibilityRotorEntry(entry.label, id: entry.target)
            }
        }
        .accessibilityRotor(SessionRotorCopy.machines) {
            ForEach(SessionRotors.machines(machines), id: \.target) { entry in
                AccessibilityRotorEntry(entry.label, id: entry.target)
            }
        }
    }
}
