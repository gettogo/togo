import SwiftUI
import TogoKit

extension View {
    /// Presents `store`'s mission control as a sheet while `store.showsMission` is set, where the
    /// window is too narrow for an inspector column: medium and large detents, the chat usable
    /// behind it. Where the window holds sidebar, chat and inspector, `MissionInspectorColumn`
    /// on the detail column's stack shows it instead. It opens only when the user asks.
    /// Diffs and previews leave through `openExternal`; a sheet closes first so the review can
    /// take the screen.
    func missionControl(
        _ store: HarnessSessionStore?, actions: ChatActions = .inert,
        openExternal: @escaping (HarnessOpen) -> Void = { _ in }
    ) -> some View {
        modifier(MissionControlPresenter(store: store, actions: actions, openExternal: openExternal))
    }
}

/// Mission control's sheet for the workspace's focused tab, on every window too narrow for the
/// inspector column (phones, an iPhone Duo folded or open, an iPad in portrait). Its targets
/// leave through `AppStore.followMissionTarget`.
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
    /// From the window's width, so a sidebar shown or hidden never switches it.
    private var presentation: MissionPresentation { MissionPresentation.choose(sizeClass, windowWidth: windowWidth) }

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
            .onChange(of: presentation) { old, new in
                // Folding a Duo with the inspector open does not raise a sheet nobody asked for.
                if old == .inspector, new == .sheet, let store, store.showsMission { store.showsMission = false }
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

/// Mission control as the split view's inspector column, for the focused tab, where the window
/// holds the sidebar, a usable chat and the inspector (`AdaptivePresentation.inspectorMinimumWidth`).
///
/// Applied to the detail column's `NavigationStack`, never inside it: an `inspector` inside the
/// stack wraps the workspace in an inspector container of its own, whose bar items are no longer
/// the stack's, so an iPhone Duo's vertical bar showed only the status, and on a tight window
/// the container overlaid and dimmed the chat. Narrower windows never show it, only the sheet
/// (`missionControl`).
///
/// Attached on every width, shown only where it fits: a branch on the width here would swap the
/// whole detail stack out when an iPad rotates or a window resizes across the threshold, taking
/// down every sheet and question the workspace had up mid-presentation (the orphaned dimming
/// view that leaves a window taking no touches).
struct MissionInspectorColumn: ViewModifier {
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.windowWidth) private var windowWidth
    let app: AppStore

    func body(content: Content) -> some View {
        let fits = MissionPresentation.choose(sizeClass, windowWidth: windowWidth) == .inspector
        let tab = app.focusedTab?.id
        let store = tab.flatMap { app.harness(for: $0) }
        content.coordinated("mission.inspector", .inspector, isPresented: shown(store, fits: fits)) { view, shown in
            view.inspector(isPresented: shown) {
                if fits, let store, let tab {
                    MissionControlStack(
                        store: store, actions: app.chatActions(for: tab),
                        openExternal: { [app] target in app.followMissionTarget(target, from: tab) },
                        showChat: { needs in
                            if store.chatAvailable, needs != nil || store.oldestOpenNeeds != nil {
                                store.revealNeeds(id: needs)
                            } else {
                                store.layer = .chat
                            }
                        },
                        onDone: nil
                    )
                    .inspectorColumnWidth(min: 280, ideal: MissionPlacement.inspectorIdeal, max: 440)
                }
            }
        }
    }

    /// Up only where the column fits; elsewhere the sheet (`missionControl`) shows the same state,
    /// so a close from here never clears what the sheet shows.
    private func shown(_ store: HarnessSessionStore?, fits: Bool) -> Binding<Bool> {
        Binding(
            get: { fits && store?.showsMission == true },
            set: { shown in
                guard fits, let store, store.showsMission != shown else { return }
                store.showsMission = shown
            }
        )
    }
}
