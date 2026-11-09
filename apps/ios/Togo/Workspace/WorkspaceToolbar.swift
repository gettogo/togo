import SwiftUI
import TogoKit

/// The workspace's bar items, in the navigation bar its stack supplies, so on an iPhone Duo they
/// move into the vertical bar beside the status bar and the terminal keeps the whole height.
/// Every item has a symbol and a title: the symbol shows in the bar, the title in overflow and
/// to VoiceOver. Order: the way back first, then the session's buttons (the layer switch
/// leading them), its menu last. When the bar runs out of room the items stay in this order of
/// priority: mission control while it has a badge, the session menu, the layer switch, the rest.
struct WorkspaceToolbar<Actions: View>: ToolbarContent {
    let leading: WorkspaceLeading
    let onLeading: () -> Void
    /// "Run again", shown once the session's agent stopped.
    let runAgain: WorkspaceRunAgain?
    let review: ReviewToolbarItems?
    /// "Terminal" or "Chat", for a session an adapter serves.
    let layer: LayerToolbarItem?
    /// Mission control, on every size class while the chat shows and the session has one.
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
            if #available(iOS 27.0, *) {
                layer.visibilityPriority(ToolbarItemVisibilityPriority(lowerThan: .high))
            } else {
                layer
            }
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

/// The session's own bar items as the bar shows them. The workspace keeps them in step with the
/// session: an item arriving or leaving as capabilities and state come in
/// crossfades in a `respond` transaction instead of popping.
struct WorkspaceBarItems {
    var runAgain: WorkspaceRunAgain?
    var layer: LayerToolbarItem?
    var mission: MissionToolbarItem?

    /// What the items look like, to tell a change worth animating from a new closure.
    struct Key: Equatable {
        var tab: TabID?
        var running: Bool?
        var layer: SessionLayer?
        var mission: Bool
    }

    func key(tab: TabID?) -> Key {
        Key(tab: tab, running: runAgain?.running, layer: layer?.layer, mission: mission != nil)
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
    /// Nothing: the workspace is the detail column; the split view's toggle handles the sidebar.
    case none

    var item: (title: String, symbol: String)? {
        switch self {
        case .back: (WorkspaceCopy.sessions, "chevron.backward")
        case .none: nil
        }
    }
}

/// Which review items mission control already carries. On the chat layer of a session whose
/// mission has Ports or Changes, those sections are one tap away in mission control, so the
/// bar leaves Preview or Changes out; on the terminal layer, and for sessions without the
/// capability, they stay.
struct ReviewFold: Equatable {
    var preview = false
    var changes = false

    /// `descriptor` is the focused session's while its chat shows with a mission, else nil.
    init(_ descriptor: HarnessDescriptor?) {
        guard let descriptor else { return }
        preview = descriptor.has(HarnessCapability.ports)
        changes = descriptor.has(HarnessCapability.changes)
    }
}

/// "Preview" and "3 changes", each its own bar item so both can go vertical. The count is a
/// badge on the symbol; the words stay the item's title.
struct ReviewToolbarItems: ToolbarContent {
    let store: AppStore
    let row: SessionRow
    let model: ReviewHeaderModel
    var folded = ReviewFold(nil)

    var body: some ToolbarContent {
        if model.hasPorts, !folded.preview {
            ToolbarItem(placement: .primaryAction) {
                Button(ReviewEntryCopy.preview, systemImage: "safari") { store.review(row, tab: .ports) }
                    .accessibilityHint(ReviewEntryCopy.previewHint)
            }
        }
        if let label = model.changesLabel, !folded.changes {
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
/// to say. A passing message such as "Copied" takes the subtitle until the next one. A session
/// with mission control shows its glance there instead ("2 agents  plan 3/7", nothing when
/// nothing is counted); the session menu keeps the status line at its top.
struct WorkspaceTitle: Equatable {
    var title: String
    /// The bar's subtitle.
    var subtitle: String
    /// The bib, status, agent, machine and link (or the passing message), for the session menu.
    var detail: String
    /// Why the link fell back from the transport asked for, for the session menu.
    var fallback: String?

    init(tab: WorkspaceTab, row: SessionRow?, machine: String?, message: String?, glance: String? = nil) {
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
            detail = message
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
        detail = words.joined(separator: " · ")
        subtitle = glance ?? detail
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
