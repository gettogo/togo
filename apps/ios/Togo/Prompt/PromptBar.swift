import SwiftUI

/// The one floating bar: it holds whatever an agent needs from the user, and when nothing waits
/// it is where the user types into the open terminal or starts a session. System glass at
/// radius 28; a waiting request brings a faint beacon light into it.
///
/// A new request rises in: the old content settles down and out, the new content rises from
/// below and the bar's height follows. The owner places the bar and keeps `model.mode` current.
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
    @State private var rising: Task<Void, Never>?

    static let radius: CGFloat = 28
    /// How far the old content settles, and how far below the new content starts.
    static let settleDistance: CGFloat = 14
    static let riseDistance: CGFloat = 26
    /// How long the old content settles before the new one rises.
    static let settlePause: Duration = .milliseconds(150)

    init(model: PromptBarModel, maxHeight: CGFloat? = nil) {
        self.model = model
        self.maxHeight = maxHeight
    }

    var body: some View {
        let stage = shown ?? model.stage
        ZStack(alignment: .bottom) {
            if stage != .hidden {
                bar(stage)
                    .transition(reduceMotion ? .opacity : .move(edge: .bottom).combined(with: .opacity))
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
            .id(Self.contentID(stage))
            // The old content has already settled out by the time it is replaced, so it leaves at
            // once and the bar sizes to the new content alone.
            .transition(reduceMotion ? .opacity : .identity)
            .offset(y: contentOffset)
            .opacity(contentOpacity)
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
        let measured = content.onGeometryChange(for: CGFloat.self) { $0.size.height } action: { contentHeight = $0 }
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

    private func move(to new: PromptBarModel.Stage) {
        guard let current = shown else {
            shown = new
            return
        }
        rising?.cancel()
        rising = nil
        switch PromptBarModel.change(from: current, to: new) {
        case .none:
            settle(new, role: nil)
        case .resize:
            settle(new, role: .chrome)
        case let .rise(role):
            if reduceMotion || current == .hidden || new == .hidden {
                settle(new, role: role)
                return
            }
            rising = Task { @MainActor in
                withAnimation(Motion.animation(.collapse, reduce: false)) {
                    contentOffset = Self.settleDistance
                    contentOpacity = 0
                }
                try? await Task.sleep(for: Self.settlePause)
                guard !Task.isCancelled else { return }
                var still = Transaction()
                still.disablesAnimations = true
                withTransaction(still) { contentOffset = Self.riseDistance }
                // One frame at the starting point, so the rise begins below rather than where
                // the old content settled.
                try? await Task.sleep(for: .milliseconds(16))
                guard !Task.isCancelled else { return }
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
            withAnimation(Motion.animation(role, reduce: reduceMotion), apply)
        } else {
            apply()
        }
    }
}
