import Foundation

/// What a window shows, kept by the system with the window so it comes back the same after
/// the app was closed: its tabs, which of them are on screen, whether the workspace is up, and
/// the rest of its place in the navigation tree (`Router`).
struct WindowRestoration: Codable, Equatable {
    var tabs: [TabID]
    var selected: TabID?
    var secondary: TabID?
    var workspaceOpen: Bool
    /// What each tab was last called, so a tab put back while its machine is away still shows
    /// its number, name and agent rather than its id. Absent in data from older versions.
    var labels: [TabLabel]? = nil
    /// Where the window was in the navigation tree: what was pushed and the sidebar choice.
    /// Absent in data from older versions.
    var router: Router? = nil

    private enum CodingKeys: String, CodingKey {
        case tabs, selected, secondary, workspaceOpen, labels, router
    }
}

extension WindowRestoration {
    /// The tabs are what matters: a label or a place in the tree that no longer decodes is
    /// dropped rather than costing the window its tabs.
    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        tabs = try c.decode([TabID].self, forKey: .tabs)
        selected = try? c.decodeIfPresent(TabID.self, forKey: .selected)
        secondary = try? c.decodeIfPresent(TabID.self, forKey: .secondary)
        workspaceOpen = (try? c.decodeIfPresent(Bool.self, forKey: .workspaceOpen)) ?? false
        labels = (try? c.decodeIfPresent([Lenient<TabLabel>].self, forKey: .labels))?.compactMap(\.value)
        router = try? c.decodeIfPresent(Router.self, forKey: .router)
    }
}

/// A tab's number, name and agent as its machine last listed them.
struct TabLabel: Codable, Equatable {
    var id: TabID
    var bib: String?
    var title: String
    var agent: String?
}

extension WindowRestoration {
    @MainActor init(_ store: AppStore) {
        let labels = store.workspace.tabs.compactMap { tab in
            tab.title == tab.sessionID && tab.bib == nil
                ? nil : TabLabel(id: tab.id, bib: tab.bib, title: tab.title, agent: tab.agent)
        }
        self.init(
            tabs: store.workspace.tabs.map(\.id), selected: store.workspace.selected,
            secondary: store.workspace.secondary, workspaceOpen: store.workspaceOpen,
            // No tab has a name worth keeping: nothing to save, as data from older versions.
            labels: labels.isEmpty ? nil : labels,
            router: store.router
        )
    }

    /// Nil for bytes from another version that no longer decode; the window starts empty.
    init?(data: Data?) {
        guard let data, let value = try? JSONDecoder().decode(Self.self, from: data) else { return nil }
        self = value
    }

    var data: Data? {
        try? JSONEncoder().encode(self)
    }
}

extension AppStore {
    /// Puts back a window's tabs. Titles and links fill in as the machines report; a session
    /// that ended while the app was closed goes with the machine's first report.
    func restore(_ saved: WindowRestoration) {
        var seen: Set<TabID> = []
        let ids = saved.tabs.filter { seen.insert($0).inserted }
        let labels = Dictionary((saved.labels ?? []).map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        workspace.tabs = ids.map { id in
            let label = labels[id]
            return WorkspaceTab(
                hostID: id.hostID, sessionID: id.sessionID, bib: label?.bib, title: label?.title ?? id.sessionID,
                link: .connecting, agent: label?.agent
            )
        }
        let selected = saved.selected.flatMap { seen.contains($0) ? $0 : nil } ?? ids.first
        workspace.selected = selected
        workspace.secondary = saved.secondary.flatMap { seen.contains($0) && $0 != selected ? $0 : nil }
        recentTabs = [selected, workspace.secondary].compactMap { $0 }
        if let place = saved.router {
            path = place.path
            sidebar.hidden = place.sidebar.hidden
        }
        workspaceOpen = saved.workspaceOpen && !ids.isEmpty
    }
}

extension AppStore {
    /// Keeps each open tab's number, name and agent as the machine lists them, so the tab
    /// still reads the same while the machine is away or after the app was closed.
    func rememberLabels(_ rows: [SessionRow], on hostID: HostID) {
        guard !rows.isEmpty else { return }
        let byID = Dictionary(rows.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        for index in workspace.tabs.indices where workspace.tabs[index].hostID == hostID {
            guard let row = byID[workspace.tabs[index].sessionID] else { continue }
            let title = row.name.isEmpty ? row.agentName : row.name
            if workspace.tabs[index].title != title { workspace.tabs[index].title = title }
            if workspace.tabs[index].bib != row.bib { workspace.tabs[index].bib = row.bib }
            if workspace.tabs[index].agent != row.agentName { workspace.tabs[index].agent = row.agentName }
        }
    }
}
