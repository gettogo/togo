import SwiftUI
import TogoKit
import UIKit

/// What the bar item's long press offers, apart from the view. Few items, each also reachable
/// elsewhere; never Approve or Deny, which need the request's own context.
enum MissionMenu {
    enum Item: Equatable, CaseIterable {
        /// Only while something needs the user: the chat, scrolled to the oldest open card.
        case goToRequest
        /// Only while a turn runs; destructive.
        case stop
        /// Only with the session's `compact` action; asks first.
        case compact
        case terminal
    }

    /// Terminal only while the chat shows: over the terminal it would go nowhere.
    static func items(needsYou: Int, working: Bool, canCompact: Bool, chatShows: Bool = true) -> [Item] {
        var items: [Item] = []
        if needsYou > 0 { items.append(.goToRequest) }
        if working { items.append(.stop) }
        if canCompact { items.append(.compact) }
        if chatShows { items.append(.terminal) }
        return items
    }

    /// The choices the session menu's mission section repeats: all but Terminal.
    static func inSessionMenu(_ items: [Item]) -> [Item] {
        items.filter { $0 != .terminal }
    }

    static func title(_ item: Item) -> String {
        switch item {
        case .goToRequest: MissionCopy.goToRequest
        case .stop: MissionCopy.stop
        case .compact: MissionCopy.compactMenu
        case .terminal: ChatCopy.terminal
        }
    }

    static func symbol(_ item: Item) -> String {
        switch item {
        case .goToRequest: "hand.raised"
        case .stop: "stop.circle"
        case .compact: "arrow.down.right.and.arrow.up.left"
        case .terminal: "terminal"
        }
    }
}

/// Mission control's bar item, on every size class: the system puts it in the vertical bar on
/// a Duo and in the top bar elsewhere. Tap shows or hides mission control (Cmd-I); a long
/// press is the system's menu, with its own lift and haptic. The badge is the needs-you count,
/// and the item stays in the bar longest while it has one.
struct MissionToolbarItem: ToolbarContent {
    let store: HarnessSessionStore
    let presentation: MissionPresentation
    /// Raises the Compact dialog; nil without the session's `compact` action.
    var compact: (() -> Void)?
    /// Stops the turn; the host reports a failure.
    var stop: () -> Void = {}

    var summary: MissionSummary { store.mission.summary }
    var badge: Int { MissionPlacement.badge(summary) }
    /// The long press's choices for the session now.
    var menuItems: [MissionMenu.Item] {
        MissionMenu.items(
            needsYou: badge, working: summary.working, canCompact: compact != nil, chatShows: store.shownLayer == .chat
        )
    }

    var body: some ToolbarContent {
        if #available(iOS 27.0, *) {
            item.visibilityPriority(DockPriority.tier(.mission, missionNeedsYou: badge > 0).priority)
        } else {
            item
        }
    }

    private var item: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Menu {
                ForEach(menuItems, id: \.self) { entry in
                    Button(MissionMenu.title(entry), systemImage: MissionMenu.symbol(entry), role: entry == .stop ? .destructive : nil) {
                        perform(entry)
                    }
                }
            } label: {
                MissionItemLabel(session: MissionKey.of(store.subject), needsYou: badge, symbol: MissionPlacement.symbol(presentation))
            } primaryAction: {
                store.showsMission.toggle()
            }
            .badge(badge)
            .keyboardShortcut("i", modifiers: .command)
            .accessibilityValue(MissionText.spoken(summary))
            .accessibilityHint(presentation == .inspector ? MissionCopy.toggleHint : MissionCopy.openHint)
            .accessibilityIdentifier("mission.item")
        }
    }

    func perform(_ entry: MissionMenu.Item) {
        switch entry {
        case .goToRequest:
            if presentation == .sheet { store.showsMission = false }
            store.revealNeeds()
        case .stop:
            stop()
        case .compact:
            compact?()
        case .terminal:
            if presentation == .sheet { store.showsMission = false }
            UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
            // motion: cause → effect — the user's pick crossfades chat into terminal in place,
            // the same as the Terminal switch beside it.
            withAnimation(Motion.animation(.fade, reduce: UIAccessibility.isReduceMotionEnabled)) {
                store.layer = .terminal
            }
        }
    }
}

/// Mission control as a section of the session menu, where a compact horizontal bar keeps three
/// items: the same toggle and the long press's choices, without Terminal, which the layer switch
/// beside the menu already is. ⌘I is the workspace's (`WorkspaceView.shortcuts`).
struct MissionMenuSection: View {
    let item: MissionToolbarItem

    var body: some View {
        Section {
            Button(MissionCopy.title, systemImage: MissionPlacement.symbol(item.presentation)) {
                item.store.showsMission.toggle()
            }
            .accessibilityValue(MissionText.spoken(item.summary))
            .accessibilityHint(MissionCopy.openHint)
            .accessibilityIdentifier("mission.menuItem")
            ForEach(MissionMenu.inSessionMenu(item.menuItems), id: \.self) { entry in
                Button(MissionMenu.title(entry), systemImage: MissionMenu.symbol(entry), role: entry == .stop ? .destructive : nil) {
                    item.perform(entry)
                }
            }
        }
    }
}

/// The item's symbol and title. It bounces once when a new request arrives on the session on
/// screen; never on a change of session and never under Reduce Motion.
private struct MissionItemLabel: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let session: String
    let needsYou: Int
    let symbol: String
    @State private var bounces = 0

    var body: some View {
        // The title is what VoiceOver says and what the overflow shows.
        Label(MissionCopy.title, systemImage: symbol)
            // motion: attention — one bounce as the badge rises: look here, something needs you.
            .symbolEffect(.bounce, value: bounces)
            .onChange(of: MissionNudge.Snapshot(session: session, needsYou: needsYou)) { old, new in
                if MissionNudge.bounces(old, new, reduceMotion: reduceMotion) { bounces += 1 }
            }
    }
}

extension AppStore {
    /// The focused tab's chat store while it can show its chat and has mission control: the one
    /// session the bar item stands for. On either layer, so the bar, its subtitle (the glance)
    /// and the session menu are the same in the chat and the terminal. Gated on the chat
    /// showing, the terminal's subtitle fell back to the status line, which took the width the
    /// layer switch and the session menu need and sent both to the system's overflow.
    var missionStore: HarnessSessionStore? {
        guard let tab = focusedTab, let harness = harness(for: tab.id),
              harness.chatAvailable, harness.hasMission else { return nil }
        return harness
    }

    /// Follows a target mission control hands out for the session in `tab`: a background
    /// process's live terminal as a tab, a subagent's chat or the session's terminal through
    /// `openHarnessTarget`, and anything else in the review.
    func followMissionTarget(_ target: HarnessOpen, from tab: TabID) {
        if target.kind == .terminal, let terminal = TerminalTarget(target.ref) {
            openBackgroundTerminal(terminal, on: tab.hostID)
        } else if !openHarnessTarget(target, from: tab) {
            openMissionTarget(target, hostID: tab.hostID, sessionID: tab.sessionID)
        }
    }

    /// Opens a mission control target that leaves for the review: a diff on Changes, a port
    /// or URL on Ports.
    func openMissionTarget(_ target: HarnessOpen, hostID: HostID, sessionID: String) {
        guard let row = controlCenter.machines.first(where: { $0.id == hostID })?.sessions
            .first(where: { $0.id == sessionID }) else { return }
        switch target.kind {
        case .diff: review(row, tab: .changes)
        case .url: review(row, tab: .ports)
        default: break
        }
    }
}

/// Says a new request needs the user, once: the chat's own line for a request names it and
/// goes first, so a second announcement within a few seconds is dropped.
@MainActor enum NeedsAnnouncement {
    private static var last: Date?
    static let window: TimeInterval = 3

    static func post(_ text: String, after delay: Duration = .zero) {
        guard UIAccessibility.isVoiceOverRunning else { return }
        Task { @MainActor in
            if delay > .zero { try? await Task.sleep(for: delay) }
            if let last, Date.now.timeIntervalSince(last) < window { return }
            last = .now
            var line = AttributedString(text)
            line.accessibilitySpeechAnnouncementPriority = .high
            AccessibilityNotification.Announcement(line).post()
        }
    }
}
