import SwiftUI
import TogoKit
import UIKit

/// The home screen: requests waiting on the user, then every machine's sessions, then the
/// archive. Each machine section explains itself when it cannot show live rows.
struct ControlCenterView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Bindable var store: AppStore
    /// Shared with whoever presents the new session sheet and the workspace, so they can zoom
    /// out of the plus button and the tapped row.
    var zoom: Namespace.ID?
    /// A fixed clock for previews and snapshots; nil follows the time of day.
    var now: Date?

    @State private var killTarget: SessionRow?
    @State private var renameTarget: SessionRow?
    @State private var renameText = ""

    static let newSessionSourceID = "newSession"
    /// How long an answered card shows its outcome before it collapses.
    static let outcomeHold = Duration.milliseconds(400)

    init(store: AppStore, zoom: Namespace.ID? = nil, now: Date? = nil) {
        self.store = store
        self.zoom = zoom
        self.now = now
    }

    var body: some View {
        content
            .background(Color.togoInk)
            .navigationTitle(ControlCenterCopy.title)
            .navigationBarTitleDisplayMode(.large)
            .toolbar { toolbar }
            .confirmationDialog(
                killTarget.map { SessionActions.killConfirmation(for: $0).title } ?? "",
                isPresented: presented($killTarget), titleVisibility: .visible, presenting: killTarget
            ) { row in
                Button(ControlCenterCopy.kill, role: .destructive) { Task { await store.kill(row) } }
            } message: { row in
                Text(SessionActions.killConfirmation(for: row).message)
            }
            .alert(
                renameTarget.map { ControlCenterCopy.renameTitle(SessionActions.label($0)) } ?? "",
                isPresented: presented($renameTarget), presenting: renameTarget
            ) { row in
                TextField(ControlCenterCopy.name, text: $renameText)
                Button(ControlCenterCopy.rename) { Task { await store.rename(row, to: renameText) } }
                Button(ControlCenterCopy.cancel, role: .cancel) {}
            }
    }

    @ViewBuilder private var content: some View {
        if store.services.hosts.isEmpty && store.controlCenter.machines.isEmpty {
            firstRun
        } else {
            TimelineView(.everyMinute) { context in
                list(now: now ?? context.date)
            }
        }
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .topBarTrailing) {
            Button(ControlCenterCopy.settings, systemImage: "gearshape") { store.path.append(.settings) }
                .foregroundStyle(Color.togoSnow)
        }
        if !store.services.hosts.isEmpty {
            ToolbarSpacer(.fixed, placement: .topBarTrailing)
            ToolbarItem(placement: .topBarTrailing) {
                Button(ControlCenterCopy.newSession, systemImage: "plus") { store.sheet = .newSession(nil) }
                    .foregroundStyle(Color.togoSnow)
                    .modifier(ZoomSource(id: Self.newSessionSourceID, namespace: zoom))
            }
        }
    }

    // MARK: First run

    private var firstRun: some View {
        VStack(spacing: 4) {
            StateView(
                kind: .empty, title: ControlCenterCopy.firstRunTitle, message: ControlCenterCopy.firstRunMessage,
                action: .init(ControlCenterCopy.addMachine) { store.sheet = .addMachine }
            )
            .fixedSize(horizontal: false, vertical: true)
            Button(ControlCenterCopy.withoutHelper) { store.sheet = .addMachine }
                .buttonStyle(.secondary(.fit))
                .multilineTextAlignment(.center)
                .padding(.horizontal, Layout.margin)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: List

    private func list(now: Date) -> some View {
        List {
            Section {
                filterPicker
            }
            if let notice = store.notice {
                Section {
                    Label(notice.text, systemImage: notice.isError ? "exclamationmark.circle" : "info.circle")
                        .font(.subheadline)
                        .foregroundStyle(Color.togoSnow)
                        .listRowBackground(Color.togoInk)
                }
            }
            requests
            ForEach(machines) { machine in
                MachineSectionView(
                    machine: machine, etAvailable: etAvailable(machine.id), now: now, narrowed: narrowed, zoom: zoom,
                    perform: perform
                )
            }
            emptyResult
            archive
        }
        .listStyle(.insetGrouped)
        .listRowSeparatorTint(Color.togoLine)
        .listSectionSpacing(.compact)
        .scrollContentBackground(.hidden)
        .searchable(text: $store.query, placement: .navigationBarDrawer, prompt: ControlCenterCopy.searchPrompt)
        .refreshable { await store.services.refresh() }
        .motion(Motion.dropIn, value: visibleRequests.map(\.id))
        .task(id: answeredCards) { await collapse(answeredCards) }
    }

    /// Segments cannot grow with the text, so at accessibility sizes the filter becomes a menu.
    @ViewBuilder private var filterPicker: some View {
        let picker = Picker(ControlCenterCopy.filter, selection: $store.filter) {
            ForEach(SessionFilter.allCases, id: \.self) { filter in
                Text(ControlCenterCopy.filterName(filter)).tag(filter)
            }
        }
        if dynamicTypeSize.isAccessibilitySize {
            picker
                .pickerStyle(.menu)
                .foregroundStyle(Color.togoSnow)
                .listRowBackground(Color.togoInk)
        } else {
            picker
                .pickerStyle(.segmented)
                .listRowInsets(EdgeInsets())
                .listRowBackground(Color.clear)
        }
    }

    @ViewBuilder private var requests: some View {
        if !visibleRequests.isEmpty {
            Section {
                ForEach(visibleRequests) { card in
                    RequestCardView(
                        card: card, machineName: machineName(card.hostID), failure: store.answerFailures[card.id],
                        onOpen: card.sessionID.map { sessionID in
                            { store.open(hostID: card.hostID, sessionID: sessionID) }
                        }
                    ) { answer(card, $0) }
                    .listRowInsets(EdgeInsets(top: 6, leading: 0, bottom: 6, trailing: 0))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
                }
            } header: {
                SectionHeading(ControlCenterCopy.waitingOnYou)
                    .textCase(nil)
            }
        }
    }

    @ViewBuilder private var emptyResult: some View {
        if narrowed && machines.isEmpty && visibleRequests.isEmpty {
            let query = store.query.trimmingCharacters(in: .whitespacesAndNewlines)
            Section {
                Text(query.isEmpty ? ControlCenterCopy.filterEmpty(store.filter) : ControlCenterCopy.noMatches(query))
                    .font(.body)
                    .foregroundStyle(Color.togoMute)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 32)
                    .listRowBackground(Color.clear)
            }
        }
    }

    @ViewBuilder private var archive: some View {
        let archived = store.controlCenter.archived
        if !narrowed && !archived.isEmpty {
            Section {
                NavigationLink {
                    ArchiveView(rows: archived, machines: machines, perform: perform)
                } label: {
                    LabeledContent {
                        Text(ControlCenterCopy.finishedSessions(archived.count))
                            .foregroundStyle(Color.togoMute)
                    } label: {
                        Text(ControlCenterCopy.archive)
                            .foregroundStyle(Color.togoSnow)
                    }
                    .frame(minHeight: 44)
                }
                .listRowBackground(Color.togoInk)
            }
        }
    }

    // MARK: Derived state

    private var narrowed: Bool {
        !store.query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || store.filter != .all
    }

    /// Machines in the order the user added them. One that has not reported yet shows as
    /// connecting, so the list has its final shape from the first frame.
    private var machines: [MachineSection] {
        let hosts = store.services.hosts
        var sections = store.visibleMachines
        if !narrowed {
            for host in hosts where !sections.contains(where: { $0.id == host.id }) {
                sections.append(MachineSection(
                    id: host.id, name: host.name, symbol: host.symbol, reachability: .connecting,
                    helper: .checking, sessions: [], lastSeen: nil
                ))
            }
        }
        let order = Dictionary(hosts.enumerated().map { ($1.id, $0) }, uniquingKeysWith: { first, _ in first })
        return sections.sorted { (order[$0.id] ?? .max) < (order[$1.id] ?? .max) }
    }

    /// Requests are what "Needs you" means, so the other filters hide them.
    private var visibleRequests: [RequestCard] {
        guard store.filter == .all || store.filter == .needsYou else { return [] }
        let needle = store.query.trimmingCharacters(in: .whitespacesAndNewlines)
        return store.controlCenter.requests.filter { card in
            needle.isEmpty || [card.title, card.agentName, card.bib ?? ""]
                .contains { $0.localizedStandardContains(needle) }
        }
    }

    private var answeredCards: Set<String> {
        Set(store.controlCenter.requests.filter { card in
            if case .done = card.phase { true } else { false }
        }.map(\.id))
    }

    private func machineName(_ id: HostID) -> String {
        store.controlCenter.machines.first { $0.id == id }?.name ?? store.services.hosts.first { $0.id == id }?.name ?? ""
    }

    private func etAvailable(_ id: HostID) -> Bool {
        store.services.hosts.first { $0.id == id }?.etAvailable ?? false
    }

    // MARK: Actions

    private func perform(_ command: SessionCommand) {
        switch command {
        case .open(let row, let transport):
            store.open(hostID: row.hostID, sessionID: row.id, transport: transport)
        case .rename(let row):
            renameText = row.name
            renameTarget = row
        case .toggleMute(let row):
            Task { await store.setMute(row, !row.muted) }
        case .copyPath(let row):
            UIPasteboard.general.string = row.cwd
        case .kill(let row):
            killTarget = row
        case .review(let row, let tab):
            store.review(row, tab: tab)
        case .newSession(let hostID):
            store.sheet = .newSession(hostID)
        case .retry:
            TutorialTip.offline.done()
            Task { await store.services.refresh() }
        case .signIn(let hostID):
            store.sheet = .signIn(hostID)
        case .installHelper(let hostID), .edit(let hostID):
            store.sheet = .editMachine(hostID)
        }
    }

    private func answer(_ card: RequestCard, _ decision: Decision) {
        Task { await store.answer(card.id, decision) }
    }

    private func collapse(_ cards: Set<String>) async {
        guard !cards.isEmpty else { return }
        try? await Task.sleep(for: Self.outcomeHold)
        guard !Task.isCancelled else { return }
        withAnimation(Motion.motion(Motion.collapse, reduce: reduceMotion)) {
            store.dismissAnswered(cards)
        }
    }

    private func presented<T>(_ target: Binding<T?>) -> Binding<Bool> {
        Binding { target.wrappedValue != nil } set: { if !$0 { target.wrappedValue = nil } }
    }
}

#if DEBUG
#Preview("Sessions") {
    NavigationStack {
        ControlCenterView(store: AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter), now: Samples.now)
    }
}

#Preview("First run") {
    NavigationStack {
        ControlCenterView(store: AppStore(services: FakeAppServices(hosts: [])))
    }
}
#endif
