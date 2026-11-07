import Foundation
import TogoKit

extension CommandLineModel {
    /// Everything the model asks of the session, so tests can stand in for it.
    struct Routes {
        var has: @MainActor (String) -> Bool
        var working: @MainActor () -> Bool
        var mode: @MainActor () -> String
        var modes: @MainActor () -> [HarnessChoice]
        /// Queued prompts the conversation shows, oldest first.
        var queued: @MainActor () -> [HarnessItem]
        /// Names the harness, so history and favourites stay with it.
        var scope: @MainActor () -> String
        var complete: @MainActor (HarnessCompleteParams.Kind, String) async throws -> [HarnessCompletion]
        /// Sends a prompt; the user item when the route returns one.
        var send: @MainActor (String, HarnessSendParams.Mode) async throws -> HarnessItem?
        var interrupt: @MainActor () async throws -> HarnessActionResult
        var run: @MainActor (String, String?, [String: JSONValue]?) async throws -> HarnessActionResult
        /// A prompt sent now reaches the session: the agent is idle, or the harness queues.
        var canSendNow: @MainActor () -> Bool = { true }
        /// Prompts the phone holds before the helper has them, oldest first.
        var pending: @MainActor () -> [PendingPrompt] = { [] }
        var cancelPending: @MainActor (UUID) -> Void = { _ in }
        var retryPending: @MainActor (UUID) -> Void = { _ in }
        /// Sends a prompt with image paths in it, as one message.
        var sendImages: (@MainActor (String, HarnessSendParams.Mode, [String]) async throws -> HarnessItem?)?
        /// Writes a picture to the session's folder on the machine; nil where it cannot take one.
        var upload: ComposerAttachments.Upload?
        /// The session's Status block, for the state chip's model and effort.
        var status: @MainActor () -> HarnessStatus? = { nil }
        /// The session is still being described or opened: the chip shows a placeholder.
        var describing: @MainActor () -> Bool = { false }
        /// The screens the descriptor lists (the chip reads the composer ones).
        var screens: @MainActor () -> [HarnessDescriptor.ScreenRef] = { [] }
        /// The harness's name, for "Restart Claude now?" and "waiting for Claude".
        var agent: @MainActor () -> String = { "" }
        /// What a screen asks before its selection runs (`onSelectConfirm`), read only while the
        /// agent works, when a restart would cost the reply.
        var confirmFor: @MainActor (String) async -> HarnessSelectConfirm? = { _ in nil }
        /// The offered action with this id (a Status block's), for its confirm line.
        var actionRef: @MainActor (String) -> HarnessActionRef? = { _ in nil }
        var showTerminal: @MainActor () -> Void = {}
        /// Mission control, at Status or Changes.
        var showMission: @MainActor () -> Void = {}

        /// Pictures go through `fs.upload` where the machine's helper serves it.
        @MainActor static func uploadRoute(_ store: HarnessSessionStore) -> ComposerAttachments.Upload? {
            guard store.subject.capabilities.contains(Capability.upload) else { return nil }
            let services = store.services
            let subject = store.subject
            return { name, data in try await services.upload(subject, name: name, data: data) }
        }

        @MainActor static func store(_ store: HarnessSessionStore) -> Routes {
            Routes(
                has: { store.has($0) },
                working: { store.working },
                mode: { store.currentMode },
                modes: { store.descriptor?.modes ?? [] },
                queued: {
                    store.timeline.conversation().filter { item in
                        if case let .user(user) = item.body { user.queued } else { false }
                    }
                },
                scope: { store.descriptor?.adapter?.id ?? "" },
                complete: { kind, query in try await store.services.complete(store.subject, kind: kind, query: query) },
                send: { text, mode in
                    try await store.send(text, mode: mode)
                    return nil
                },
                interrupt: { try await store.interrupt() },
                run: { action, target, args in try await store.run(action: action, target: target, args: args) },
                canSendNow: { store.canSendNow },
                pending: { store.queue.prompts },
                cancelPending: { store.cancelQueued($0) },
                retryPending: { store.retryQueued($0) },
                sendImages: { text, mode, images in
                    try await store.send(text, mode: mode, images: images)
                    return nil
                },
                upload: uploadRoute(store),
                status: { ModeMemory.statusBlock(store.mission.mission) },
                describing: { store.phase == .describing || store.phase == .opening },
                screens: { store.descriptor?.screens ?? [] },
                agent: { store.descriptor?.adapter?.name ?? "" },
                confirmFor: { id in await confirm(store, screen: id) },
                actionRef: { id in
                    store.mission.mission?.sections.lazy.compactMap { $0.status?.actions?.first { $0.id == id } }.first
                },
                showTerminal: { store.layer = .terminal },
                showMission: { store.showsMission = true }
            )
        }

        /// A screen's confirm for a selection: a picker's `onSelectConfirm`, or a one-field
        /// settings screen's `actionConfirm`.
        @MainActor private static func confirm(_ store: HarnessSessionStore, screen id: String) async -> HarnessSelectConfirm? {
            guard let screen = try? await store.screen(id) else { return nil }
            if let confirm = screen.picker?.onSelectConfirm { return confirm }
            return screen.settings?.sections.lazy.flatMap(\.fields).compactMap(\.actionConfirm).first
        }
    }
}
