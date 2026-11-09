import SwiftUI

/// What a person sees before any machine exists: the mark, what Togo is for, and the one way to
/// start. The guide is there for whoever asks for it; nothing opens by itself.
struct FirstRunView: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let tutorial: TutorialModel?
    /// True while the launch is on screen. Its mark moves into this screen's mark on the way
    /// out, so this one waits; under Reduce Motion the launch cross-fades to it instead.
    var launchPlaying = false
    let onAddMachine: () -> Void
    @State private var guideShown = false
    /// The read-only demo machine, for anyone without a machine at hand (App Review included).
    @State private var demo: DemoItem?

    /// The side of the mark's box here. The launch shrinks into it.
    static let markSide: CGFloat = 120

    init(tutorial: TutorialModel? = nil, launchPlaying: Bool = false, onAddMachine: @escaping () -> Void) {
        self.tutorial = tutorial
        self.launchPlaying = launchPlaying
        self.onAddMachine = onAddMachine
    }

    var body: some View {
        GeometryReader { proxy in
            ScrollView {
                VStack(spacing: dynamicTypeSize.isAccessibilitySize ? 16 : 24) {
                    LaunchMark(side: Self.markSide)
                        .launchTarget()
                        .opacity(launchPlaying && !reduceMotion ? 0 : 1)
                        .accessibilityElement()
                        .accessibilityLabel(Text(verbatim: "Togo"))
                        .accessibilityAddTraits([.isHeader, .isImage])
                    VStack(spacing: 8) {
                        Text(String(localized: "onboard.firstRun.line", defaultValue: "Your coding agents, from anywhere."))
                            .font(.title2.weight(.semibold))
                            .foregroundStyle(Color.togoSnow)
                        Text(String(
                            localized: "onboard.firstRun.detail",
                            defaultValue: "Chat with Claude Code, Codex, OpenCode and more on your Mac or Linux machine, and answer their requests from the Lock Screen."
                        ))
                        .font(.body)
                        .foregroundStyle(Color.togoMute)
                    }
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                }
                .screenMargins()
                .padding(.vertical, 24)
                .frame(maxWidth: .infinity, minHeight: proxy.size.height)
            }
            .scrollBounceBehavior(.basedOnSize)
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(spacing: 4) {
                Button(String(localized: "onboard.firstRun.add", defaultValue: "Add a machine"), action: onAddMachine)
                    .buttonStyle(.primary)
                if tutorial != nil {
                    Button(String(localized: "tutorial.firstRun.open", defaultValue: "How it works"), action: openGuide)
                        .buttonStyle(.secondary)
                }
                Button(SetupPage.demoTitle) {
                    demo = DemoItem(store: DemoMachine.store())
                }
                .buttonStyle(.secondary)
            }
            .screenMargins()
            .padding(.vertical, 12)
            .background(Color.togoInk)
        }
        .background(Color.togoInk)
        .modifier(GuideCover(tutorial: tutorial, shown: $guideShown, onAddMachine: onAddMachine))
        .presentCover("firstRun.demo", item: $demo) { item in
            DemoControlCenter(store: item.store) { demo = nil }
        }
    }

    private struct DemoItem: Identifiable {
        let store: AppStore
        var id: ObjectIdentifier { ObjectIdentifier(store) }
    }

    private func openGuide() {
        if let tutorial, !tutorial.showsOnLaunch {
            tutorial.go(to: .welcome)
        }
        guideShown = true
    }
}

/// The guide over the first-run screen, only once it is asked for.
private struct GuideCover: ViewModifier {
    let tutorial: TutorialModel?
    @Binding var shown: Bool
    let onAddMachine: () -> Void

    func body(content: Content) -> some View {
        if let tutorial {
            content.tutorialCover(tutorial, isPresented: $shown, onAddMachine: onAddMachine)
        } else {
            content
        }
    }
}
