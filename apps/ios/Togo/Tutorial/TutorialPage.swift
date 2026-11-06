import SwiftUI

/// The guide's pages, in order. The raw value is what progress saves.
enum TutorialPage: Int, CaseIterable, Sendable {
    case welcome, setup, helper, privacy, session, ready

    var title: String {
        switch self {
        case .welcome: String(localized: "tutorial.welcome.title", defaultValue: "Your agents keep running on your machine")
        case .setup: String(localized: "tutorial.setup.title", defaultValue: "What the machine needs")
        case .helper: String(localized: "tutorial.helper.title", defaultValue: "With or without the helper")
        case .privacy: String(localized: "tutorial.privacy.title", defaultValue: "Notifications and privacy")
        case .session: String(localized: "tutorial.session.title", defaultValue: "Working in a session")
        case .ready: String(localized: "tutorial.ready.title", defaultValue: "Ready to add a machine")
        }
    }

    var message: String {
        switch self {
        case .welcome:
            String(
                localized: "tutorial.welcome.message",
                defaultValue: "Agents run in tmux on your Mac or Linux machine, not on the phone. Togo attaches to them over SSH or Mosh, and they keep going when you close the app."
            )
        case .setup:
            String(
                localized: "tutorial.setup.message",
                defaultValue: "Tick each one as you go. Togo also ticks them when it finds them on a machine you add."
            )
        case .helper:
            String(
                localized: "tutorial.helper.message",
                defaultValue: "Togo works over plain SSH. The helper is a small program on the machine that adds the rest."
            )
        case .privacy:
            String(
                localized: "tutorial.privacy.message",
                defaultValue: "What reaches your lock screen, and who can read it on the way."
            )
        case .session:
            String(
                localized: "tutorial.session.message",
                defaultValue: "A session is a real terminal. These keys and gestures make it work on a phone."
            )
        case .ready:
            String(
                localized: "tutorial.ready.message",
                defaultValue: "You need the machine's name or address and your user name on it. Or look around a demo machine first."
            )
        }
    }
}

/// One page of the guide: a title and a sentence, then the page's content, scrolling as one.
struct TutorialPageLayout<Content: View>: View {
    let page: TutorialPage
    @ViewBuilder var content: Content

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                VStack(alignment: .leading, spacing: 10) {
                    Text(page.title)
                        .font(.title.weight(.bold))
                        .foregroundStyle(Color.togoSnow)
                        .accessibilityAddTraits(.isHeader)
                    Text(page.message)
                        .font(.body)
                        .foregroundStyle(Color.togoMute)
                }
                content
            }
            .screenMargins()
            .padding(.top, 8)
            .padding(.bottom, 24)
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(Color.togoInk)
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

// MARK: Welcome

struct WelcomePage: View {
    let animated: Bool

    var body: some View {
        TutorialPageLayout(page: .welcome) {
            ConnectionDiagram(animated: animated)
            VStack(alignment: .leading, spacing: 20) {
                TutorialPoint(
                    String(localized: "tutorial.welcome.tmux.title", defaultValue: "Sessions live in tmux"),
                    systemImage: "rectangle.split.3x1",
                    String(
                        localized: "tutorial.welcome.tmux.detail",
                        defaultValue: "Each agent runs in its own tmux session, so it carries on when the phone sleeps or loses signal."
                    )
                )
                TutorialPoint(
                    String(localized: "tutorial.welcome.attach.title", defaultValue: "The phone attaches"),
                    systemImage: "iphone",
                    String(
                        localized: "tutorial.welcome.attach.detail",
                        defaultValue: "Open a session to see it as it is on the machine, type into it, or answer what the agent asked."
                    )
                )
            }
        }
    }
}

/// Phone, network, machine, tmux and agents, drawn in brand tokens. The link draws in and the
/// agents appear once; under Reduce Motion, or when not animated, it starts complete.
struct ConnectionDiagram: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let animated: Bool
    @State private var shown = false

    private var complete: Bool { shown || !animated || reduceMotion }

    var body: some View {
        HStack(alignment: .center, spacing: 0) {
            phone
            link
            machine
        }
        .dynamicTypeSize(...DynamicTypeSize.xLarge)
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(String(
            localized: "tutorial.welcome.diagram",
            defaultValue: "This phone connects over SSH or Mosh to your machine, where agents run inside tmux."
        ))
        .task {
            guard animated, !reduceMotion, !shown else { return }
            try? await Task.sleep(for: .milliseconds(250))
            withAnimation(Motion.animation(.dropIn, reduce: reduceMotion)) { shown = true }
        }
    }

    private var phone: some View {
        RoundedRectangle(cornerRadius: 12)
            .strokeBorder(Color.togoSnow, lineWidth: 2)
            .frame(width: 48, height: 88)
            .overlay(alignment: .top) {
                Capsule().fill(Color.togoSnow).frame(width: 14, height: 4).padding(.top, 8)
            }
    }

    private var link: some View {
        VStack(spacing: 6) {
            Line()
                .trim(from: 0, to: complete ? 1 : 0)
                .stroke(Color.togoMute, style: StrokeStyle(lineWidth: 2, lineCap: .round, dash: [2, 6]))
                .frame(height: 2)
            Text(String(localized: "tutorial.welcome.diagram.link", defaultValue: "SSH or Mosh"))
                .font(.caption)
                .foregroundStyle(Color.togoMute)
                .lineLimit(1)
                .fixedSize()
        }
        .frame(minWidth: 72, maxWidth: 110)
        .padding(.horizontal, 8)
    }

    private var machine: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(String(localized: "tutorial.welcome.diagram.machine", defaultValue: "Mac or Linux"))
                .font(.caption.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .lineLimit(1)
            VStack(alignment: .leading, spacing: 6) {
                Text(verbatim: "tmux")
                    .font(.caption2.monospaced())
                    .foregroundStyle(Color.togoMute)
                ForEach(Array(["claude", "codex", "shell"].enumerated()), id: \.offset) { index, agent in
                    Text(verbatim: agent)
                        .font(.caption2.monospaced())
                        .foregroundStyle(Color.togoSnow)
                        .lineLimit(1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 3)
                        .background(Color.togoGrouped, in: .rect(cornerRadius: 4))
                        .opacity(complete ? 1 : 0)
                        .offset(y: complete || reduceMotion ? 0 : 6)
                        .animation(Motion.animation(.dropIn, reduce: reduceMotion)?.delay(Double(index) * 0.12), value: complete)
                }
            }
            .padding(8)
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Color.togoLine))
        }
        .padding(10)
        .frame(width: 128)
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Color.togoSnow, lineWidth: 2))
    }
}

private struct Line: Shape {
    func path(in rect: CGRect) -> Path {
        Path { path in
            path.move(to: CGPoint(x: rect.minX, y: rect.midY))
            path.addLine(to: CGPoint(x: rect.maxX, y: rect.midY))
        }
    }
}

// MARK: Notifications and privacy

struct PrivacyPage: View {
    var body: some View {
        TutorialPageLayout(page: .privacy) {
            VStack(alignment: .leading, spacing: 24) {
                TutorialPoint(
                    String(localized: "tutorial.privacy.lockScreen.title", defaultValue: "On the lock screen"),
                    systemImage: "bell",
                    String(
                        localized: "tutorial.privacy.lockScreen.detail",
                        defaultValue: "A notification shows the whole request, so you can answer without opening Togo. It is end-to-end encrypted, and only this phone can read it."
                    )
                )
                TutorialPoint(
                    String(localized: "tutorial.privacy.liveActivity.title", defaultValue: "In a Live Activity"),
                    systemImage: "rectangle.badge.checkmark",
                    String(
                        localized: "tutorial.privacy.liveActivity.detail",
                        defaultValue: "A Live Activity shows only which session it is, its status and for how long. It shows what the agent wrote only if you turn that on."
                    )
                )
                TutorialPoint(
                    String(localized: "tutorial.privacy.relay.title", defaultValue: "On the way"),
                    systemImage: "lock",
                    String(
                        localized: "tutorial.privacy.relay.detail",
                        defaultValue: "Notifications pass through Togo's relay to reach Apple. The relay carries them sealed and never sees what they say."
                    )
                )
            }
        }
    }
}

// MARK: Ready

struct ReadyPage: View {
    static let demoTitle = String(localized: "tutorial.ready.demo", defaultValue: "Explore with a demo machine")
    /// Set at accessibility sizes, where the demo button moves from the bottom bar into the page.
    var onDemo: (() -> Void)?

    var body: some View {
        TutorialPageLayout(page: .ready) {
            VStack(alignment: .leading, spacing: 20) {
                TutorialPoint(
                    String(localized: "tutorial.ready.find.title", defaultValue: "Togo looks for machines nearby"),
                    systemImage: "dot.radiowaves.left.and.right",
                    String(
                        localized: "tutorial.ready.find.detail",
                        defaultValue: "Machines on the same network that offer SSH show up by name. You can also type an address."
                    )
                )
                TutorialPoint(
                    String(localized: "tutorial.ready.key.title", defaultValue: "This phone makes its own key"),
                    systemImage: "key",
                    String(
                        localized: "tutorial.ready.key.detail",
                        defaultValue: "The private half never leaves the phone. Togo shows the public half for you to add to the machine."
                    )
                )
                TutorialPoint(
                    String(localized: "tutorial.ready.later.title", defaultValue: "This guide stays in Settings"),
                    systemImage: "gearshape",
                    String(
                        localized: "tutorial.ready.later.detail",
                        defaultValue: "Open it again from Settings, Getting started."
                    )
                )
            }
            if let onDemo {
                Button(Self.demoTitle, action: onDemo)
                    .buttonStyle(.secondary)
            }
        }
    }
}
