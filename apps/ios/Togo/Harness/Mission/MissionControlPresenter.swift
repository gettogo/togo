import SwiftUI
import TogoKit

extension View {
    /// Presents `store`'s mission control while `store.showsMission` is set: a sheet with
    /// medium and large detents that leaves the chat usable behind it on a compact width, a
    /// trailing inspector beside the chat on a regular one. Diffs and previews leave through
    /// `openExternal`; a sheet closes first so the review can take the screen.
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
    let store: HarnessSessionStore?
    let actions: ChatActions
    let openExternal: (HarnessOpen) -> Void

    @State private var detent: PresentationDetent = .medium
    /// A diff or preview asked for from the sheet, opened once the sheet has gone.
    @State private var pending: HarnessOpen?

    private var presentation: MissionPresentation { MissionPresentation.choose(sizeClass) }

    func body(content: Content) -> some View {
        content
            .sheet(isPresented: shown(.sheet), onDismiss: flushPending) {
                if let store {
                    stack(store, inSheet: true)
                        .presentationDetents([.medium, .large], selection: $detent)
                        .presentationBackgroundInteraction(.enabled(upThrough: .medium))
                        .presentationContentInteraction(.scrolls)
                        .presentationDragIndicator(.visible)
                        .presentationBackground(Color.togoInk)
                }
            }
            .inspector(isPresented: shown(.inspector)) {
                if let store {
                    stack(store, inSheet: false)
                        .inspectorColumnWidth(min: 280, ideal: 320, max: 440)
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
            showChat: {
                if inSheet { store.showsMission = false }
                store.layer = .chat
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

/// The bar item that shows and hides mission control beside the chat. A symbol and a title,
/// so it can stand in a vertical bar on Duo; Cmd-I from a hardware keyboard.
struct MissionToolbarItem: ToolbarContent {
    let store: HarnessSessionStore

    var body: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Button(MissionCopy.title, systemImage: "gauge.with.dots.needle.33percent") { store.showsMission.toggle() }
                .keyboardShortcut("i", modifiers: .command)
                .accessibilityValue(MissionText.spoken(store.mission.summary))
                .accessibilityHint(MissionCopy.toggleHint)
        }
    }
}

extension AppStore {
    /// The bar item for the focused tab while its chat shows and it has mission control.
    var missionItem: MissionToolbarItem? {
        guard let tab = focusedTab, let harness = harness(for: tab.id),
              harness.shownLayer == .chat, harness.hasMission else { return nil }
        return MissionToolbarItem(store: harness)
    }

    /// Follows a target mission control hands out for the session in `tab`: a background
    /// process's live terminal as a tab, a subagent's chat or the session's terminal through
    /// `openHarnessTarget`, and anything else in the review.
    func followMissionTarget(_ target: HarnessOpen, from tab: TabID) {
        if target.kind == .terminal, let terminal = TerminalTarget(target.ref) {
            openBackgroundTerminal(terminal, on: tab.hostID)
        } else if !openHarnessTarget(target, from: tab) {
            openMissionTarget(target, hostID: tab.hostID, sessionID: tab.sessionID)
        }
    }

    /// Opens a mission control target that leaves for the review: a diff on Changes, a port
    /// or URL on Ports.
    func openMissionTarget(_ target: HarnessOpen, hostID: HostID, sessionID: String) {
        guard let row = controlCenter.machines.first(where: { $0.id == hostID })?.sessions
            .first(where: { $0.id == sessionID }) else { return }
        switch target.kind {
        case .diff: review(row, tab: .changes)
        case .url: review(row, tab: .ports)
        default: break
        }
    }
}
