import SwiftUI
import TogoKit
import UIKit

/// The home screen: requests waiting on the user, then every machine's sessions, then the
/// archive. Each machine section explains itself when it cannot show live rows.
struct ControlCenterView: View {
    @Bindable var store: AppStore
    /// Shared with whoever presents the new session sheet and the workspace, so they can zoom
    /// out of the plus button and the tapped row.
    var zoom: Namespace.ID?
    /// A fixed clock for previews and snapshots; nil follows the time of day.
    var now: Date?

    @State private var killTarget: SessionRow?
    @State private var renameTarget: SessionRow?

    static let newSessionSourceID = "newSession"

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
            .modifier(SessionDialogs(store: store, kill: $killTarget, rename: $renameTarget))
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
                SessionFilterPicker(store: store)
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
        .sessionRotors(machines, requests: visibleRequests)
        .modifier(RequestCollapse(store: store))
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
                    .id(SessionRotors.Target.card(card.id))
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
        if store.archiveShown {
            Section {
                NavigationLink(value: AppRoute.archive) {
                    ArchiveRow(count: store.controlCenter.archived.count)
                }
                .listRowBackground(Color.togoInk)
            }
        }
    }

    // MARK: Derived state

    private var narrowed: Bool { store.listNarrowed }
    private var machines: [MachineSection] { store.listMachines }
    private var visibleRequests: [RequestCard] { store.listRequests }

    private func machineName(_ id: HostID) -> String {
        store.machineName(for: id)
    }

    private func etAvailable(_ id: HostID) -> Bool {
        store.etAvailable(id)
    }

    // MARK: Actions

    private func perform(_ command: SessionCommand) {
        store.run(
            command, open: { row, transport in store.open(hostID: row.hostID, sessionID: row.id, transport: transport) },
            kill: { killTarget = $0 }, rename: { renameTarget = $0 }
        )
    }

    private func answer(_ card: RequestCard, _ decision: Decision) {
        Task { await store.answer(card.id, decision) }
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
