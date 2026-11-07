import SwiftUI
import TogoKit

extension View {
    /// Presents `store`'s mission control while `store.showsMission` is set: a sheet with
    /// medium and large detents that leaves the chat usable behind it on a compact width, a
    /// trailing inspector beside the chat on a regular one, open by default where both fit.
    /// Diffs and previews leave through `openExternal`; a sheet closes first so the review can
    /// take the screen.
    func missionControl(
        _ store: HarnessSessionStore?, actions: ChatActions = .inert,
        openExternal: @escaping (HarnessOpen) -> Void = { _ in }
    ) -> some View {
        modifier(MissionControlPresenter(store: store, actions: actions, openExternal: openExternal))
    }
}

/// Mission control for the workspace's focused tab: a sheet on a compact width, an inspector
/// beside the panes on iPad and an unfolded Duo. Its targets leave through
/// `AppStore.followMissionTarget`.
struct FocusedMissionControl: ViewModifier {
    let app: AppStore

    func body(content: Content) -> some View {
        let tab = app.focusedTab?.id
        content.missionControl(
            tab.flatMap { app.harness(for: $0) },
            actions: tab.map { app.chatActions(for: $0) } ?? .inert,
            openExternal: { [app] target in
                guard let tab else { return }
                app.followMissionTarget(target, from: tab)
            }
        )
    }
}

private struct MissionControlPresenter: ViewModifier {
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.windowWidth) private var windowWidth
    let store: HarnessSessionStore?
    let actions: ChatActions
    let openExternal: (HarnessOpen) -> Void

    @State private var detent: PresentationDetent = .medium
    /// A diff or preview asked for from the sheet, opened once the sheet has gone.
    @State private var pending: HarnessOpen?
    /// The workspace's width with the inspector's column and the bars included.
    @State private var width: CGFloat = 0

    private var presentation: MissionPresentation { MissionPresentation.choose(sizeClass, windowWidth: windowWidth) }

    /// What decides whether the inspector opens by itself.
    private struct DefaultKey: Equatable {
        var session: String?
        var presentation: MissionPresentation
        var wide: Bool
        var hasMission: Bool
    }

    private var defaultKey: DefaultKey {
        DefaultKey(
            session: store.map { MissionInspectorMemory.key($0.subject) }, presentation: presentation,
            wide: width >= MissionPlacement.openByDefaultWidth, hasMission: store?.hasMission ?? false
        )
    }

    func body(content: Content) -> some View {
        content
            // A side context: it steps aside for a sheet asked for from the chat behind it.
            .presentSheet("mission.sheet", isPresented: shown(.sheet), yields: true, onDismiss: flushPending) {
                if let store {
                    stack(store, inSheet: true)
                        // The side context's medium sheet (`AdaptivePresentation`), with the
                        // chat still usable behind it at the medium height.
                        .presentationDetents([.medium, .large], selection: $detent)
                        .presentationBackgroundInteraction(.enabled(upThrough: .medium))
                        .presentationContentInteraction(.scrolls)
                        .presentationDragIndicator(.visible)
                }
            }
            .coordinated("mission.inspector", .inspector, isPresented: shown(.inspector)) { view, shown in
                view.inspector(isPresented: shown) {
                    if let store {
                        stack(store, inSheet: false)
                            .inspectorColumnWidth(min: 280, ideal: MissionPlacement.inspectorIdeal, max: 440)
                    }
                }
            }
            .onGeometryChange(for: CGFloat.self) { proxy in
                proxy.size.width + proxy.safeAreaInsets.leading + proxy.safeAreaInsets.trailing
            } action: { width = $0 }
            .onChange(of: presentation) { old, new in
                // Folding a Duo with the inspector open does not raise a sheet nobody asked for.
                if old == .inspector, new == .sheet, let store, store.showsMission { store.showsMission = false }
            }
            .onChange(of: defaultKey, initial: true) { _, key in
                guard let store, !store.showsMission else { return }
                let closed = MissionInspectorMemory.shared.closedByUser(store.subject)
                if MissionPlacement.opensByDefault(key.presentation, width: width, hasMission: key.hasMission, closedByUser: closed) {
                    store.showsMission = true
                }
            }
    }

    private func shown(_ kind: MissionPresentation) -> Binding<Bool> {
        Binding(
            get: { presentation == kind && store?.showsMission == true },
            set: { shown in
                // A size class change hands the open state over to the other presentation, so
                // only a close from this one's own presentation clears it.
                guard presentation == kind, let store, store.showsMission != shown else { return }
                store.showsMission = shown
                MissionInspectorMemory.shared.record(shown: shown, for: store.subject, in: kind)
                if !shown { detent = .medium }
            }
        )
    }

    private func stack(_ store: HarnessSessionStore, inSheet: Bool) -> some View {
        MissionControlStack(
            store: store, actions: actions,
            openExternal: { target in
                if inSheet {
                    pending = target
                    store.showsMission = false
                } else {
                    openExternal(target)
                }
            },
            showChat: { needs in
                if inSheet { store.showsMission = false }
                if store.chatAvailable, needs != nil || store.oldestOpenNeeds != nil {
                    store.revealNeeds(id: needs)
                } else {
                    store.layer = .chat
                }
            },
            onDone: inSheet ? { store.showsMission = false } : nil
        )
    }

    private func flushPending() {
        guard let target = pending else { return }
        pending = nil
        openExternal(target)
    }
}
