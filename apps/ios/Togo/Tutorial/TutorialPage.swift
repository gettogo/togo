import SwiftUI

/// The guide's pages, in order. The raw value is what progress saves.
enum TutorialPage: Int, CaseIterable, Sendable {
    case welcome, setup, helper, privacy, session, ready

    var title: String {
        switch self {
        case .welcome: String(localized: "tutorial.welcome.title.short", defaultValue: "Your agents keep running")
        case .setup: String(localized: "tutorial.setup.title.short", defaultValue: "Get your machine ready")
        case .helper: String(localized: "tutorial.helper.title.short", defaultValue: "The helper adds the rest")
        case .privacy: String(localized: "tutorial.privacy.title.short", defaultValue: "Private by design")
        case .session: String(localized: "tutorial.session.title.short", defaultValue: "Made for touch")
        case .ready: String(localized: "tutorial.ready.title.short", defaultValue: "Add your first machine")
        }
    }

    /// One short sentence. Anything more sits behind Learn more on the page.
    var message: String {
        switch self {
        case .welcome:
            String(
                localized: "tutorial.welcome.message.short",
                defaultValue: "They run on your Mac or Linux machine, even when this app is closed."
            )
        case .setup:
            String(localized: "tutorial.setup.message.short", defaultValue: "Tick each step as you go.")
        case .helper:
            String(
                localized: "tutorial.helper.message.short",
                defaultValue: "Plain SSH works. The helper adds status, approvals and notifications."
            )
        case .privacy:
            String(
                localized: "tutorial.privacy.message.short",
                defaultValue: "Only this device can read your notifications."
            )
        case .session:
            String(
                localized: "tutorial.session.message.short",
                defaultValue: "Keys, gestures and a compose bar make the terminal easy to use."
            )
        case .ready:
            String(
                localized: "tutorial.ready.message.short",
                defaultValue: "You need its name or address and your user name on it."
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
                        Text(page.title)
                            .font(.title.weight(.bold))
                            .foregroundStyle(Color.togoSnow)
                            .accessibilityAddTraits(.isHeader)
                        Text(page.message)
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
                .disclosed(disclosure)
            }
        }
    }
}

// MARK: Welcome

struct WelcomePage: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let animated: Bool
    /// Opens Learn more, for snapshots.
    var expanded = false

    var body: some View {
        TutorialPageLayout(page: .welcome) {
            // At accessibility sizes the sentence says what the diagram shows, and the diagram
            // would push Learn more off the screen.
            if !dynamicTypeSize.isAccessibilitySize {
                ConnectionDiagram(animated: animated)
            }
            TutorialMore(expanded: expanded) {
                TutorialPoint(
                    String(localized: "tutorial.welcome.tmux.title", defaultValue: "Sessions live in tmux"),
                    systemImage: "rectangle.split.3x1",
                    String(
                        localized: "tutorial.welcome.tmux.detail",
                        defaultValue: "Each agent runs in its own tmux session, so it carries on when your device sleeps or loses signal."
                    )
                )
                TutorialPoint(
                    String(localized: "tutorial.welcome.attach.title", defaultValue: "Your device attaches"),
                    systemImage: DeviceName.symbol,
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

    /// The entrance waits this long, so it starts once the page has settled.
    static let entranceDelay = 0.25

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
            defaultValue: "This device connects over SSH or Mosh to your machine, where agents run inside tmux."
        ))
        .onAppear {
            guard animated, !reduceMotion, !shown else { return }
            // A first-run entrance: a beat after the page settles, the link draws in and the
            // agents reveal one after another (30 ms apart, 150 ms at most).
            withAnimation(Motion.animation(.progress, reduce: reduceMotion)?.delay(Self.entranceDelay)) { shown = true }
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
                // trunc-ok: the fixed words "SSH or Mosh" under the link, sized to fit
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
                .fixedSize(horizontal: false, vertical: true)
            VStack(alignment: .leading, spacing: 6) {
                Text(verbatim: "tmux")
                    .font(.caption2.monospaced())
                    .foregroundStyle(Color.togoMute)
                ForEach(Array(["claude", "codex", "shell"].enumerated()), id: \.offset) { index, agent in
                    Text(verbatim: agent)
                        .font(.caption2.monospaced())
                        .foregroundStyle(Color.togoSnow)
                        // trunc-ok: fixed words (claude, codex, shell) in the diagram
                        .lineLimit(1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 3)
                        .background(Color.togoGrouped, in: .rect(cornerRadius: 4))
                        .opacity(complete ? 1 : 0)
                        .offset(y: complete ? 0 : Motion.revealRise(reduce: reduceMotion))
                        // Each agent reveals on its own beat of the entrance's transaction.
                        .transaction(value: complete) { transaction in
                            guard transaction.animation != nil else { return }
                            transaction.animation = Motion.animation(.reveal, reduce: reduceMotion)?
                                .delay(Self.entranceDelay + Motion.revealDelay + Motion.staggerDelay(index))
                        }
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
    @Environment(\.liveActivitiesAvailable) private var liveActivitiesAvailable
    /// Opens Learn more, for snapshots.
    var expanded = false

    var body: some View {
        TutorialPageLayout(page: .privacy) {
            PrivacyMark()
            TutorialMore(expanded: expanded) {
                TutorialPoint(
                    String(localized: "tutorial.privacy.lockScreen.title", defaultValue: "On the lock screen"),
                    systemImage: "bell",
                    String(
                        localized: "tutorial.privacy.lockScreen.detail",
                        defaultValue: "A notification shows the whole request, so you can answer without opening Togo. It is end-to-end encrypted, and only this device can read it."
                    )
                )
                if liveActivitiesAvailable {
                    TutorialPoint(
                        String(localized: "tutorial.privacy.liveActivity.title", defaultValue: "In a Live Activity"),
                        systemImage: "rectangle.badge.checkmark",
                        String(
                            localized: "tutorial.privacy.liveActivity.detail",
                            defaultValue: "A Live Activity shows only which session it is, its status and for how long. It shows what the agent wrote only if you turn that on."
                        )
                    )
                }
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
            NearbyMark()
            if let onDemo {
                Button(Self.demoTitle, action: onDemo)
                    .buttonStyle(.secondary)
            }
        }
    }
}

/// A page's single symbol, large and quiet, for pages whose idea has no diagram.
struct TutorialMark: View {
    let systemImage: String
    let label: String

    var body: some View {
        Image(systemName: systemImage)
            .font(.system(size: 44, weight: .regular))
            .foregroundStyle(Color.togoSnow)
            .frame(width: 112, height: 112)
            .overlay(Circle().strokeBorder(Color.togoLine, lineWidth: 2))
            .frame(maxWidth: .infinity)
            .padding(.vertical, 8)
            .accessibilityElement()
            .accessibilityLabel(label)
    }
}

struct PrivacyMark: View {
    var body: some View {
        TutorialMark(
            systemImage: "lock.shield",
            label: String(localized: "tutorial.privacy.mark", defaultValue: "A lock: notifications are sealed end to end.")
        )
    }
}

struct NearbyMark: View {
    var body: some View {
        TutorialMark(
            systemImage: "dot.radiowaves.left.and.right",
            label: String(localized: "tutorial.ready.mark", defaultValue: "Togo looks for machines nearby.")
        )
    }
}
