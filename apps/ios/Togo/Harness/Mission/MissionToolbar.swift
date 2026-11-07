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

    static func items(needsYou: Int, working: Bool, canCompact: Bool) -> [Item] {
        var items: [Item] = []
        if needsYou > 0 { items.append(.goToRequest) }
        if working { items.append(.stop) }
        if canCompact { items.append(.compact) }
        items.append(.terminal)
        return items
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

    private var summary: MissionSummary { store.mission.summary }
    private var badge: Int { MissionPlacement.badge(summary) }

    var body: some ToolbarContent {
        if #available(iOS 27.0, *) {
            item.visibilityPriority(badge > 0 ? ToolbarItemVisibilityPriority(higherThan: .high) : .automatic)
        } else {
            item
        }
    }

    private var item: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Menu {
                ForEach(MissionMenu.items(needsYou: badge, working: summary.working, canCompact: compact != nil), id: \.self) { entry in
                    Button(MissionMenu.title(entry), systemImage: MissionMenu.symbol(entry), role: entry == .stop ? .destructive : nil) {
                        perform(entry)
                    }
                }
            } label: {
                MissionItemLabel(session: MissionInspectorMemory.key(store.subject), needsYou: badge)
            } primaryAction: {
                MissionInspectorMemory.shared.toggle(store, in: presentation)
            }
            .badge(badge)
            .keyboardShortcut("i", modifiers: .command)
            .accessibilityValue(MissionText.spoken(summary))
            .accessibilityHint(presentation == .inspector ? MissionCopy.toggleHint : MissionCopy.openHint)
            .accessibilityIdentifier("mission.item")
        }
    }

    private func perform(_ entry: MissionMenu.Item) {
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

/// The item's symbol and title. It bounces once when a new request arrives on the session on
/// screen; never on a change of session and never under Reduce Motion.
private struct MissionItemLabel: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let session: String
    let needsYou: Int
    @State private var bounces = 0

    var body: some View {
        Label(MissionCopy.title, systemImage: "gauge.with.dots.needle.33percent")
            // motion: attention — one bounce as the badge rises: look here, something needs you.
            .symbolEffect(.bounce, value: bounces)
            .onChange(of: MissionNudge.Snapshot(session: session, needsYou: needsYou)) { old, new in
                if MissionNudge.bounces(old, new, reduceMotion: reduceMotion) { bounces += 1 }
            }
    }
}

extension AppStore {
    /// The focused tab's chat store while its chat shows and it has mission control: the one
    /// session the bar item stands for.
    var missionStore: HarnessSessionStore? {
        guard let tab = focusedTab, let harness = harness(for: tab.id),
              harness.shownLayer == .chat, harness.hasMission else { return nil }
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
