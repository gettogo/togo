import Foundation
import TogoKit

/// What the roster shows, worked out from the store in one place so the lines, the headline and
/// the tally always agree: machines in the order the user added them, each machine's sessions in
/// bib order, and a session that waits on an answer marked as needing you.
struct RosterModel: Equatable {
    struct Group: Identifiable, Equatable {
        var id: HostID { machine.id }
        var machine: MachineSection
        var lines: [Line]

        var name: String { machine.name }
        var count: Int { lines.count }
    }

    struct Line: Identifiable, Equatable {
        var id: TabID { TabID(hostID: row.hostID, sessionID: row.id) }
        var row: SessionRow
        /// A request from this session waits for an answer, or the agent says it needs the user.
        var needsYou: Bool
        /// The session's mission summary where a harness serves it: agents, background jobs,
        /// plan progress and what needs the user.
        var mission: MissionSummary? = nil

        /// The status the line shows: "Needs you" wins over whatever the agent last reported.
        var status: SessionStatus { needsYou ? .needsYou : row.status }
    }

    /// How many sessions are in each state, sessions that need the user left out.
    struct Tally: Equatable {
        var working = 0
        var idle = 0
        var done = 0
    }

    var groups: [Group]
    /// Sessions that need the user, plus requests that belong to no listed session.
    var waiting: Int
    var tally: Tally

    /// This device has no network: the roster says so once, above every machine.
    var deviceOffline: Bool {
        groups.contains { $0.machine.reachability == .deviceOffline }
    }

    static let empty = RosterModel(groups: [], waiting: 0, tally: Tally())

    /// `requests` count while they are open or on their way; an answered one has left the bar.
    /// `missions` holds the sessions' mission summaries; one with something that needs the user
    /// marks its line as needing you, so the headline counts it.
    static func make(
        machines: [MachineSection], requests: [RequestCard], missions: [TabID: MissionSummary] = [:],
        opening: Set<TabID> = [], openingUnlisted: Int = 0
    ) -> RosterModel {
        let pending = requests.filter(\.isPending)
        // A page an agent asked to open, not yet used, needs the user like a request does.
        let asking = Set(pending.compactMap(\.tabID)).union(opening)
        var tally = Tally()
        var needing = Set<TabID>()
        let groups = machines.map { machine in
            Group(machine: machine, lines: machine.sessions.map { row in
                let id = TabID(hostID: row.hostID, sessionID: row.id)
                // An open tab's store is live; the snapshot covers every other session.
                let mission = missions[id] ?? row.mission
                let asks = (mission?.needsYou ?? 0) > 0
                let line = Line(
                    row: row, needsYou: asking.contains(id) || row.status == .needsYou || asks, mission: mission
                )
                if line.needsYou {
                    needing.insert(id)
                } else {
                    tally.count(row.status)
                }
                return line
            })
        }
        let unlisted = Set(pending.filter { card in card.tabID.map { !needing.contains($0) } ?? true }.map(\.id))
        return RosterModel(groups: groups, waiting: needing.count + unlisted.count + openingUnlisted, tally: tally)
    }

    var lines: [Line] { groups.flatMap(\.lines) }

    /// "1 needs you", "3 need you", or "All clear".
    var headline: String { RosterCopy.headline(waiting: waiting) }

    /// "2 working  1 idle  1 done", states with no sessions left out. Empty with no sessions.
    var tallyText: String {
        [
            (tally.working, RosterCopy.working), (tally.idle, RosterCopy.idle), (tally.done, RosterCopy.done),
        ]
        .filter { $0.0 > 0 }
        .map { "\($0.0) \($0.1)" }
        .joined(separator: "  ")
    }

    /// The line drawn as selected: only beside a terminal, and only while its line is listed.
    static func selection(focused: TabID?, wide: Bool, in model: RosterModel) -> TabID? {
        guard wide, let focused, model.lines.contains(where: { $0.id == focused }) else { return nil }
        return focused
    }
}

extension RosterModel.Tally {
    mutating func count(_ status: SessionStatus) {
        switch status {
        case .working: working += 1
        case .done, .exited, .ended, .failed: done += 1
        case .needsYou, .idle, .unknown: idle += 1
        }
    }
}

extension RequestCard {
    /// Still waiting on the user: open, or an answer on its way that may yet fail.
    var isPending: Bool {
        if case .done = phase { false } else { true }
    }
}

extension AppStore {
    /// The roster as this window shows it.
    var roster: RosterModel {
        let opens = LinkCenter.shared.requests
        return RosterModel.make(
            machines: listMachines, requests: controlCenter.requests, missions: missions,
            opening: opens.waitingTabs, openingUnlisted: opens.unlisted
        )
    }

    /// Mission summaries of the sessions this window has open, read live from their stores.
    var missions: [TabID: MissionSummary] {
        var out: [TabID: MissionSummary] = [:]
        for (id, store) in harnessTabs.stores where store.mission.mission != nil {
            out[id] = store.mission.summary
        }
        return out
    }
}

/// Every sentence the roster says, keyed under `roster.` in the string catalog.
enum RosterCopy {
    static func headline(waiting: Int) -> String {
        switch waiting {
        case 0: String(localized: "roster.allClear", defaultValue: "All clear")
        case 1: String(localized: "roster.needsYou.one", defaultValue: "1 needs you")
        default: String(localized: "roster.needsYou.many", defaultValue: "\(waiting) need you")
        }
    }

    static let working = String(localized: "roster.tally.working", defaultValue: "working")
    static let idle = String(localized: "roster.tally.idle", defaultValue: "idle")
    static let done = String(localized: "roster.tally.done", defaultValue: "done")

    static let more = String(localized: "roster.more", defaultValue: "More")
    static let newSession = String(localized: "roster.newSession", defaultValue: "New session")
    static let show = String(localized: "roster.show.filter", defaultValue: "Show")
    static let searchPrompt = String(localized: "roster.search", defaultValue: "Search")
    static let clearSearch = String(localized: "roster.search.clear", defaultValue: "Clear search")
    static let settings = String(localized: "roster.settings", defaultValue: "Settings")
    static let addMachine = String(localized: "roster.addMachine", defaultValue: "Add machine")
    static let archive = String(localized: "roster.archive", defaultValue: "Archive")
    static let noSessions = String(localized: "roster.noSessions", defaultValue: "No sessions")

    static let noMachinesTitle = String(localized: "roster.noMachines.title", defaultValue: "No machines yet")
    static let noMachinesAction = String(localized: "roster.noMachines.action", defaultValue: "Add a machine")

    static let detailClear = String(localized: "roster.detail.clear", defaultValue: "All clear")
    static let detailWaiting = String(localized: "roster.detail.waiting", defaultValue: "Something needs you")
    static let detailMessage = String(
        localized: "roster.detail.message", defaultValue: "Pick a line to open its terminal."
    )
    static let showRoster = String(localized: "roster.show", defaultValue: "Show sessions")

    static func machineCount(_ name: String, _ count: Int) -> String {
        String(localized: "roster.machine.spoken", defaultValue: "\(name), \(count) sessions")
    }
}
