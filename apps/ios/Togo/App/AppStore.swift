import Foundation
import Observation
import TogoKit

@MainActor @Observable final class AppStore {
    /// A finished session stays with the live ones this long before it moves to the archive.
    static let archiveAfter: TimeInterval = 6 * 60 * 60

    var controlCenter: ControlCenterState
    var workspace = WorkspaceState()
    var query = ""
    var filter: SessionFilter = .all
    var path: [AppRoute] = []
    var sheet: AppSheet?
    var workspaceOpen = false
    var firstRun: Bool
    var notice: Notice?
    /// The tab whose compose bar is open, if any.
    var composing: TabID?
    /// An image pasted into a terminal, waiting for that tab's compose bar to take it.
    var pastedImage: PastedImage?
    /// A session link waiting for its machine to report.
    var pendingLink: DeepLink?
    /// Bumped by ⌘F; the workspace opens its find bar when it changes.
    var findRequests = 0
    /// Bumped when a stage, unstage, discard or commit in the review goes through, so the
    /// workspace header counts the changes again.
    var reviewWrites = 0
    /// Why the open review can no longer change anything: its session or machine went while a
    /// commit message was being written, so it stays open, read-only, to keep the message.
    var reviewEnded: String?
    /// The open review holds a commit message the user has typed.
    var reviewHasDraft = false
    /// Compose text per tab. Cleared when it is sent and when the tab closes.
    var drafts: [TabID: String] = [:]
    /// The board of every session, shown over the workspace.
    var boardShown = false
    /// The workspace with its chrome hidden.
    var zen = false
    /// Both panes are on screen. The workspace sets it from its arrangement; while false the
    /// secondary tab waits in state and everything acts on the primary pane.
    var panesVisible = false
    /// Open tabs, most recently put on screen first.
    var recentTabs: [TabID] = []
    /// The workspace sits beside the session sidebar instead of in a cover over the list.
    var workspaceInline = false
    /// The window is wide enough for two panes, so ⌘\ can open one.
    var panesFit = false
    /// The session sidebar of a regular-width window.
    var sidebar = SidebarState()
    /// Why the last answer to a card did not arrive, by card id. It sits on the card the user
    /// will answer again rather than in `notice`, which belongs to the whole screen.
    private(set) var answerFailures: [String: String] = [:]
    /// Machines removed while their monitor may still have a snapshot on the way. A snapshot
    /// for one is dropped unless the machine is back in the services' list.
    @ObservationIgnored var removedMachines: Set<HostID> = []
    @ObservationIgnored private var isRunning = false
    let services: any AppServices
    /// What the review screens read and write. Previews and tests pass fixtures; the app passes
    /// the live services once they exist.
    let reviewServices: any ReviewServices

    init(
        services: any AppServices, controlCenter: ControlCenterState = .init(), firstRun: Bool = false,
        reviewServices: (any ReviewServices)? = nil
    ) {
        self.services = services
        self.reviewServices = reviewServices ?? Self.defaultReviewServices()
        self.controlCenter = controlCenter
        self.firstRun = firstRun
    }

    /// Applies every snapshot the services publish until the stream ends or the task is
    /// cancelled. Each window's store runs its own loop; a second call on the same store while
    /// the first is running returns at once.
    func run() async {
        guard !isRunning else { return }
        isRunning = true
        defer { isRunning = false }
        for await snapshot in services.snapshots() {
            apply(snapshot)
        }
    }

    // MARK: Control center

    var visibleMachines: [MachineSection] {
        let needle = query.trimmingCharacters(in: .whitespacesAndNewlines)
        let narrowed = !needle.isEmpty || filter != .all
        return controlCenter.machines.compactMap { machine in
            var machine = machine
            machine.sessions = machine.sessions.filter { row in
                filter.admits(row.status) && (needle.isEmpty || Self.matches(row, needle))
            }
            return narrowed && machine.sessions.isEmpty ? nil : machine
        }
    }

    func apply(_ snapshot: HostViewSnapshot) {
        if removedMachines.contains(snapshot.hostID) {
            guard services.hosts.contains(where: { $0.id == snapshot.hostID }) else { return }
            removedMachines.remove(snapshot.hostID)
        }
        let (live, archived) = Self.partition(snapshot.sessions, at: snapshot.at)
        var section = controlCenter.machines.first { $0.id == snapshot.hostID } ?? newSection(snapshot.hostID)
        section.reachability = snapshot.reachability
        section.helper = snapshot.helper
        section.capabilities = snapshot.capabilities
        section.sessions = live.sorted(by: Self.bibOrder)
        if snapshot.reachability == .online {
            section.lastSeen = snapshot.at
        }

        if let index = controlCenter.machines.firstIndex(where: { $0.id == snapshot.hostID }) {
            controlCenter.machines[index] = section
        } else {
            controlCenter.machines.append(section)
        }
        controlCenter.archived = (controlCenter.archived.filter { $0.hostID != snapshot.hostID } + archived)
            .sorted { $0.statusSince > $1.statusSince }
        switch snapshot.reachability {
        case .online:
            closeEndedTabs(on: snapshot.hostID, listed: Set(snapshot.sessions.map(\.id)))
        case .failed:
            // A helper being installed or updated drops the connection until it is back, which
            // the monitor reports as a failure too; the review waits for it.
            switch snapshot.helper {
            case .missing, .outdated: break
            case .checking, .present: endReview(hostID: snapshot.hostID, sessionID: nil, why: ReviewEntryCopy.machineLost(machineName(for: snapshot.hostID)))
            }
        case .keyMissing:
            endReview(hostID: snapshot.hostID, sessionID: nil, why: ReviewEntryCopy.machineLost(machineName(for: snapshot.hostID)))
        case .connecting, .waking, .offline, .authNeeded:
            break
        }
        controlCenter.requests = mergedRequests(snapshot)
        let cardIDs = Set(controlCenter.requests.map(\.id))
        answerFailures = answerFailures.filter { cardIDs.contains($0.key) }
        controlCenter.loaded = true
        resolvePendingLink()
    }

    func answer(_ cardID: String, _ decision: Decision) async {
        guard let card = controlCenter.requests.first(where: { $0.id == cardID }), card.phase == .open else { return }
        answerFailures[cardID] = nil
        setPhase(.sending(decision), of: cardID)
        do {
            try await services.answer(hostID: card.hostID, requestID: cardID, decision: decision)
            setPhase(.done(Self.outcome(of: decision, on: card)), of: cardID)
            TutorialTip.request.done()
        } catch where ServiceErrors.isAlreadyAnswered(error) {
            setPhase(.done(.answeredElsewhere), of: cardID)
        } catch {
            setPhase(.open, of: cardID)
            answerFailures[cardID] = ControlCenterCopy.answerFailed(hostName(card.hostID))
        }
    }

    /// Takes away answered cards once the list has shown their outcome. Open cards stay.
    func dismissAnswered(_ ids: Set<String>) {
        controlCenter.requests.removeAll { card in
            guard ids.contains(card.id), case .done = card.phase else { return false }
            return true
        }
    }

    /// A numbered option counts as a denial when the agent marked it as one, so "No" reads "Denied".
    static func outcome(of decision: Decision, on card: RequestCard) -> RequestCard.Outcome {
        switch decision {
        case .deny: .denied
        case .option(let n): card.options.first { $0.n == n }?.role == .deny ? .denied : .approved
        case .approve, .text: .approved
        }
    }

    func kill(_ row: SessionRow) async {
        do {
            try await services.kill(hostID: row.hostID, sessionID: row.id)
            closeEndedTab(TabID(hostID: row.hostID, sessionID: row.id))
        } catch {
            notice = Notice(text: "Couldn't end \(Self.label(row)): \(ServiceErrors.message(error))", isError: true)
        }
    }

    func rename(_ row: SessionRow, to name: String) async {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed != row.name else { return }
        update(row) { $0.name = trimmed }
        do {
            try await services.rename(hostID: row.hostID, sessionID: row.id, name: trimmed)
        } catch {
            update(row) { $0.name = row.name }
            notice = Notice(text: "Couldn't rename \(Self.label(row)): \(ServiceErrors.message(error))", isError: true)
        }
    }

    func setMute(_ row: SessionRow, _ muted: Bool) async {
        update(row) { $0.muted = muted }
        do {
            try await services.setMute(hostID: row.hostID, sessionID: row.id, muted: muted)
        } catch {
            update(row) { $0.muted = row.muted }
            notice = Notice(text: "Couldn't change alerts for \(Self.label(row)).", isError: true)
        }
    }

    // MARK: Workspace

    func open(hostID: HostID, sessionID: String, transport: TransportPreference? = nil) {
        let id = TabID(hostID: hostID, sessionID: sessionID)
        if let index = workspace.tabs.firstIndex(where: { $0.id == id }) {
            if let transport {
                workspace.tabs[index].transport = transport
            }
        } else {
            let row = session(hostID: hostID, id: sessionID)
            workspace.tabs.append(WorkspaceTab(
                hostID: hostID, sessionID: sessionID, bib: row?.bib,
                title: row.map(\.name) ?? sessionID, link: .connecting, transport: transport
            ))
        }
        if id == workspace.secondary {
            workspace.secondary = workspace.selected
        }
        workspace.selected = id
        touch(id)
        workspaceOpen = true
    }

    func close(_ id: TabID) {
        guard let index = workspace.tabs.firstIndex(where: { $0.id == id }) else { return }
        workspace.tabs.remove(at: index)
        releasePanes(closing: id)
        if workspace.selected == id {
            let neighbour = min(index, workspace.tabs.count - 1)
            workspace.selected = neighbour >= 0 ? workspace.tabs[neighbour].id : nil
        }
        if workspace.tabs.isEmpty {
            workspaceOpen = false
        }
    }

    /// Shows a tab in the primary pane. A tab already beside it trades places with the primary.
    func select(_ id: TabID) {
        guard workspace.tabs.contains(where: { $0.id == id }) else { return }
        place(id, in: .primary)
    }

    private static func defaultReviewServices() -> any ReviewServices {
        #if DEBUG
        FakeReviewServices()
        #else
        NoReviewServices()
        #endif
    }

    // MARK: Private

    private static func matches(_ row: SessionRow, _ needle: String) -> Bool {
        [row.name, row.agent, row.agentName, row.cwd, row.bib ?? ""]
            .contains { $0.localizedStandardContains(needle) }
    }

    private static func partition(_ rows: [SessionRow], at now: Date) -> (live: [SessionRow], archived: [SessionRow]) {
        var live: [SessionRow] = []
        var archived: [SessionRow] = []
        for row in rows {
            let finished = row.status == .done || row.status == .exited
            if finished && now.timeIntervalSince(row.statusSince) > archiveAfter {
                archived.append(row)
            } else {
                live.append(row)
            }
        }
        return (live, archived)
    }

    /// Bib order never depends on status, so a row the user is about to tap never moves.
    private static func bibOrder(_ a: SessionRow, _ b: SessionRow) -> Bool {
        switch (a.bib.flatMap { Int($0) }, b.bib.flatMap { Int($0) }) {
        case let (x?, y?) where x != y: return x < y
        case (_?, nil): return true
        case (nil, _?): return false
        default:
            let byName = a.name.localizedStandardCompare(b.name)
            return byName == .orderedSame ? a.id < b.id : byName == .orderedAscending
        }
    }

    /// Cards keep the order they arrived in, which is oldest first. A card the user is answering
    /// keeps its phase even if the snapshot still lists it as open. An answered card stays until
    /// the list collapses it, and an open one the machine closed turns into its outcome first.
    private func mergedRequests(_ snapshot: HostViewSnapshot) -> [RequestCard] {
        let incoming = Dictionary(snapshot.requests.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        var merged: [RequestCard] = []
        for var card in controlCenter.requests {
            if card.hostID != snapshot.hostID {
                merged.append(card)
            } else if var fresh = incoming[card.id] {
                fresh.phase = card.phase
                merged.append(fresh)
            } else {
                switch card.phase {
                case .sending, .done:
                    merged.append(card)
                case .open:
                    guard let outcome = snapshot.closed[card.id] else { continue }
                    card.phase = .done(outcome)
                    merged.append(card)
                }
            }
        }
        let known = Set(merged.map(\.id))
        merged += snapshot.requests.filter { !known.contains($0.id) }
        return merged
    }

    private func newSection(_ id: HostID) -> MachineSection {
        let host = services.hosts.first { $0.id == id }
        return MachineSection(
            id: id, name: host?.name ?? "Machine", symbol: host?.symbol ?? .laptop,
            reachability: .connecting, helper: .checking, sessions: [], lastSeen: nil
        )
    }

    private func hostName(_ id: HostID) -> String {
        controlCenter.machines.first { $0.id == id }?.name ?? services.hosts.first { $0.id == id }?.name ?? "the machine"
    }

    private func session(hostID: HostID, id: String) -> SessionRow? {
        controlCenter.machines.first { $0.id == hostID }?.sessions.first { $0.id == id }
            ?? controlCenter.archived.first { $0.hostID == hostID && $0.id == id }
    }

    private func setPhase(_ phase: RequestCard.Phase, of cardID: String) {
        guard let index = controlCenter.requests.firstIndex(where: { $0.id == cardID }) else { return }
        controlCenter.requests[index].phase = phase
    }

    private func update(_ row: SessionRow, _ change: (inout SessionRow) -> Void) {
        guard let m = controlCenter.machines.firstIndex(where: { $0.id == row.hostID }),
              let s = controlCenter.machines[m].sessions.firstIndex(where: { $0.id == row.id }) else { return }
        change(&controlCenter.machines[m].sessions[s])
    }

    private static func label(_ row: SessionRow) -> String {
        row.bib ?? row.name
    }
}
