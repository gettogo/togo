import SwiftUI
import TogoKit
import TogoPush
import UserNotifications

/// The guide's pictures are the app's own views with fixed demo content: nothing is connected,
/// and none of them takes input.

/// A short exchange in a session's chat: the prompt, the turn's work line and the reply, under
/// the session's title and the Terminal item the real bar carries.
struct ChatDemo: View {
    static let prompt = String(localized: "tutorial.chat.demo.prompt", defaultValue: "Add a test for an empty password")
    static let reply = String(
        localized: "tutorial.chat.demo.reply",
        defaultValue: "Added `rejects an empty password` to `test/login.test.ts`. It passes, and the other 41 tests still do."
    )

    /// A finished turn: 1 minute 12 seconds, 6 steps, 1 file changed.
    static let summary = WorkSummary(
        running: false, startedAt: Date(timeIntervalSince1970: 0), endedAt: Date(timeIntervalSince1970: 72),
        steps: 6, filesChanged: 1, failed: false
    )

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            header
            PromptBubble(text: Self.prompt)
            WorkLine(summary: Self.summary, steps: [], disclosure: Disclosure(), onToggle: {}, onStep: { _ in }, counting: false)
            ReplyRow(text: Self.reply)
        }
        .padding(16)
        .background(Color.togoInk, in: .rect(cornerRadius: 20, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 20, style: .continuous).strokeBorder(Color.togoLine))
        // A picture: at the largest sizes the page's own text carries the point.
        .dynamicTypeSize(...DynamicTypeSize.xLarge)
        .allowsHitTesting(false)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(String(
            localized: "tutorial.chat.demo.label",
            defaultValue: "A chat with Claude Code. You ask it to add a test for an empty password; it works for a minute and says what it changed. A Terminal button sits at the top."
        ))
    }

    private var header: some View {
        HStack(alignment: .center, spacing: 8) {
            AgentGlyph(agent: "claude")
                .font(.body)
                .foregroundStyle(Color.togoSnow)
            BibText("04")
            Text(verbatim: "app")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                // trunc-ok: a fixed demo session name
                .lineLimit(1)
            Spacer(minLength: 8)
            Label(ChatCopy.terminal, systemImage: "terminal")
                .font(.subheadline.weight(.medium))
                .foregroundStyle(Color.togoSnow)
                .labelStyle(.titleAndIcon)
                // trunc-ok: the one-word bar item it stands for
                .lineLimit(1)
                .padding(.horizontal, 12)
                .padding(.vertical, 7)
                .background(Color.togoRaised, in: Capsule())
        }
    }
}

/// A request on the Lock Screen: the Live Activity card iOS draws, with its Approve and Deny.
struct LockScreenDemo: View {
    /// A fixed clock for snapshots; nil follows the time of day.
    var now: Date?

    static let requestID = "tutorial-request"

    var body: some View {
        let at = now ?? Date()
        let state = TogoActivityAttributes.ContentState(
            bib: "04", agent: "claude", status: "needs_you", waiting: 1,
            since: Int(at.timeIntervalSince1970) - 252, requestId: Self.requestID, command: nil
        )
        let context = LiveContext(
            title: ChatDemo.prompt, ask: String(localized: "tutorial.approvals.demo.ask", defaultValue: "Run this command?"),
            subject: PushSubject(kind: "command", text: "npm test"), answer: .approveDeny
        )
        let card = LiveCard(
            agent: "claude", status: "needs_you", waiting: 1, requestId: Self.requestID, command: nil,
            context: context, machine: "Studio", stale: false
        )
        // A host id only so the card draws its buttons; hit testing is off, so they never run.
        LiveLockScreenView(state: state, card: card, hostId: "tutorial")
            .background(Color.togoInk)
            .clipShape(RoundedRectangle(cornerRadius: 24, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 24, style: .continuous).strokeBorder(Color.togoLine))
            .environment(\.liveClockFrozenAt, now)
            .allowsHitTesting(false)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(String(
                localized: "tutorial.approvals.demo.label",
                defaultValue: "A Live Activity on the Lock Screen: Claude Code on Studio needs you to approve npm test, with Deny and Approve buttons."
            ))
    }
}

/// A request as a notification, opened to show its actions: the words the notification
/// extension builds for the push and the actions of the category it files it under. For devices
/// without Live Activities, such as an iPad.
struct NotificationDemo: View {
    /// A fixed clock for snapshots; nil follows the time of day.
    var now: Date?

    static let payload = PushPayload(
        kind: .needsYou, sessionId: "tutorial-session", bib: "04", agent: "claude", title: "Run npm test?",
        body: "npm test", requestId: LockScreenDemo.requestID,
        options: [PushOption(n: 1, label: "Yes", role: "approve"), PushOption(n: 2, label: "No", role: "deny")],
        subject: PushSubject(kind: "command", text: "npm test"), ts: 0, sessionName: "app"
    )

    var body: some View {
        let text = NotificationTextBuilder.build(Self.payload)
        VStack(alignment: .leading, spacing: 8) {
            if let text {
                content(text)
                actions(text.category)
            }
        }
        .dynamicTypeSize(...DynamicTypeSize.xLarge)
        .allowsHitTesting(false)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(String(
            localized: "tutorial.approvals.notification.label",
            defaultValue: "A notification from Togo: Claude Code wants to run npm test, with Approve and Deny below it."
        ))
    }

    private func content(_ text: NotificationText) -> some View {
        HStack(alignment: .top, spacing: 10) {
            LaunchMark(side: 38)
                .frame(width: 38, height: 38)
                .background(Color.togoInk, in: .rect(cornerRadius: 9, style: .continuous))
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline) {
                    Text(text.title)
                        .font(.subheadline.weight(.semibold))
                    Spacer(minLength: 8)
                    Text(String(localized: "tutorial.approvals.notification.now", defaultValue: "now"))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                if !text.subtitle.isEmpty {
                    Text(text.subtitle)
                        .font(.subheadline.weight(.semibold))
                }
                Text(text.body)
                    .font(.subheadline)
            }
            .fixedSize(horizontal: false, vertical: true)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.regularMaterial, in: .rect(cornerRadius: 24, style: .continuous))
    }

    /// The category's Approve and Deny, as the system lists them under an opened notification.
    private func actions(_ category: String) -> some View {
        let shown = NotificationCategorySet.make(approveWithoutUnlock: false)
            .first { $0.identifier == category }?.actions
            .filter { [NotificationActionID.approve, NotificationActionID.deny].contains($0.identifier) } ?? []
        return VStack(spacing: 0) {
            ForEach(Array(shown.enumerated()), id: \.offset) { index, action in
                if index > 0 { Divider() }
                HStack {
                    Text(action.title)
                    Spacer(minLength: 8)
                    Image(systemName: action.identifier == NotificationActionID.approve ? "checkmark" : "xmark")
                }
                .font(.body)
                .foregroundStyle(action.options.contains(.destructive) ? Color.red : Color.primary)
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
            }
        }
        // width-ok: a maximum, the width of the action menu iOS draws under an opened notification.
        .frame(maxWidth: 280)
        .background(.regularMaterial, in: .rect(cornerRadius: 16, style: .continuous))
    }
}

/// The marks of the agents a session can start, as the new-session picker shows them.
struct AgentMarks: View {
    @ScaledMetric(relativeTo: .title2) private var cell: CGFloat = 36

    static var agents: [AgentCatalog.Entry] {
        AgentCatalog.entries.filter { $0.id != "shell" }
    }

    var body: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: cell), spacing: 12)], alignment: .leading, spacing: 12) {
            ForEach(Self.agents, id: \.id) { entry in
                AgentGlyph(agent: entry.id)
                    .font(.title2)
                    .foregroundStyle(Color.togoSnow)
                    .frame(width: cell, height: cell)
            }
        }
        .padding(16)
        .background(Color.togoGrouped, in: .rect(cornerRadius: 16, style: .continuous))
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(String(
            localized: "tutorial.sessions.agents.label",
            defaultValue: "Agents you can start: \(Self.names)"
        ))
    }

    private static var names: String {
        ListFormatter.localizedString(byJoining: agents.map(\.name))
    }
}
