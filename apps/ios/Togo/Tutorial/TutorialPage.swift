import SwiftUI
import TogoKit

/// The guide's pages, in order: what Togo gives you, then what your machine needs. The raw value
/// is what progress saves.
enum TutorialPage: Int, CaseIterable, Sendable {
    case welcome, approvals, sessions, setup

    /// `liveActivities` is false where the device shows none, such as an iPad: requests are
    /// answered from a notification there.
    func title(liveActivities: Bool = true) -> String {
        switch self {
        case .welcome: String(localized: "tutorial.chat.title", defaultValue: "Chat with your agents")
        case .approvals where !liveActivities:
            String(localized: "tutorial.approvals.title.notification", defaultValue: "Answer from a notification")
        case .approvals: String(localized: "tutorial.approvals.title", defaultValue: "Answer from the Lock Screen")
        case .sessions: String(localized: "tutorial.sessions.title", defaultValue: "Start agents, review their work")
        case .setup: String(localized: "tutorial.setup.title.machine", defaultValue: "Set up your machine")
        }
    }

    /// One or two short sentences. Anything more sits behind Learn more on the page.
    func message(liveActivities: Bool = true) -> String {
        switch self {
        case .welcome:
            String(
                localized: "tutorial.chat.message",
                defaultValue: "Claude Code, Codex, OpenCode and more run on your Mac or Linux machine. Each one gets a chat here, with its terminal one tap away."
            )
        case .approvals where !liveActivities:
            String(
                localized: "tutorial.approvals.message.notification",
                defaultValue: "When an agent asks to run something, approve or deny it right from the notification, without opening Togo."
            )
        case .approvals:
            String(
                localized: "tutorial.approvals.message",
                defaultValue: "When an agent asks to run something, approve or deny it from the Lock Screen, a notification or your Apple Watch."
            )
        case .sessions:
            String(
                localized: "tutorial.sessions.message",
                defaultValue: "Start a session in any folder on your machine, then look over what the agent changed before you commit it."
            )
        case .setup:
            String(
                localized: "tutorial.setup.message.ssh",
                defaultValue: "Togo signs in over SSH. You need the machine's name or address and your user name on it."
            )
        }
    }
}


/// One page of the guide: a title and a sentence, then the page's content, scrolling as one.
/// With room to spare, the content sits in the middle of the space under the title rather than
/// leaving an empty band below it; `centered: false` keeps it at the top, for content that grows
/// as the user opens it, such as the checklist.
struct TutorialPageLayout<Content: View>: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.liveActivitiesAvailable) private var liveActivitiesAvailable
    let page: TutorialPage
    var centered = true
    @ViewBuilder var content: Content

    private static var topPadding: CGFloat { 8 }
    private static var bottomPadding: CGFloat { 24 }

    var body: some View {
        GeometryReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    VStack(alignment: .leading, spacing: 10) {
                        Text(page.title(liveActivities: liveActivitiesAvailable))
                            .font(.title.weight(.bold))
                            .foregroundStyle(Color.togoSnow)
                            .accessibilityAddTraits(.isHeader)
                        Text(page.message(liveActivities: liveActivitiesAvailable))
                            .font(.body)
                            .foregroundStyle(Color.togoMute)
                    }
                    Spacer(minLength: 28)
                    VStack(alignment: .leading, spacing: 28) {
                        content
                    }
                    if centers {
                        Spacer(minLength: 0)
                    }
                }
                .frame(
                    minHeight: centers ? max(0, proxy.size.height - Self.topPadding - Self.bottomPadding) : nil,
                    alignment: .top
                )
                .screenMargins()
                .padding(.top, Self.topPadding)
                .padding(.bottom, Self.bottomPadding)
            }
            .scrollBounceBehavior(.basedOnSize)
            // At large text sizes a page runs past the screen; the indicator says it scrolls.
            .scrollIndicatorsFlash(onAppear: true)
        }
        .background(Color.togoInk)
    }

    /// At accessibility sizes the text fills the screen, so the content follows it directly.
    private var centers: Bool {
        centered && !dynamicTypeSize.isAccessibilitySize
    }
}

/// A symbol, a heading and a few sentences: the unit the text pages are built from.
struct TutorialPoint<Detail: View>: View {
    @ScaledMetric(relativeTo: .body) private var iconWidth: CGFloat = 28
    let title: String
    let systemImage: String
    @ViewBuilder var detail: Detail

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: 6) {
                Text(title)
                    .font(.headline)
                    .foregroundStyle(Color.togoSnow)
                detail
                    .font(.body)
                    .foregroundStyle(Color.togoMute)
                    .fixedSize(horizontal: false, vertical: true)
            }
        } icon: {
            Image(systemName: systemImage)
                .font(.body.weight(.medium))
                .foregroundStyle(Color.togoSnow)
                .frame(width: iconWidth)
        }
        .accessibilityElement(children: .combine)
    }
}

extension TutorialPoint where Detail == Text {
    init(_ title: String, systemImage: String, _ text: String) {
        self.init(title: title, systemImage: systemImage) { Text(text) }
    }
}

/// Secondary detail for a page, folded away under "Learn more" so the page shows one idea.
struct TutorialMore<Content: View>: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// Opens with the page growing and the points revealing; closes with the points concealing
    /// before the page shrinks.
    @State private var disclosure: Disclosure
    @ViewBuilder var content: Content

    init(expanded: Bool = false, @ViewBuilder content: () -> Content) {
        _disclosure = State(initialValue: Disclosure(open: expanded))
        self.content = content()
    }

    private var expanded: Bool { disclosure.isOpen }

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Button {
                Motion.toggle($disclosure, reduce: reduceMotion)
            } label: {
                HStack(spacing: 6) {
                    Text(expanded
                        ? String(localized: "tutorial.more.hide", defaultValue: "Show less")
                        : String(localized: "tutorial.more.show", defaultValue: "Learn more"))
                        .contentTransition(.interpolate)
                    Image(systemName: "chevron.down")
                        .font(.footnote.weight(.semibold))
                        .rotationEffect(.degrees(expanded ? 180 : 0))
                        .accessibilityHidden(true)
                }
                .font(.body.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .frame(minHeight: 44)
                .contentShape(.rect)
                // motion: cause → effect — the tapped row's chevron turns as it opens
                .motion(.respond, value: expanded)
            }
            .buttonStyle(.plain)
            .accessibilityValue(expanded
                ? String(localized: "tutorial.more.expanded", defaultValue: "Expanded")
                : String(localized: "tutorial.more.collapsed", defaultValue: "Collapsed"))
            if disclosure.open {
                VStack(alignment: .leading, spacing: 20) {
                    content
                }
                // The tap opens it: its edge uncovers the words, nothing rises.
                .unfolds(disclosure)
            }
        }
    }
}

// MARK: Chat

struct WelcomePage: View {
    /// Opens Learn more, for snapshots.
    var expanded = false

    var body: some View {
        TutorialPageLayout(page: .welcome) {
            ChatDemo()
            TutorialMore(expanded: expanded) {
                TutorialPoint(
                    String(localized: "tutorial.chat.terminal.title", defaultValue: "The terminal is one tap away"),
                    systemImage: "terminal",
                    String(
                        localized: "tutorial.chat.terminal.detail",
                        defaultValue: "In a chat, tap the Terminal button to see the agent's own screen, with Esc, Ctrl, Tab and arrow keys above the keyboard."
                    )
                )
                TutorialPoint(
                    String(localized: "tutorial.chat.running.title", defaultValue: "Agents keep running"),
                    systemImage: "rectangle.split.3x1",
                    String(
                        localized: "tutorial.chat.running.detail",
                        defaultValue: "Each agent runs in tmux on your machine, so it carries on when this device sleeps or loses signal."
                    )
                )
                TutorialPoint(
                    String(localized: "tutorial.chat.fallback.title", defaultValue: "Some sessions open as a terminal"),
                    systemImage: "rectangle.on.rectangle",
                    String(
                        localized: "tutorial.chat.fallback.detail",
                        defaultValue: "A plain shell, an agent Togo can't read yet, or any session on a machine without the Togo helper opens in the terminal instead."
                    )
                )
            }
        }
    }
}

// MARK: Approvals

struct ApprovalsPage: View {
    @Environment(\.liveActivitiesAvailable) private var liveActivitiesAvailable
    /// A fixed clock for snapshots; nil follows the time of day.
    var now: Date?
    /// Opens Learn more, for snapshots.
    var expanded = false

    var body: some View {
        TutorialPageLayout(page: .approvals) {
            if liveActivitiesAvailable {
                LockScreenDemo(now: now)
            } else {
                NotificationDemo(now: now)
            }
            TutorialMore(expanded: expanded) {
                TutorialPoint(
                    String(localized: "tutorial.approvals.notification.title", defaultValue: "In a notification"),
                    systemImage: "bell",
                    String(
                        localized: "tutorial.approvals.notification.detail",
                        defaultValue: "It shows the whole request, so you can answer without opening Togo. It is encrypted on your machine for this device, and only this device can read it."
                    )
                )
                if liveActivitiesAvailable {
                    TutorialPoint(
                        String(localized: "tutorial.approvals.live.title", defaultValue: "In a Live Activity"),
                        systemImage: "rectangle.badge.checkmark",
                        String(
                            localized: "tutorial.approvals.live.detail",
                            defaultValue: "While a session's Live Activity runs, the request shows there in place of a notification, with the same Approve and Deny. The command shows only if you turn that on for the machine."
                        )
                    )
                }
                TutorialPoint(
                    String(localized: "tutorial.approvals.unlock.title", defaultValue: "Approve asks you to unlock first"),
                    systemImage: "lock",
                    String(
                        localized: "tutorial.approvals.unlock.detail",
                        defaultValue: "On this device you can turn that off in Settings. A one-tap answer never picks \"always allow\"."
                    )
                )
                TutorialPoint(
                    String(localized: "tutorial.approvals.relay.title", defaultValue: "What the relay sees"),
                    systemImage: "arrow.left.arrow.right",
                    liveActivitiesAvailable
                        ? String(
                            localized: "tutorial.approvals.relay.detail",
                            defaultValue: "Pushes reach Apple through Togo's relay, which can't read notifications or your answers. It can read a Live Activity's status: the session number, agent, state, how many requests wait, when it started and the request's id."
                        )
                        : String(
                            localized: "tutorial.approvals.relay.detail.notification",
                            defaultValue: "Pushes reach Apple through Togo's relay, which can't read notifications or your answers. It sees only when they are sent and which device they go to."
                        )
                )
            }
        }
    }
}

// MARK: Sessions and review

struct SessionsPage: View {
    var body: some View {
        TutorialPageLayout(page: .sessions) {
            AgentMarks()
            VStack(alignment: .leading, spacing: 20) {
                TutorialPoint(
                    String(localized: "tutorial.sessions.start.title", defaultValue: "Start a session"),
                    systemImage: "plus.bubble",
                    String(
                        localized: "tutorial.sessions.start.detail",
                        defaultValue: "Pick a machine, a folder and an agent. It starts in tmux on the machine and opens here as a chat."
                    )
                )
                TutorialPoint(
                    String(localized: "tutorial.sessions.review.title", defaultValue: "Review changes"),
                    systemImage: "plus.forwardslash.minus",
                    String(
                        localized: "tutorial.sessions.review.detail",
                        defaultValue: "Read the diff, stage or discard files, commit, or comment on a line and send it to the agent."
                    )
                )
            }
        }
    }
}
