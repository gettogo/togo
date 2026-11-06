import CoreSpotlight
import Foundation
import TogoKit
import TogoPush

/// What Shortcuts, Siri and Spotlight can name without reaching a machine: the saved machines
/// and the sessions each one last reported. Names and bibs only; never addresses, folders or
/// screen text.
struct IntentSnapshot: Codable, Equatable, Sendable {
    struct Machine: Codable, Equatable, Sendable {
        var id: HostID
        var name: String
    }

    struct Session: Codable, Equatable, Sendable, Identifiable {
        var hostID: HostID
        var machine: String
        var bib: String
        var name: String

        /// A bib is unique on its machine, so the pair names one session.
        var id: String { "\(hostID.uuidString)/\(bib)" }
    }

    var machines: [Machine] = []
    var sessions: [Session] = []

    /// The saved machines, in their order. A machine that is gone takes its sessions with it;
    /// a renamed one carries its new name onto them.
    func with(machines: [Machine]) -> IntentSnapshot {
        let names = Dictionary(machines.map { ($0.id, $0.name) }, uniquingKeysWith: { first, _ in first })
        return IntentSnapshot(machines: machines, sessions: sessions.compactMap { session in
            guard let name = names[session.hostID] else { return nil }
            var session = session
            session.machine = name
            return session
        })
    }

    /// The sessions one machine reports. A machine that is out of reach and lists nothing keeps
    /// the sessions it last reported, so a sleeping laptop's sessions can still be opened by
    /// name. Sessions without a bib have no link and are left out.
    func applying(_ view: HostViewSnapshot) -> IntentSnapshot {
        guard let machine = machines.first(where: { $0.id == view.hostID }),
              view.reachability == .online || !view.sessions.isEmpty
        else { return self }
        let reported = view.sessions.compactMap { row -> Session? in
            guard let bib = row.bib, !bib.isEmpty else { return nil }
            return Session(hostID: machine.id, machine: machine.name, bib: bib, name: row.name)
        }
        var next = self
        next.sessions = machines.flatMap { other in
            other.id == machine.id ? reported : sessions.filter { $0.hostID == other.id }
        }
        return next
    }
}

/// The snapshot as a file in the app group, readable after first unlock like the pending
/// requests the waiting widget counts.
struct IntentSnapshotFile: Sendable {
    let url: URL

    static var shared: IntentSnapshotFile? {
        FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: SharedGroup.ios).map {
            IntentSnapshotFile(url: $0.appending(path: "Library/Application Support/Shortcuts/snapshot.json"))
        }
    }

    func read() -> IntentSnapshot {
        guard let data = try? Data(contentsOf: url) else { return IntentSnapshot() }
        return (try? JSONDecoder().decode(IntentSnapshot.self, from: data)) ?? IntentSnapshot()
    }

    func write(_ snapshot: IntentSnapshot) {
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        guard let data = try? JSONEncoder().encode(snapshot) else { return }
        try? data.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }
}

/// Where intents read what the app last knew, and how they hand the app a link to open. Every
/// read is a local file or memory; nothing here reaches a machine. Links go wherever the app
/// sends any other `togo://` link.
@MainActor final class IntentHub {
    static var current = IntentHub.local()

    let snapshot: () -> IntentSnapshot
    let requests: () -> [PendingRequest]
    let presets: () async -> [AgentPreset]
    let clock: () -> Date
    private var opener: ((DeepLink) -> Void)?
    private var queued: [DeepLink] = []

    init(
        snapshot: @escaping () -> IntentSnapshot, requests: @escaping () -> [PendingRequest],
        presets: @escaping () async -> [AgentPreset], clock: @escaping () -> Date
    ) {
        self.snapshot = snapshot
        self.requests = requests
        self.presets = presets
        self.clock = clock
    }

    /// Reads the app group's files: the snapshot the app keeps and the requests the waiting
    /// widget counts.
    static func local(presets: @escaping () async -> [AgentPreset] = { [] }) -> IntentHub {
        let file = IntentSnapshotFile.shared
        let directory = SharedGroup.storeDirectory(SharedGroup.ios)
        return IntentHub(
            snapshot: { file?.read() ?? IntentSnapshot() },
            requests: { directory.map { PendingStore(directory: $0, clock: { .now }).all() } ?? [] },
            presets: presets, clock: { .now }
        )
    }

    /// Hands links to the app, first any that arrived before it was ready.
    func connect(_ opener: @escaping (DeepLink) -> Void) {
        self.opener = opener
        let waiting = queued
        queued = []
        waiting.forEach(opener)
    }

    func open(_ link: DeepLink) {
        if let opener { opener(link) } else { queued.append(link) }
    }
}

/// Spotlight's copy of the snapshot: machine names, session names and bibs.
enum IntentIndex {
    /// Brings Spotlight from `old` to `new`. With no `old`, as on the first write after a
    /// launch, everything is cleared and indexed again, so an earlier failure cannot leave a
    /// forgotten machine behind. Returns false when Spotlight refused any step.
    static func sync(from old: IntentSnapshot?, to new: IntentSnapshot) async -> Bool {
        let index = CSSearchableIndex.default()
        let machines = new.machines.map(MachineEntity.init)
        let sessions = new.sessions.map(SessionEntity.init)
        do {
            if let old {
                let goneMachines = Set(old.machines.map(\.id)).subtracting(machines.map(\.id))
                let goneSessions = Set(old.sessions.map(\.id)).subtracting(sessions.map(\.id))
                try await index.deleteAppEntities(identifiedBy: Array(goneMachines), ofType: MachineEntity.self)
                try await index.deleteAppEntities(identifiedBy: Array(goneSessions), ofType: SessionEntity.self)
            } else {
                // Togo puts nothing else in Spotlight.
                try await index.deleteAllSearchableItems()
            }
            try await index.indexAppEntities(machines)
            try await index.indexAppEntities(sessions)
            return true
        } catch {
            return false
        }
    }
}
