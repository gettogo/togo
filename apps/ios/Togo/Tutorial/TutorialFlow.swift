import SwiftUI

/// How the guide was left. Adding a machine opens a sheet, which the presenter shows once the
/// guide has finished dismissing.
enum TutorialExit: Equatable, Sendable {
    case closed, addMachine
}

/// The guide: six pages that swipe or step with Back and Continue, Skip on every page, and the
/// last page's way into adding a machine or the demo. Present it full screen and dismiss it
/// when `onClose` is called.
struct TutorialFlow: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Bindable var model: TutorialModel
    /// False draws the diagram and terminal demo finished and still, for snapshots.
    var animated = true
    /// A fixed clock for the demo machine in snapshots; nil follows the time of day.
    var now: Date?
    let onClose: (TutorialExit) -> Void

    @State private var demo: AppStore?

    var body: some View {
        NavigationStack {
            TabView(selection: selection) {
                ForEach(TutorialPage.allCases, id: \.self) { page in
                    content(page).tag(page)
                }
            }
            .tabViewStyle(.page(indexDisplayMode: .never))
            .background(Color.togoInk)
            .safeAreaInset(edge: .bottom, spacing: 0) { controls }
            .toolbar { toolbar }
            .navigationBarTitleDisplayMode(.inline)
        }
        .fullScreenCover(item: demoItem) { item in
            DemoControlCenter(store: item.store, now: now) { demo = nil }
        }
        .togoControls()
    }

    private var selection: Binding<TutorialPage> {
        Binding { model.page } set: { model.go(to: $0) }
    }

    @ViewBuilder private func content(_ page: TutorialPage) -> some View {
        switch page {
        case .welcome: WelcomePage(animated: animated)
        case .setup: SetupPage(model: model)
        case .helper: HelperPage()
        case .privacy: PrivacyPage()
        case .session: SessionPage(playing: Self.playsSessionDemo(on: model.page, animated: animated))
        case .ready: ReadyPage(onDemo: demoInPage)
        }
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        if model.page != .welcome {
            ToolbarItem(placement: .topBarLeading) {
                Button(String(localized: "tutorial.back", defaultValue: "Back"), systemImage: "chevron.left") {
                    step(model.back)
                }
                .foregroundStyle(Color.togoSnow)
            }
        }
        ToolbarItem(placement: .topBarTrailing) {
            Button(String(localized: "tutorial.skip", defaultValue: "Skip")) {
                model.skip()
                onClose(.closed)
            }
            .foregroundStyle(Color.togoSnow)
        }
    }

    private var controls: some View {
        VStack(spacing: 16) {
            PageIndicator(page: model.page)
            VStack(spacing: 4) {
                if model.page == .ready {
                    Button(String(localized: "tutorial.ready.add", defaultValue: "Add your first machine")) {
                        model.finish()
                        onClose(.addMachine)
                    }
                    .buttonStyle(.primary)
                    // At accessibility sizes the page carries this button, so the bar stays short.
                    if !dynamicTypeSize.isAccessibilitySize {
                        Button(ReadyPage.demoTitle, action: openDemo)
                            .buttonStyle(.secondary)
                    }
                } else {
                    Button(String(localized: "tutorial.continue", defaultValue: "Continue")) { step(model.next) }
                        .buttonStyle(.primary)
                }
            }
        }
        .screenMargins()
        .padding(.top, 12)
        .padding(.bottom, 8)
        .background(Color.togoInk)
    }

    /// The session demo loops only while its page is the one on screen.
    static func playsSessionDemo(on page: TutorialPage, animated: Bool) -> Bool {
        animated && page == .session
    }

    private var demoInPage: (() -> Void)? {
        guard dynamicTypeSize.isAccessibilitySize else { return nil }
        return openDemo
    }

    private func openDemo() {
        demo = DemoMachine.store(now: now ?? .now)
    }

    /// Under Reduce Motion the page changes in place: a paged TabView slides with any animation.
    private func step(_ move: () -> Void) {
        if reduceMotion {
            var transaction = Transaction()
            transaction.disablesAnimations = true
            withTransaction(transaction, move)
        } else {
            withAnimation(Motion.animation(.tabSettle, reduce: reduceMotion), move)
        }
    }

    private var demoItem: Binding<DemoItem?> {
        Binding { demo.map(DemoItem.init) } set: { if $0 == nil { demo = nil } }
    }

    private struct DemoItem: Identifiable {
        let store: AppStore
        var id: ObjectIdentifier { ObjectIdentifier(store) }
    }
}

/// Where the user is in the guide, as short bars: the current one long and snow, the rest
/// short hairlines. VoiceOver reads it as "Page 2 of 6".
struct PageIndicator: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let page: TutorialPage

    var body: some View {
        HStack(spacing: 6) {
            ForEach(TutorialPage.allCases, id: \.self) { item in
                Capsule()
                    .fill(item == page ? Color.togoSnow : Color.togoLine)
                    .frame(width: item == page ? 20 : 8, height: 4)
            }
        }
        .motion(.tabSettle, value: page)
        .frame(minHeight: 8)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(String(
            localized: "tutorial.pageIndicator",
            defaultValue: "Page \(page.rawValue + 1) of \(TutorialPage.allCases.count)"
        ))
    }
}

extension View {
    /// Shows the guide full screen on first launch, until it is finished or skipped. Use this or
    /// `FirstRunView(tutorial:)`, never both: each would present the guide on its own.
    func tutorialOnFirstLaunch(_ model: TutorialModel, onAddMachine: @escaping () -> Void) -> some View {
        modifier(FirstLaunchTutorial(model: model, onAddMachine: onAddMachine))
    }

    /// The guide full screen while `isPresented` is true. "Getting started" calls
    /// `TutorialModel.reopen()` first, so it opens on the first page.
    func tutorialCover(
        _ model: TutorialModel, isPresented: Binding<Bool>, onAddMachine: @escaping () -> Void
    ) -> some View {
        modifier(TutorialCover(model: model, isPresented: isPresented, onAddMachine: onAddMachine))
    }
}

private struct FirstLaunchTutorial: ViewModifier {
    let model: TutorialModel
    let onAddMachine: () -> Void
    @State private var presented = false

    func body(content: Content) -> some View {
        content
            .onAppear { presented = model.showsOnLaunch }
            .tutorialCover(model, isPresented: $presented, onAddMachine: onAddMachine)
    }
}

private struct TutorialCover: ViewModifier {
    let model: TutorialModel
    @Binding var isPresented: Bool
    let onAddMachine: () -> Void
    @State private var exit = TutorialExit.closed

    func body(content: Content) -> some View {
        content
            .fullScreenCover(isPresented: $isPresented) {
                if exit == .addMachine { onAddMachine() }
                exit = .closed
            } content: {
                TutorialFlow(model: model) { how in
                    exit = how
                    isPresented = false
                }
            }
    }
}
