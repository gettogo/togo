import SwiftUI
import TogoKit

/// The workspace's bar items, in the navigation bar its stack supplies, so on an iPhone Duo they
/// move into the vertical bar beside the status bar and the terminal keeps the whole height.
/// Every item has a symbol and a title: the symbol shows in the bar, the title in overflow and
/// to VoiceOver. Order: the way back first, then the session's buttons (the layer switch
/// leading them), its menu last.
struct WorkspaceToolbar<Actions: View>: ToolbarContent {
    let leading: WorkspaceLeading
    let onLeading: () -> Void
    /// "Run again", shown once the session's agent stopped.
    let runAgain: WorkspaceRunAgain?
    let review: ReviewToolbarItems?
    /// "Terminal" or "Chat", for a session an adapter serves.
    let layer: LayerToolbarItem?
    /// Mission control beside the chat, on a regular width.
    var mission: MissionToolbarItem? = nil
    @ViewBuilder let actions: () -> Actions

    var body: some ToolbarContent {
        if let item = leading.item {
            ToolbarItem(placement: .navigation) {
                Button(item.title, systemImage: item.symbol, action: onLeading)
            }
        }
        if let runAgain {
            ToolbarItem(placement: .primaryAction) {
                Button(LifecycleCopy.runAgain, systemImage: "arrow.clockwise", action: runAgain.run)
                    .disabled(runAgain.running)
            }
        }
        if let layer {
            layer
        }
        if let mission {
            mission
        }
        if let review {
            review
        }
        // Not the ellipsis: that symbol belongs to the system's own overflow.
        if #available(iOS 27.0, *) {
            menu.visibilityPriority(.high)
        } else {
            menu
        }
    }

    private var menu: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Menu {
                actions()
            } label: {
                Label(WorkspaceCopy.actions, systemImage: "slider.horizontal.3")
            }
        }
    }
}

/// "Run again" once the session's agent stopped; while the helper starts it the item waits.
struct WorkspaceRunAgain {
    var running: Bool
    var run: () -> Void
}

/// The item before the title.
enum WorkspaceLeading: Equatable {
    /// "Sessions", back to the list the workspace covers.
    case back
    /// Shows the sidebar the workspace sits beside.
    case showSidebar
    /// Nothing: the sidebar is on screen beside the workspace.
    case none

    var item: (title: String, symbol: String)? {
        switch self {
        case .back: (WorkspaceCopy.sessions, "chevron.backward")
        case .showSidebar: (AppCommandCopy.showSidebar, "sidebar.leading")
        case .none: nil
        }
    }
}

/// "Preview" and "3 changes", each its own bar item so both can go vertical. The count is a
/// badge on the symbol; the words stay the item's title.
struct ReviewToolbarItems: ToolbarContent {
    let store: AppStore
    let row: SessionRow
    let model: ReviewHeaderModel

    var body: some ToolbarContent {
        if model.hasPorts {
            ToolbarItem(placement: .primaryAction) {
                Button(ReviewEntryCopy.preview, systemImage: "safari") { store.review(row, tab: .ports) }
                    .accessibilityHint(ReviewEntryCopy.previewHint)
            }
        }
        if let label = model.changesLabel {
            ToolbarItem(placement: .primaryAction) {
                Button(label, systemImage: "plus.forwardslash.minus") { store.review(row, tab: .changes) }
                    .badge(model.changes)
                    .accessibilityLabel(label)
                    .accessibilityHint(ReviewEntryCopy.changesHint)
            }
        }
    }
}

/// What the custom header used to draw, said in the navigation title and subtitle instead:
/// the session's name, then its bib, status, agent, machine and the link while it has something
/// to say. A passing message such as "Copied" takes the subtitle until the next one.
struct WorkspaceTitle: Equatable {
    var title: String
    var subtitle: String
    /// Why the link fell back from the transport asked for, for the session menu.
    var fallback: String?

    init(tab: WorkspaceTab, row: SessionRow?, machine: String?, message: String?) {
        let name = row.map { $0.name.isEmpty ? $0.agentName : $0.name } ?? tab.title
        title = name
        let link = tab.link
        if case .live(let transport, let note?) = link {
            fallback = WorkspaceCopy.fallback(transport: transport, note: note)
        } else {
            fallback = nil
        }
        if let message {
            subtitle = message
            return
        }
        var words: [String] = []
        if let bib = row?.bib ?? tab.bib { words.append(bib) }
        if let row { words.append(LifecycleCopy.status(row.status, exit: row.exit)) }
        if let agent = row?.agentName ?? tab.agent, !agent.isEmpty, agent.caseInsensitiveCompare(name) != .orderedSame { words.append(agent) }
        if let machine { words.append(machine) }
        // The transport is said only while the link is not simply up, or fell back.
        if case .live = link {
            if fallback != nil { words.append(link.word) }
        } else {
            words.append(link.word)
        }
        subtitle = words.joined(separator: " · ")
    }
}

extension LinkPhase {
    /// The one word for the link: the transport while live, otherwise what the link is doing.
    var word: String {
        switch self {
        case .waking: WorkspaceCopy.waking
        case .connecting: WorkspaceCopy.connecting
        case .live(let transport, _): transport
        case .roaming: WorkspaceCopy.roaming
        case .reconnecting: WorkspaceCopy.reconnecting
        case .failed, .signedOut: WorkspaceCopy.notConnected
        }
    }
}

/// The bar's items, title and subtitle on the workspace. Shown and hidden at once, never
/// animated, so the terminal under it is fitted once per change.
struct WorkspaceBar<Items: ToolbarContent>: ViewModifier {
    /// Zen with the controls away: the bar stays, so the terminal keeps its size, and holds only
    /// "Show controls". A bar that came and went would resize the terminal each way.
    let revealOnly: Bool
    let title: WorkspaceTitle?
    let onReveal: @MainActor () -> Void
    @ToolbarContentBuilder let items: () -> Items

    func body(content: Content) -> some View {
        content
            .toolbar {
                if revealOnly {
                    ToolbarItem(placement: .primaryAction) {
                        Button(ZenCopy.showControls, systemImage: "rectangle.topthird.inset.filled", action: onReveal)
                            .accessibilityIdentifier("zen.showControls")
                    }
                }
                if !revealOnly {
                    items()
                }
            }
            .navigationTitle(revealOnly ? "" : title?.title ?? "")
            .navigationSubtitle(revealOnly ? "" : title?.subtitle ?? "")
            .navigationBarTitleDisplayMode(.inline)
            .modifier(TitleOffBesideVerticalBar())
    }
}

/// Beside a vertical bar the system sets the title in a row of its own above the content, which
/// would take back the height the bar gave the terminal. There the title leaves the screen; the
/// window and VoiceOver keep it, and the session menu says the name and status at its top.
private struct TitleOffBesideVerticalBar: ViewModifier {
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

/// Reads the counts behind "Preview" and "3 changes" when the session on screen changes, when
/// its agent stops, after a write in the review and when the review closes.
struct ReviewCounts: ViewModifier {
    let model: ReviewHeaderModel
    let store: AppStore
    let row: SessionRow?

    func body(content: Content) -> some View {
        content
            .task(id: row.map { store.reviewSubject(for: $0) }) {
                if let row { await model.select(store.reviewSubject(for: row)) }
            }
            .onChange(of: row?.status) { _, status in
                if let status { Task { await model.statusChanged(to: status) } }
            }
            .onChange(of: store.reviewWrites) { Task { await model.refresh() } }
            .onChange(of: store.reviewing != nil) { _, open in Task { await model.reviewChanged(open: open) } }
    }
}
