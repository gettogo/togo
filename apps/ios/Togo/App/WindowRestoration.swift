import Foundation

/// What a window shows, kept by the system with the window so it comes back the same after
/// the app was closed: its tabs, which of them are on screen, and whether the workspace is up.
struct WindowRestoration: Codable, Equatable {
    var tabs: [TabID]
    var selected: TabID?
    var secondary: TabID?
    var workspaceOpen: Bool
}

extension WindowRestoration {
    @MainActor init(_ store: AppStore) {
        self.init(
            tabs: store.workspace.tabs.map(\.id), selected: store.workspace.selected,
            secondary: store.workspace.secondary, workspaceOpen: store.workspaceOpen
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
        workspace.tabs = ids.map { id in
            WorkspaceTab(hostID: id.hostID, sessionID: id.sessionID, bib: nil, title: id.sessionID, link: .connecting)
        }
        let selected = saved.selected.flatMap { seen.contains($0) ? $0 : nil } ?? ids.first
        workspace.selected = selected
        workspace.secondary = saved.secondary.flatMap { seen.contains($0) && $0 != selected ? $0 : nil }
        recentTabs = [selected, workspace.secondary].compactMap { $0 }
        workspaceOpen = saved.workspaceOpen && !ids.isEmpty
    }
}
