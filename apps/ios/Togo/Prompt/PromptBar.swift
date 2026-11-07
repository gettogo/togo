import SwiftUI

/// The one floating bar: it holds whatever an agent needs from the user, and when nothing waits
/// it is where the user types into the open terminal or starts a session. System glass at
/// radius 28; a waiting request brings a faint beacon light into it.
///
/// A new request rises in: the old content conceals as it settles down, and when it has gone
/// the new content rises from below on `dropIn` while the bar's height follows in the same
/// transaction (motion spec §2.3). The owner places the bar and keeps `model.mode` current.
struct PromptBar: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.colorScheme) private var colorScheme
    let model: PromptBarModel
    /// The most the bar may take, about half its window; past it the content scrolls.
    var maxHeight: CGFloat?
    @State private var contentHeight: CGFloat = 0
    /// The stage on screen, which trails `model.stage` while a change plays.
    @State private var shown: PromptBarModel.Stage?
    @State private var contentOffset: CGFloat = 0
    @State private var contentOpacity: Double = 1
    /// Counts swaps, so a stage that arrives mid-swap wins and the older rise never lands.
    @State private var swaps = PromptBarSwap()

    static let radius: CGFloat = 28
    /// How far the old content settles, and how far below the new content starts.
    static let settleDistance: CGFloat = 14
    static let riseDistance: CGFloat = 26

    init(model: PromptBarModel, maxHeight: CGFloat? = nil) {
        self.model = model
        self.maxHeight = maxHeight
    }

    var body: some View {
        let stage = shown ?? model.stage
        ZStack(alignment: .bottom) {
            if stage != .hidden {
                bar(stage)
                    // motion: origin — the bar presents from the bottom edge it lives on, only when its first request is news.
                    .transition(.asymmetric(
                        insertion: Motion.present(from: .bottom, reduce: reduceMotion),
                        removal: Motion.conceal(reduce: reduceMotion)
                    ))
            }
        }
        .frame(maxWidth: .infinity)
        .background { keyAnswers }
        .onChange(of: model.stage, initial: true) { _, new in move(to: new) }
        .haptic(.connect, trigger: model.arrivals, change: { old, new in
            new > old && scenePhase == .active
        })
    }

    private func bar(_ stage: PromptBarModel.Stage) -> some View {
        capped(content(stage))
            // The outgoing content's settle. Inside the identity, so new content starts in place
            // and only its own insertion moves it.
            .offset(y: contentOffset)
            .opacity(contentOpacity)
            .id(Self.contentID(stage))
            // The old content has already concealed by the time it is replaced, so it leaves at
            // once; the new content rises from below while the bar sizes to it.
            // motion: continuity — the next request takes the answered one's place in the same bar.
            .transition(Self.contentTransition(reduce: reduceMotion))
            .frame(maxWidth: .infinity, alignment: .leading)
            .modifier(PromptRequestLight(on: stage.request != nil, radius: Self.radius))
            .clipShape(.rect(cornerRadius: Self.radius, style: .continuous))
            .glassEffect(glass, in: .rect(cornerRadius: Self.radius, style: .continuous))
    }

    @ViewBuilder private func content(_ stage: PromptBarModel.Stage) -> some View {
        switch stage {
        case let .request(request, position, count):
            PromptRequestView(model: model, request: request, position: position, count: count, replying: false)
        case let .replying(request, position, count):
            PromptRequestView(model: model, request: request, position: position, count: count, replying: true)
        case let .input(tab):
            PromptInputView(model: model, tab: tab)
        case let .start(expanded):
            PromptStartView(model: model, expanded: expanded)
        case .hidden:
            EmptyView()
        }
    }

    /// The content at its own height, or scrolling in `maxHeight` once it would pass it.
    @ViewBuilder private func capped(_ content: some View) -> some View {
        // The measured height only picks plain or scrolling; it never rides an animation, so it
        // cannot feed back into the layout that is moving (spec §4).
        let measured = content.onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height in
            var still = Transaction()
            still.disablesAnimations = true
            withTransaction(still) { contentHeight = height }
        }
        if let maxHeight, contentHeight > maxHeight {
            ScrollView { measured }
                .scrollBounceBehavior(.basedOnSize)
                .frame(height: maxHeight)
        } else {
            measured
        }
    }

    /// The content's identity: a request keeps its own through replying, so its beacon and
    /// question stay put while the answers turn into a reply line.
    static func contentID(_ stage: PromptBarModel.Stage) -> String {
        switch stage {
        case let .request(request, _, _), let .replying(request, _, _): "request-\(request.id)"
        case let .input(tab): "input-\(tab.hostID.uuidString)-\(tab.sessionID)"
        case .start: "start"
        case .hidden: "hidden"
        }
    }

    /// Bottom-accessory glass, tinted toward the raised surface in light mode and the grouped
    /// one in dark, as the bar is drawn in the design.
    private var glass: Glass {
        colorScheme == .dark
            ? .regular.tint(Color.togoGrouped.opacity(0.5))
            : .regular.tint(Color.togoRaised.opacity(0.6))
    }

    /// y, n and r on a hardware keyboard answer the request on screen, while no field holds
    /// the keyboard, the bar's window is the active one and the request has been there long
    /// enough to read. Each names the request drawn, so one mid-rise is not answered.
    @ViewBuilder private var keyAnswers: some View {
        if model.acceptsKeyAnswers, scenePhase == .active, let id = (shown ?? model.stage).request?.id {
            ZStack {
                Button { model.answer(.yes, to: id) } label: { EmptyView() }
                    .keyboardShortcut("y", modifiers: [])
                Button { model.answer(.no, to: id) } label: { EmptyView() }
                    .keyboardShortcut("n", modifiers: [])
                Button { model.beginReply() } label: { EmptyView() }
                    .keyboardShortcut("r", modifiers: [])
            }
            .frame(width: 0, height: 0)
            .opacity(0)
            .accessibilityHidden(true)
        }
    }

    /// New content rises in from below; old content, already concealed, leaves at once. Under
    /// Reduce Motion both crossfade.
    static func contentTransition(reduce: Bool) -> AnyTransition {
        reduce ? .opacity : .asymmetric(insertion: .offset(y: riseDistance).combined(with: .opacity), removal: .identity)
    }

    private func move(to new: PromptBarModel.Stage) {
        guard let current = shown else {
            shown = new
            return
        }
        let swap = swaps.begin()
        switch PromptBarSwap.plan(PromptBarModel.change(from: current, to: new), from: current, to: new, reduce: reduceMotion) {
        case .instant:
            settle(new, role: nil)
        case let .inPlace(role):
            settle(new, role: role)
        case let .concealThenRise(role):
            // The old content settles down and out on `conceal`; when it has gone, the newest
            // stage rises in on `role` and the bar's height follows in the same transaction.
            // motion: continuity — the answered request settles out before the next one takes its place.
            withAnimation(Motion.animation(.conceal, reduce: false), completionCriteria: .logicallyComplete) {
                contentOffset = Self.settleDistance
                contentOpacity = 0
            } completion: {
                guard swaps.isCurrent(swap) else { return }
                // motion: continuity — the newest stage comes in as the bar sizes to it, in one transaction.
                withAnimation(Motion.animation(role, reduce: false)) {
                    shown = model.stage
                    contentOffset = 0
                    contentOpacity = 1
                }
            }
        }
    }

    /// Lands on `stage`, animated on `role` when there is one, with the content back in place.
    private func settle(_ stage: PromptBarModel.Stage, role: MotionRole?) {
        let apply = {
            shown = stage
            contentOffset = 0
            contentOpacity = 1
        }
        if let role {
            // motion: state change — the bar moves to its next stage on the role the change calls for.
            withAnimation(Motion.animation(role, reduce: reduceMotion), apply)
        } else {
            apply()
        }
    }
}

/// How the bar moves from one stage to the next, apart from the view so it can be tested.
struct PromptBarSwap: Equatable {
    enum Plan: Equatable {
        /// Nothing the user would see as a new state: no animation.
        case instant
        /// The content changes in place on `role` and the bar's height follows.
        case inPlace(MotionRole)
        /// The old content conceals first, then the new rises in on `role` (§2.3).
        case concealThenRise(MotionRole)
    }

    private(set) var generation = 0

    /// Starts a swap and returns its number; any earlier swap's completion is now stale.
    mutating func begin() -> Int {
        generation += 1
        return generation
    }

    func isCurrent(_ swap: Int) -> Bool { swap == generation }

    /// The bar appearing or leaving, and every change under Reduce Motion, is one transaction
    /// (the bar's own present/conceal, or a crossfade); a request replacing what was there
    /// conceals before it rises.
    static func plan(
        _ change: PromptBarModel.Change, from old: PromptBarModel.Stage, to new: PromptBarModel.Stage, reduce: Bool
    ) -> Plan {
        switch change {
        case .none: return .instant
        case .resize: return .inPlace(.chrome)
        case let .rise(role):
            if reduce || old == .hidden || new == .hidden { return .inPlace(role) }
            return .concealThenRise(role)
        }
    }
}
