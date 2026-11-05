import SwiftUI

/// What a person sees before any machine exists: what Togo does and the one way to start.
/// Given the guide's model, it opens the guide on first launch and offers it again below, so
/// do not also use `tutorialOnFirstLaunch` around it.
struct FirstRunView: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let tutorial: TutorialModel?
    let onAddMachine: () -> Void
    @State private var guideShown = false

    init(tutorial: TutorialModel? = nil, onAddMachine: @escaping () -> Void) {
        self.tutorial = tutorial
        self.onAddMachine = onAddMachine
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                VStack(alignment: .leading, spacing: 12) {
                    Text(verbatim: "Togo")
                        .font(.largeTitle.weight(.bold))
                        .foregroundStyle(Color.togoSnow)
                        .accessibilityAddTraits(.isHeader)
                    Text(String(
                        localized: "onboard.firstRun.message",
                        defaultValue: "See the coding agents running on your machines and answer them from here."
                    ))
                    .font(.title3)
                    .foregroundStyle(Color.togoSnow)
                }
                VStack(alignment: .leading, spacing: 16) {
                    step(String(localized: "onboard.firstRun.step.find", defaultValue: "Pick a Mac or Linux machine you reach over SSH."), "laptopcomputer")
                    step(String(localized: "onboard.firstRun.step.key", defaultValue: "Check its key and sign in with a key made on this phone."), "key")
                    step(String(localized: "onboard.firstRun.step.helper", defaultValue: "Add the Togo helper for status, requests and notifications."), "bell")
                }
            }
            .screenMargins()
            .padding(.top, dynamicTypeSize.isAccessibilitySize ? 24 : 72)
        }
        .scrollBounceBehavior(.basedOnSize)
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(spacing: 4) {
                Button(String(localized: "onboard.firstRun.add", defaultValue: "Add a machine"), action: onAddMachine)
                    .buttonStyle(.primary)
                if tutorial != nil {
                    Button(String(localized: "tutorial.firstRun.open", defaultValue: "How Togo works"), action: openGuide)
                        .buttonStyle(.secondary)
                }
            }
            .screenMargins()
            .padding(.vertical, 12)
            .background(Color.togoInk)
        }
        .background(Color.togoInk)
        .modifier(GuideCover(tutorial: tutorial, shown: $guideShown, onAddMachine: onAddMachine))
    }

    private func openGuide() {
        if let tutorial, !tutorial.showsOnLaunch {
            tutorial.go(to: .welcome)
        }
        guideShown = true
    }

    private func step(_ text: String, _ systemImage: String) -> some View {
        Label {
            Text(text)
                .font(.body)
                .foregroundStyle(Color.togoMute)
        } icon: {
            Image(systemName: systemImage)
                .foregroundStyle(Color.togoSnow)
                .frame(width: 28)
        }
    }
}

/// The guide over the first-run screen, opened by itself until it has been finished or skipped.
private struct GuideCover: ViewModifier {
    let tutorial: TutorialModel?
    @Binding var shown: Bool
    let onAddMachine: () -> Void

    func body(content: Content) -> some View {
        if let tutorial {
            content
                .onAppear { if tutorial.showsOnLaunch { shown = true } }
                .tutorialCover(tutorial, isPresented: $shown, onAddMachine: onAddMachine)
        } else {
            content
        }
    }
}
