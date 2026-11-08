import SwiftUI
import TogoKit

/// Mission control's part of the workspace around the focused session: the bar item, the
/// nudge when a request arrives (the badge's bounce is the item's own; here the `.connect`
/// haptic and the VoiceOver line), Compact asked for from the item's long press, and the
/// adapter's screens opened from the session menu.
@MainActor @Observable final class MissionWorkspaceModel {
    /// Runs Compact from the bar item's menu and asks for it in the system dialog.
    let compact = PrimitiveActions()
    /// An adapter screen opened from the session menu.
    var screen: SessionScreen?

    init() {}

    /// The bar item for `app`'s focused session, while its chat shows and it has a mission.
    func item(
        for app: AppStore, presentation: MissionPresentation, report: @escaping @MainActor (String?) -> Void
    ) -> MissionToolbarItem? {
        guard let store = app.missionStore, let tab = app.focusedTab?.id else { return nil }
        let ref = Self.compactRef(store)
        return MissionToolbarItem(
            store: store, presentation: presentation,
            compact: ref.map { ref in
                { [weak self] in
                    guard let self else { return }
                    let context = Self.context(store, app: app, tab: tab, model: self)
                    Task { await self.compact.trigger(ref, place: .toast, context: context) }
                }
            },
            stop: { Task { report(await Self.stop(store)) } }
        )
    }

    /// Compact from the Status block's actions; with the capability alone, an equivalent that
    /// asks first, so the menu's "Compact…" always leads to the same dialog.
    static func compactRef(_ store: HarnessSessionStore) -> HarnessActionRef? {
        let status = store.mission.mission?.sections.first { $0.kind == .status }?.status
        if let status, let ref = status.actions?.first(where: { StatusLogic.compactIDs.contains($0.id) }) { return ref }
        guard store.has(HarnessCapability.compact) else { return nil }
        return HarnessActionRef(id: HarnessCapability.compact, label: StatusCopy.compact, confirm: MissionCopy.compactConfirm)
    }

    /// Stops the turn; nil when it stopped, else what to say.
    static func stop(_ store: HarnessSessionStore) async -> String? {
        do {
            let result = try await store.interrupt()
            return result.result == .failed ? (result.message ?? MissionCopy.stopFailed) : nil
        } catch {
            return MissionCopy.stopFailed
        }
    }

    static func context(_ store: HarnessSessionStore, app: AppStore, tab: TabID, model: MissionWorkspaceModel) -> PrimitiveContext {
        PrimitiveContext.store(
            store,
            openScreen: { [weak model] id in model?.screen = SessionScreen(store: store, tab: tab, id: id) },
            open: { [app] target in app.followMissionTarget(target, from: tab) },
            showTerminal: { store.layer = .terminal }
        )
    }
}

/// One of the adapter's screens, in a sheet over the workspace.
struct SessionScreen: Identifiable {
    let store: HarnessSessionStore
    let tab: TabID
    let id: String
}

/// Hosts what `MissionWorkspaceModel` raises, and the nudge, on the workspace.
struct MissionWorkspace: ViewModifier {
    let app: AppStore
    @Bindable var model: MissionWorkspaceModel

    private var nudge: MissionNudge.Snapshot {
        let store = app.missionStore
        return MissionNudge.Snapshot(
            session: store.map { MissionInspectorMemory.key($0.subject) },
            needsYou: store.map { MissionPlacement.badge($0.mission.summary) } ?? 0
        )
    }

    func body(content: Content) -> some View {
        content
            .primitiveActions(model.compact, context: .inert)
            .haptic(.connect, trigger: nudge, change: MissionNudge.arrived)
            .onChange(of: nudge) { old, new in
                guard MissionNudge.arrived(old, new) else { return }
                NeedsAnnouncement.post(MissionCopy.needs(new.needsYou), after: .seconds(1))
            }
            // Resume, MCP servers, Providers, Settings: each by its class, from the descriptor.
            .harnessPresent(
                item: $model.screen,
                as: AdaptivePresentation.content(forScreen: model.screen?.id ?? "", in: model.screen?.store.descriptor),
                name: "mission.screen"
            ) { screen in
                HarnessScreenSheet(store: screen.store, screenID: screen.id) { [app] target in
                    app.followMissionTarget(target, from: screen.tab)
                }
                // Paths and links in these screens go through the chat's router.
                .environment(\.openURL, OpenURLAction { [app] url in app.openChatLink(url, from: screen.tab) })
            }
    }
}

/// The session menu's Session section: the adapter's `more` screens (Resume…, MCP servers…,
/// Providers…, Settings…), each opening in a sheet. The same system menu on every device, so
/// it is in the vertical bar on a Duo.
struct MissionSessionSection: View {
    let store: HarnessSessionStore
    let tab: TabID
    let model: MissionWorkspaceModel

    var body: some View {
        let screens = MissionLayout.moreScreens(store.descriptor)
        if !screens.isEmpty {
            Section(MissionCopy.sessionSection) {
                ForEach(screens) { screen in
                    Button(MissionCopy.opensScreen(screen.title), systemImage: Self.symbol(screen.id)) {
                        model.screen = SessionScreen(store: store, tab: tab, id: screen.id)
                    }
                }
            }
        }
    }

    static func symbol(_ id: String) -> String {
        switch id {
        case "resume": "clock.arrow.circlepath"
        case "mcp": "server.rack"
        case "providers": "building.2"
        case "settings": "gearshape"
        default: "rectangle.stack"
        }
    }
}
