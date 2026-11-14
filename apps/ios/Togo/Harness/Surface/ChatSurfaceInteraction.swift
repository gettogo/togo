import Foundation
import Observation
import TogoKit

/// One command or incoming form in the existing chat. Its lease lasts only
/// until its original controls close or the user hands control back.
@MainActor @Observable final class ChatSurfaceInteraction {
    struct Block: Identifiable {
        let view: SurfaceView
        let node: SurfaceNode
        var id: String { "\(view.id)/\(view.instance)/\(node.id)" }
    }

    struct Failure: LocalizedError {
        let message: String
        var errorDescription: String? { message }
    }

    let id = UUID()
    let session: SurfaceSession
    let command: String?
    private(set) var requestID: String?
    let earlierRequests: Set<String>
    private(set) var submitted: Bool
    private(set) var presented = false
    private(set) var retired = false
    private var completed = false
    let finished: @MainActor () -> Void

    init(command: String?, requestID: String? = nil, earlierRequests: Set<String> = [], session: SurfaceSession, finished: @escaping @MainActor () -> Void) {
        self.command = command
        self.requestID = requestID
        self.earlierRequests = earlierRequests
        self.session = session
        self.submitted = command == nil
        self.finished = finished
    }

    var blocks: [Block] {
        guard let scene = session.scene else { return [] }
        return scene.views.compactMap { view in
            guard !view.closed, let node = SurfaceValues.chatInteraction(view.document.root, mode: view.mode) else { return nil }
            return Block(view: view, node: node)
        }
    }

    var showingForm: Bool {
        func form(_ node: SurfaceNode) -> Bool {
            node.properties["role"] == .string("omp.editor.ask") || node.children.contains(where: form)
        }
        return blocks.contains { form($0.node) }
    }

    func attachRequest(_ id: String) { if requestID == nil { requestID = id } }

    func run(options: SurfaceOptions) async {
        guard !retired else { return }
        await session.run(options: options)
    }

    /// The main composer waits only for the original program to take the
    /// command. The resulting controls stay in the chat until they close.
    func submit() async throws {
        guard let command else { return }
        try await wait(until: .now + .seconds(8)) {
            self.session.phase == .active && (self.session.scene.flatMap(Self.composer) != nil || !self.blocks.isEmpty)
        }
        guard session.canInteract, let scene = session.scene, scene.features.contains("send"),
              blocks.isEmpty,
              let target = Self.composer(in: scene),
              let current = SurfaceValues.string(target.node.properties["text"]) else {
            throw Failure(message: SurfaceCopy.finishCurrent)
        }
        guard current.isEmpty else { throw Failure(message: SurfaceCopy.deskDraft) }
        let outcome = await session.input(surface: target.view.id, node: target.node.id,
                            event: ["ev": .string("send"), "text": .string(command)], expectedText: current)
        guard outcome == .dispatched, !retired else { throw Failure(message: SurfaceCopy.unconfirmed) }
        submitted = true
        changed()
        // Dispatch is not command completion. A delayed overlay may still be
        // on its way; never close just because the next frame has no controls.
    }

    func changed() {
        guard !retired else { return }
        if !blocks.isEmpty { presented = true }
        if submitted, presented, blocks.isEmpty, session.pending == nil, session.phase == .active { finish() }
    }

    func retire() { retired = true }
    func stop() async { await session.stop() }

    private func finish() {
        guard !completed else { return }
        completed = true
        finished()
    }

    func turnStarted() {
        guard submitted, !presented, !retired else { return }
        finish()
    }

    private func wait(until deadline: ContinuousClock.Instant, ready: () -> Bool) async throws {
        while !ready() {
            try Task.checkCancellation()
            if retired || session.phase == .unavailable || session.notice != nil { throw Failure(message: SurfaceCopy.unconfirmed) }
            guard ContinuousClock.now < deadline else { throw Failure(message: SurfaceCopy.unconfirmed) }
            try await Task.sleep(for: .milliseconds(20))
        }
        try Task.checkCancellation()
        if retired || session.phase == .unavailable || session.notice != nil { throw Failure(message: SurfaceCopy.unconfirmed) }
    }

    static func composer(in scene: SurfaceScene) -> Block? {
        for view in scene.views where !view.closed && view.listen && !view.document.suspended {
            guard let node = view.document.root.node(id: view.document.focus ?? ""),
                  node.kind == "editor", node.properties["sendable"] == .bool(true),
                  !SurfaceValues.disabled(node.properties["disabled"]), node.properties["hidden"] != .bool(true) else { continue }
            return Block(view: view, node: node)
        }
        return nil
    }
}
