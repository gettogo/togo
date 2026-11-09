import SwiftUI

/// How the guide was left. Adding a machine opens a sheet, which the presenter shows once the
/// guide has finished dismissing.
enum TutorialExit: Equatable, Sendable {
    case closed, addMachine
}

/// The guide: four pages that swipe or step with Back and Continue, Skip on every page, and the
/// last page's way into adding a machine or the demo. Present it full screen and dismiss it
/// when `onClose` is called.
struct TutorialFlow: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Bindable var model: TutorialModel
    /// A fixed clock for the Lock Screen picture and the demo machine in snapshots; nil follows
    /// the time of day.
    var now: Date?
    let onClose: (TutorialExit) -> Void

    @State private var demo: AppStore?
    /// The last page's buttons as the bar shows them. A swipe moves the page with no
    /// transaction, so the bar follows here: Continue gives way to the last page's two buttons
    /// at once, and back (buttons are controls, not news).
    @State private var lastFollowed: Bool?

    private static let lastPage = TutorialPage.allCases.last ?? .setup
    private var lastShown: Bool { lastFollowed ?? (model.page == Self.lastPage) }

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
            .onChange(of: model.page == Self.lastPage) { _, last in
                withTransaction(Transaction(animation: nil)) { lastFollowed = last }
            }
            .toolbar { toolbar }
            .navigationBarTitleDisplayMode(.inline)
        }
        // A cover is a new root, so it may open over the guide wherever the guide is a sheet.
        .presentCover("tutorial.demo", item: demoItem, nests: true) { item in
            DemoControlCenter(store: item.store, now: now) { demo = nil }
        }
        .togoControls()
        // Skip and the last page's buttons are the ways out, as a cover and as a page sheet.
        .interactiveDismissDisabled()
    }

    private var selection: Binding<TutorialPage> {
        Binding { model.page } set: { model.go(to: $0) }
    }

    @ViewBuilder private func content(_ page: TutorialPage) -> some View {
        switch page {
        case .welcome: WelcomePage()
        case .approvals: ApprovalsPage(now: now)
        case .sessions: SessionsPage()
        case .setup: SetupPage(model: model, onDemo: demoInPage)
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
            Button(String(localized: "tutorial.skip", defaultValue: "Skip"), systemImage: "forward.end") {
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
                if lastShown {
                    Group {
                        Button(String(localized: "tutorial.ready.add", defaultValue: "Add your first machine")) {
                            model.finish()
                            onClose(.addMachine)
                        }
                        .buttonStyle(.primary)
                        // At accessibility sizes the page carries this button, so the bar stays short.
                        if !dynamicTypeSize.isAccessibilitySize {
                            Button(SetupPage.demoTitle, action: openDemo)
                                .buttonStyle(.secondary)
                        }
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
            // motion: continuity — the next page slides in from where the swipe would bring it
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
        // motion: continuity — the long bar travels with the page the user is on
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

    /// The guide while `isPresented` is true: full screen in a compact window, a page sheet in a
    /// wide one (`PresentationContent.walkthrough`). "Getting started" calls
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
            .harnessPresent(isPresented: $isPresented, as: .walkthrough, name: "tutorial.cover") {
                if exit == .addMachine { onAddMachine() }
                exit = .closed
            } body: {
                TutorialFlow(model: model) { how in
                    exit = how
                    isPresented = false
                }
            }
    }
}
