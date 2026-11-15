import Foundation
import Observation

/// State for one visible native interface. The view owns its run task; leaving
/// the scene cancels and joins the connection. Reading never acknowledges UI.
@MainActor @Observable public final class SurfaceSession {
    public enum Phase: Sendable { case loading, active, unavailable, suspended }
    public enum Notice: Sendable { case unconfirmed, unavailable }
    public enum InputOutcome: Sendable { case notDispatched, dispatched, refused, unconfirmed }
    public struct Pending: Sendable {
        public let surface: String
        public let node: String
        let instance: UInt64
        let before: SurfaceNode
        let kind: String
        let desiredText: String?
        let deadline: ContinuousClock.Instant
        let token: UUID
        var dispatched = false
    }

    public private(set) var phase: Phase = .suspended
    public private(set) var scene: SurfaceScene?
    public private(set) var generation: String?
    public private(set) var pending: Pending?
    public private(set) var notice: Notice?
    public private(set) var inputFailure: HelperError?
    public var canInteract: Bool { phase == .active && pending == nil && notice == nil }
    public var companionFeatures: Set<String> { Set(lease?.features ?? []) }

    @ObservationIgnored private let id: String
    @ObservationIgnored private let makeConnection: @MainActor () async throws -> any SurfaceConnection
    @ObservationIgnored private let pollInterval: Duration
    @ObservationIgnored private let pendingTimeout: Duration
    @ObservationIgnored private var worker: Task<Void, Never>?
    @ObservationIgnored private var request = UUID()
    @ObservationIgnored private var connection: (any SurfaceConnection)?
    @ObservationIgnored private var lease: SurfaceLease?
    @ObservationIgnored private var receipts: [String: Receipt] = [:]
    @ObservationIgnored private var displayedRevision: UInt64?
    @ObservationIgnored private var acknowledging: UUID?

    private struct Receipt: Equatable {
        var instance: UInt64
        var sequence: Int64
    }

    public init(
        id: String, makeConnection: @escaping @MainActor () async throws -> any SurfaceConnection,
        pollInterval: Duration = .milliseconds(250), pendingTimeout: Duration = .seconds(4)
    ) {
        self.id = id
        self.makeConnection = makeConnection
        self.pollInterval = pollInterval
        self.pendingTimeout = pendingTimeout
    }

    /// Serializes replacement runs so an old connection is released before a
    /// layout change or foreground transition tries to reclaim the pane.
    public func run(options: SurfaceOptions) async {
        let token = UUID()
        request = token
        let previous = worker
        previous?.cancel()
        await previous?.value
        guard request == token, !Task.isCancelled else { return }
        let task = Task { await self.drive(options: options, token: token) }
        worker = task
        await withTaskCancellationHandler { await task.value } onCancel: { task.cancel() }
        if request == token { worker = nil }
    }

    public func stop() async {
        let token = UUID()
        request = token
        let task = worker
        task?.cancel()
        await task?.value
        guard request == token else { return }
        worker = nil
        phase = .suspended
    }

    private func drive(options: SurfaceOptions, token: UUID) async {
        phase = .loading
        scene = nil
        notice = nil
        inputFailure = nil
        pending = nil
        receipts = [:]
        acknowledging = nil
        displayedRevision = nil
        var opened: (any SurfaceConnection)?
        var ownedLease: SurfaceLease?
        do {
            let client = try await makeConnection()
            opened = client
            try Task.checkCancellation()
            let lease = try await client.surfaceOpen(id: id, options: options)
            ownedLease = lease
            try Task.checkCancellation()
            connection = client
            self.lease = lease
            generation = lease.generation
            var revision: UInt64 = 0
            while !Task.isCancelled {
                let next = try await client.surfaceSnapshot(lease, revision: revision, kinds: Set(options.kinds))
                try Task.checkCancellation()
                guard self.lease == lease else { break }
                if let next {
                    scene = next
                    revision = next.revision
                    phase = .active
                    settlePending(next)
                    let retained = Set(next.views.map(\.id))
                    receipts = receipts.filter { retained.contains($0.key) }
                }
                if let pending, ContinuousClock.now >= pending.deadline {
                    self.pending = nil
                    notice = .unconfirmed
                }
                try await Task.sleep(for: pollInterval)
            }
        } catch {
            if !Task.isCancelled {
                phase = .unavailable
                notice = pending == nil ? .unavailable : .unconfirmed
            }
        }
        if let opened {
            if !Task.isCancelled, let ownedLease { try? await opened.surfaceClose(ownedLease) }
            // Closing the actual stream revokes ownership even when cancellation
            // prevents an explicit RPC close or its reply cannot cross the network.
            await opened.close()
        }
        connection = nil
        lease = nil
        pending = nil
        if Task.isCancelled { phase = .suspended }
    }

    /// A display receipt includes the generation, so a delayed view callback
    /// cannot acknowledge a new conversation with the same numeric revision.
    public func displayed(revision: UInt64, generation: String) async {
        guard phase == .active, self.generation == generation, scene?.revision == revision else { return }
        displayedRevision = revision
        guard acknowledging == nil else { return }
        let acknowledgment = UUID()
        acknowledging = acknowledgment
        defer { if acknowledging == acknowledgment { acknowledging = nil } }
        guard let connection, let lease else { return }
        while let revision = displayedRevision {
            displayedRevision = nil
            guard self.lease == lease, phase == .active, let scene, scene.revision == revision else { continue }
            for view in scene.views where !view.closed {
                let receipt = Receipt(instance: view.instance, sequence: view.document.sequence)
                if receipts[view.id] == receipt { continue }
                do { try await connection.surfaceAck(lease, view: view) } catch {
                    guard self.lease == lease else { return }
                    self.lease = nil
                    phase = .unavailable
                    notice = pending == nil ? .unavailable : .unconfirmed
                    pending = nil
                    await connection.close()
                    return
                }
                guard self.lease == lease else { return }
                receipts[view.id] = receipt
            }
        }
    }

    /// Dispatches once, then waits for a producer document to reflect a change.
    /// A timeout remains visible and does not silently repeat the action.
    @discardableResult public func input(
        surface: String, node: String, event: [String: JSONValue], expectedText: String? = nil
    ) async -> InputOutcome {
        guard canInteract, let connection, let lease, let view = scene?.views.first(where: { $0.id == surface }),
              !view.closed, view.listen, !view.document.suspended,
              let target = view.document.root.node(id: node), target.properties["disabled"] != .bool(true),
              target.properties["hidden"] != .bool(true), case .string(let kind)? = event["ev"] else { return .notDispatched }
        inputFailure = nil
        var event = event
        event["id"] = .string(node)
        event["sf"] = .string(surface)
        let token = UUID()
        let desired: String?
        if kind == "edit" { desired = Self.editedText(event, before: target) } else { desired = nil }
        pending = Pending(surface: surface, node: node, instance: view.instance, before: target,
                          kind: kind, desiredText: desired, deadline: .now + pendingTimeout, token: token)
        do {
            let result = try await connection.surfaceInput(lease, surface: surface, instance: view.instance,
                                                          event: .object(event), expectedText: expectedText)
            guard self.lease == lease, pending?.token == token else { return .unconfirmed }
            if result.refused == true {
                // A rejected precondition changes nothing. Keep reading the
                // original scene; never replay the input or revoke its lease.
                pending = nil
                return .refused
            }
            if result.dispatched {
                pending?.dispatched = true
                if kind == "send", result.consumed == true { pending = nil }
                else if let scene { settlePending(scene) }
                return .dispatched
            } else {
                pending = nil
                notice = .unconfirmed
                return .unconfirmed
            }
        } catch {
            guard self.lease == lease, pending?.token == token else { return .unconfirmed }
            pending = nil
            inputFailure = error as? HelperError
            notice = .unconfirmed
            return .unconfirmed
        }
    }

    private func settlePending(_ next: SurfaceScene) {
        guard let pending, pending.dispatched else { return }
        guard let view = next.views.first(where: { $0.id == pending.surface }), !view.closed,
              view.instance == pending.instance, let node = view.document.root.node(id: pending.node) else {
            self.pending = nil
            return
        }
        if pending.kind == "edit", let desired = pending.desiredText {
            if node.properties["text"] == .string(desired) { self.pending = nil }
        } else if pending.kind == "focus", view.document.focus == pending.node {
            self.pending = nil
        } else if node != pending.before {
            self.pending = nil
        }
    }

    private static func editedText(_ event: [String: JSONValue], before node: SurfaceNode) -> String? {
        guard case .string(let current)? = node.properties["text"], case .string(let replacement)? = event["text"],
              case .number(let from)? = event["from"], case .number(let to)? = event["to"],
              from.isFinite, to.isFinite, from.rounded() == from, to.rounded() == to else { return nil }
        let units = Array(current.utf16)
        guard from >= 0, to >= from, to <= Double(units.count) else { return nil }
        let start = Int(from), end = Int(to)
        func boundary(_ offset: Int) -> Bool {
            !(offset > 0 && offset < units.count && (0xd800...0xdbff).contains(units[offset - 1]) && (0xdc00...0xdfff).contains(units[offset]))
        }
        guard boundary(start), boundary(end) else { return nil }
        let result = Array(units[..<start]) + Array(replacement.utf16) + Array(units[end...])
        return String(decoding: result, as: UTF16.self)
    }
}
