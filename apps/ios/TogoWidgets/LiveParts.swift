import Foundation
import SwiftUI
import Synchronization
import TogoPush

/// The agent's mark, in the current foreground style and font: its logo, its letters in SF
/// Mono, or the terminal for a shell or an unknown program.
struct LiveMark: View {
    let agent: String

    var body: some View {
        Group {
            switch LiveAgentMark.of(agent) {
            case .logo(let name): Image(name)
            case .symbol(let name): Image(systemName: name)
            case .monogram(let letters): Text(verbatim: letters).monospaced().fontWeight(.semibold)
            }
        }
        .accessibilityHidden(true)
    }
}

/// The running clock at the width of its widest reading. A system timer text takes all the
/// width it is offered, which pushed the title out on the lock screen and was cut to "0:…" in
/// the island; a hidden reading sizes it instead, so neither happens.
struct LiveClock: View {
    @Environment(\.liveClockFrozenAt) private var frozenAt
    let since: Int

    var body: some View {
        Text(verbatim: placeholder)
            .monospacedDigit()
            .hidden()
            .overlay(alignment: .trailing) {
                LiveElapsed(since: since)
                    .multilineTextAlignment(.trailing)
            }
            .fixedSize()
    }

    /// "00:00" under 50 minutes, else "0:00:00": room for the next hour before the next push.
    private var placeholder: String {
        let elapsed = (frozenAt ?? Date()).timeIntervalSince(LiveCopy.sinceDate(since))
        return elapsed < 50 * 60 ? "00:00" : "0:00:00"
    }
}

/// What a request is about. Commands, paths and URLs are machine text, in mono on a raised
/// plate; a question's own text stays in the body font. The helper already shortened it from
/// the middle, so the start and the end both show.
struct LiveDetailBlock: View {
    let detail: LiveCard.Detail
    var lines = 2
    var cornerRadius: CGFloat = 12
    /// Inner space above and below machine text; the island's tighter block uses less.
    var verticalPadding: CGFloat = 8

    var body: some View {
        Group {
            if detail.isMachineText {
                Text(verbatim: detail.text)
                    .font(.system(.footnote, design: .monospaced))
                    // trunc-ok: the Live Activity has a fixed height; the helper middle-shortened the command or path so both ends show, and a tap opens the request in full
                    .lineLimit(lines)
                    .truncationMode(.middle)
                    .padding(.horizontal, 10)
                    .padding(.vertical, verticalPadding)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color.togoRaised, in: RoundedRectangle(cornerRadius: cornerRadius, style: .continuous))
            } else {
                Text(verbatim: detail.text)
                    .font(.subheadline)
                    // trunc-ok: the Live Activity has a fixed height; a tap opens the request with its whole text
                    .lineLimit(lines)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .foregroundStyle(Color.togoSnow)
        .privacySensitive()
    }
}

/// The question above its detail, or on its own when there is none.
struct LiveAskText: View {
    let ask: String
    let alone: Bool

    var body: some View {
        Text(verbatim: ask)
            .font(alone ? .subheadline : .footnote)
            .foregroundStyle(alone ? Color.togoSnow : Color.togoMute)
            // trunc-ok: the Live Activity has a fixed height; a tap opens the request with its whole question
            .lineLimit(alone ? 3 : 1)
            .frame(maxWidth: .infinity, alignment: .leading)
            .privacySensitive()
    }
}

/// The session's title line: the mark, then the title.
struct LiveTitle: View {
    let card: LiveCard
    var font: Font = .headline

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            LiveMark(agent: card.agentID)
                .font(font)
                .foregroundStyle(Color.togoSnow)
            Text(verbatim: card.title)
                .font(font)
                .foregroundStyle(Color.togoSnow)
                // trunc-ok: the Live Activity has a fixed width; the helper caps a session title at 64 characters and a tap opens the session
                .lineLimit(1)
                .privacySensitive()
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(card.agentName.isEmpty ? card.title : "\(card.agentName), \(card.title)")
    }
}

/// Reads the activity's context: the sealed one it carries, else the request the notification
/// service extension stored for it. The last opened envelope is kept, since the system draws
/// every presentation from the same state.
enum LiveContextSource {
    private struct Opened: Sendable {
        var envelope: String
        var context: LiveContext
    }

    private static let last = Mutex<Opened?>(nil)

    static func context(_ state: TogoActivityAttributes.ContentState, hostId: String) -> LiveContext? {
        if let envelope = state.ctx {
            if let hit = last.withLock({ $0?.envelope == envelope ? $0?.context : nil }) { return hit }
            if let opened = LiveContext.open(envelope, hostId: hostId, keys: KeychainDeviceKeys(accessGroup: nil)) {
                last.withLock { $0 = Opened(envelope: envelope, context: opened) }
                return opened
            }
        }
        guard let requestId = state.requestId, let directory = SharedGroup.storeDirectory(SharedGroup.ios)
        else { return nil }
        let pending = PendingStore(directory: directory, clock: { Date() }).request(requestId)
        return fallback(state, hostId: hostId, pending: pending)
    }

    static func fallback(
        _ state: TogoActivityAttributes.ContentState, hostId: String, pending: PendingRequest?
    ) -> LiveContext? {
        guard let pending, pending.hostId == hostId, pending.requestId == state.requestId else { return nil }
        return LiveContext(pending: pending)
    }

    /// The card for one render: the machine is named only when there is more than one.
    static func card(_ state: TogoActivityAttributes.ContentState, hostId: String, stale: Bool) -> LiveCard {
        let pairings = LiveCopy.sharedPairings()
        let several = (pairings?.all().count ?? 0) > 1
        return LiveCard(
            agent: state.agent, status: state.status, waiting: state.waiting, requestId: state.requestId,
            command: state.command, context: context(state, hostId: hostId),
            machine: several ? LiveCopy.machineName(hostId: hostId, pairings: pairings) : nil, stale: stale,
            showsCommand: pairings?.pairing(hostId: hostId)?.appHostID
                .map(SharedSettings.readLiveActivityCommandMachines().contains) ?? false
        )
    }
}
