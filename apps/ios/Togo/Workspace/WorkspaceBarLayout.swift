import SwiftUI
import TogoKit

/// Which top bar the workspace has, from the size classes and whether the system drew the bar
/// vertically; never from the device.
///
/// A horizontal bar in a compact window (an iPhone either way up, an iPad in a narrow window)
/// keeps three session items: Changes, the layer switch and the session menu. The open tabs move
/// from the rail into the title's menu and mission control into the session menu. A Duo's
/// vertical bar, folded or unfolded, and every regular-width bar keep the rail and every item
/// (workspace-chat.md).
enum WorkspaceBarLayout: Equatable {
    /// Title switcher; Changes, Terminal, session menu.
    case compact
    /// Tab rail under the bar; mission control and Preview in the bar.
    case full

    static func choose(
        horizontal: UserInterfaceSizeClass?, vertical: UserInterfaceSizeClass?, verticalBar: Bool
    ) -> WorkspaceBarLayout {
        // The folded Duo is compact too, but its vertical bar has room for every item.
        if verticalBar { return .full }
        return horizontal == .regular && vertical != .compact ? .full : .compact
    }

    /// The numbered chips under the bar.
    var showsRail: Bool { self == .full }
    /// The open tabs and All sessions in the title's menu.
    var titleMenu: Bool { self == .compact }
    /// Mission control and Preview as bar items; else in the session menu.
    var sessionItemsInBar: Bool { self == .full }
}

extension EnvironmentValues {
    /// Whether the bar is a Duo's vertical bar, for hosts the system cannot tell (snapshot
    /// tests). Nil reads the system's `toolbarVerticalEdge`.
    @Entry var barIsVerticalOverride: Bool?
}

/// Hands its content whether the system drew the bar vertically, read in the same pass so no
/// layout shows for a frame before the right one.
struct BarEdgeReader<Content: View>: View {
    @Environment(\.barIsVerticalOverride) private var override
    @ViewBuilder let content: (Bool) -> Content

    var body: some View {
        if let override {
            content(override)
        } else {
            system
        }
    }

    @ViewBuilder private var system: some View {
        if #available(iOS 27.1, *) {
            SystemBarEdge(content: content)
        } else {
            content(false)
        }
    }
}

@available(iOS 27.1, *)
private struct SystemBarEdge<Content: View>: View {
    @Environment(\.toolbarVerticalEdge) private var edge
    let content: (Bool) -> Content

    var body: some View { content(edge != nil) }
}

/// The title's menu in the compact bar: the open tabs in rail order (⌘1 to ⌘9), each with its
/// number and status and the focused one checked, where picking one switches tabs as a chip tap
/// does. Then Close tab for the one on screen while there is another, as the chip's menu had
/// it, and All sessions back to the roster.
struct WorkspaceTabMenu: View {
    struct Entry: Identifiable, Equatable {
        var id: TabID
        /// "04 app", as the chip said it.
        var title: String
        var status: String?
    }

    let entries: [Entry]
    let selected: TabID?
    let onSelect: (TabID) -> Void
    let onClose: (TabID) -> Void
    let onAllSessions: () -> Void

    var body: some View {
        Section(WorkspaceCopy.tabs) {
            ForEach(entries) { entry in
                Toggle(isOn: Binding(get: { entry.id == selected }, set: { if $0 { onSelect(entry.id) } })) {
                    Text(verbatim: entry.title)
                    if let status = entry.status { Text(status) }
                }
                .accessibilityIdentifier("workspace.tabMenu.\(entry.id.sessionID)")
            }
        }
        Section {
            if entries.count > 1, let selected {
                Button(WorkspaceCopy.closeTab, systemImage: "xmark") { onClose(selected) }
            }
            Button(WorkspaceCopy.allSessions, systemImage: "list.bullet", action: onAllSessions)
                .accessibilityIdentifier("workspace.allSessions")
        }
    }
}

extension AppStore {
    /// The title menu's entries, in the order of the tabs and ⌘1 to ⌘9.
    var tabMenuEntries: [WorkspaceTabMenu.Entry] {
        workspace.tabs.map { tab in
            let label = chipLabel(for: tab)
            return WorkspaceTabMenu.Entry(
                id: tab.id,
                title: [label.bib, label.name].compactMap(\.self).joined(separator: " "),
                status: session(for: tab).map { LifecycleCopy.status($0.status, exit: $0.exit) }
            )
        }
    }
}

/// Beside a vertical bar the system sets the title in a row of its own above the content, which
/// would take back the height the bar gave the terminal. There the title leaves the screen; the
/// window and VoiceOver keep it, and the session menu says the name and status at its top.
struct TitleOffBesideVerticalBar: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 27.1, *) {
            content.modifier(VerticalEdgeTitle())
        } else {
            content
        }
    }
}

@available(iOS 27.1, *)
private struct VerticalEdgeTitle: ViewModifier {
    @Environment(\.toolbarVerticalEdge) private var edge

    func body(content: Content) -> some View {
        content.toolbar(removing: edge == nil ? nil : .title)
    }
}

/// The workspace is a task screen: on a Duo the vertical bar keeps the session's items and lets
/// the tab bar minimize first (HIG, iPhone Duo; `D14`).
struct TaskBarCompression: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 27.1, *) {
            content.toolbarVerticalCompressionBehavior(.prefersToolbarItems)
        } else {
            content
        }
    }
}
