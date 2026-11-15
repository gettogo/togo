import Foundation
import Observation
import TogoKit

/// One source-driven menu on one connection. The helper reads and drives the
/// terminal; this model only carries screens, actions and ownership.
@MainActor @Observable final class TerminalNativeInteraction {
    enum Phase { case opening, active, ended }
    struct Failure: LocalizedError {
        let message: String
        var errorDescription: String? { message }
    }

    let id = UUID()
    private(set) var phase: Phase = .opening
    private(set) var screen: HarnessScreen?
    private(set) var failure: String?
    private(set) var busy = false
    private(set) var closing = false
    private(set) var confirmedEnd = false
    @ObservationIgnored var onEnd: @MainActor () -> Void = {}
    @ObservationIgnored private let sessionID: String
    @ObservationIgnored private let makeConnection: @MainActor () async throws -> any TerminalNativeConnection
    @ObservationIgnored private let pollInterval: Duration
    @ObservationIgnored private var client: (any TerminalNativeConnection)?
    @ObservationIgnored private var polling: Task<Void, Never>?
    @ObservationIgnored private var closingTask: Task<Void, Never>?
    @ObservationIgnored private var lifetime = UUID()
    @ObservationIgnored private var searchRequest = UUID()
    @ObservationIgnored private var reading = false
    @ObservationIgnored private var pendingSearch = false
    @ObservationIgnored private var ownQueryBase: String?
    @ObservationIgnored private var ownQuery: String?

    init(sessionID: String, pollInterval: Duration = .seconds(5),
         makeConnection: @escaping @MainActor () async throws -> any TerminalNativeConnection) {
        self.sessionID = sessionID
        self.pollInterval = pollInterval
        self.makeConnection = makeConnection
    }

    func start(_ text: String) async throws -> HarnessActionResult {
        try await open(action: "terminal.command", args: ["text": .string(text)])
    }

    func attach() async throws -> HarnessActionResult {
        try await open(action: "terminal.attach", args: nil)
    }

    private func open(action: String, args: [String: JSONValue]?) async throws -> HarnessActionResult {
        guard phase == .opening, !busy else { throw Failure(message: SurfaceCopy.finishCurrent) }
        let binding = lifetime
        var dispatched = false
        busy = true
        defer { busy = false }
        do {
            let opened = try await makeConnection()
            guard lifetime == binding, phase != .ended else {
                await opened.close()
                throw CancellationError()
            }
            client = opened
            dispatched = true
            let result = try await opened.harnessAction(.init(id: sessionID, action: action, args: args))
            guard lifetime == binding, phase != .ended else { throw Failure(message: SurfaceCopy.unconfirmed) }
            if result.result == .failed { throw Failure(message: result.message ?? SurfaceCopy.unconfirmed) }
            if let next = result.screen {
                try accept(next, transition: true)
                phase = .active
                startPolling()
            } else { await finish(confirmed: result.result == .applied) }
            return result
        } catch {
            let reported: Error = dispatched && !(error is Failure) && (error as? HelperError)?.refusalReason == nil
                ? Failure(message: SurfaceCopy.unconfirmed) : error
            if lifetime == binding {
                failure = (reported as? Failure)?.message ?? SurfaceCopy.unconfirmed
                await finish()
            }
            throw reported
        }
    }

    func run(_ action: String, target: String?, args: [String: JSONValue]?) async -> HarnessActionResult? {
        var dispatched = false
        do {
            try await waitForRead()
            guard !busy, phase == .active, let client, let token = screen?.interaction,
                  ["terminal.select", "terminal.key"].contains(action) else { throw Failure(message: SurfaceCopy.finishCurrent) }
            if let given = args?["interaction"], given != .string(token) { throw Failure(message: SurfaceCopy.inputChanged) }
            let binding = lifetime
            busy = true
            defer { busy = false }
            var values = args ?? [:]
            values["interaction"] = .string(token)
            dispatched = true
            let result = try await client.harnessAction(.init(id: sessionID, action: action, target: target, args: values))
            guard lifetime == binding, phase == .active else { return nil }
            if result.result == .failed {
                failure = result.message ?? SurfaceCopy.unconfirmed
                await finish()
            } else if let next = result.screen {
                try accept(next, transition: true)
                failure = nil
            } else { await finish(confirmed: result.result == .applied) }
            return result
        } catch is CancellationError { return nil }
        catch {
            guard phase != .ended else { return nil }
            let message = (error as? Failure)?.message ?? SurfaceCopy.unconfirmed
            failure = message
            if dispatched || !(error is Failure) { await finish() }
            return .init(result: .failed, message: message)
        }
    }

    func read(query: String? = nil, expected: String? = nil, before: String? = nil) async throws -> HarnessScreen {
        try Task.checkCancellation()
        let panel = expected ?? screen?.interaction
        guard panel == screen?.interaction else { throw Failure(message: SurfaceCopy.inputChanged) }
        let request = UUID()
        if query != nil { searchRequest = request; pendingSearch = true }
        defer { if query != nil, searchRequest == request { pendingSearch = false } }
        try await waitForRead()
        if query != nil, searchRequest != request { throw CancellationError() }
        guard !busy, phase == .active, let client, let token = screen?.interaction, panel == token else {
            throw Failure(message: SurfaceCopy.finishCurrent)
        }
        if let query {
            guard screen?.picker?.query != nil, before != nil else { throw Failure(message: SurfaceCopy.unsupported) }
            guard query.utf8.count <= 200, !query.unicodeScalars.contains(where: { $0.value < 32 || $0.value == 127 }) else {
                throw Failure(message: SurfaceCopy.searchTooLong)
            }
        }
        let binding = lifetime
        reading = true
        defer { reading = false }
        var values = ["interaction": token]
        if let query {
            values["query"] = query
            if let before {
                values["before"] = before == ownQueryBase && screen?.picker?.query == ownQueryBase ? ownQuery ?? before : before
            }
        }
        do {
            // A superseded caller must not release the serialization guard
            // while its already-dispatched query is still being applied.
            let operation = Task { try await client.harnessScreen(id: sessionID, screen: "terminal.menu", query: values) }
            let next = try await operation.value
            guard lifetime == binding, phase == .active else { throw CancellationError() }
            if query != nil, Task.isCancelled || searchRequest != request {
                if let actual = next.picker?.query {
                    ownQueryBase = ownQueryBase ?? screen?.picker?.query
                    ownQuery = actual
                }
                throw CancellationError()
            }
            try Task.checkCancellation()
            if query != nil, searchRequest != request { throw CancellationError() }
            try accept(next)
            ownQueryBase = nil
            ownQuery = nil
            return screen ?? next
        } catch {
            if lifetime == binding, query != nil, Self.refusalDetail(error) == "query" {
                let refresh = Task { try await client.harnessScreen(id: sessionID, screen: "terminal.menu", query: ["interaction": token]) }
                let actual = try await refresh.value
                guard lifetime == binding, phase == .active else { throw CancellationError() }
                try accept(actual)
                ownQueryBase = nil
                ownQuery = nil
                throw Failure(message: String(localized: "terminal.searchChanged", defaultValue: "Search changed in Terminal. Your text is kept here."))
            }
            if lifetime == binding, let reason = Self.endReason(error) {
                failure = reason == "moved" ? SurfaceCopy.inputChanged : nil
                await finish()
            }
            throw error
        }
    }

    /// Cancel presses Esc only when idle. Handback closes ownership without
    /// a key, even if a request is still in flight.
    @discardableResult func close(cancel: Bool) async -> Bool {
        guard phase != .ended else { await closingTask?.value; return true }
        if cancel, reading {
            do { try await waitForRead() }
            catch { failure = SurfaceCopy.unconfirmed; await finish(); return false }
            if phase == .ended { await closingTask?.value; return true }
        }
        if cancel, busy {
            failure = SurfaceCopy.finishCurrent
            return false
        }
        if busy || reading {
            await finish()
            return true
        }
        polling?.cancel()
        if let client, let token = screen?.interaction {
            busy = true
            let result: HarnessActionResult?
            do {
                result = try await client.harnessAction(.init(id: sessionID,
                    action: cancel ? "terminal.cancel" : "terminal.release", args: ["interaction": .string(token)]))
            } catch { result = nil }
            busy = false
            if cancel, result?.result != .applied {
                failure = result?.message ?? SurfaceCopy.unconfirmed
                await finish()
                return false
            }
        }
        await finish(confirmed: cancel)
        return true
    }

    private func accept(_ next: HarnessScreen, transition: Bool = false) throws {
        guard let token = next.interaction, !token.isEmpty, token.utf8.count <= 512,
              transition || screen?.interaction == nil || screen?.interaction == token else { throw Failure(message: SurfaceCopy.inputChanged) }
        if screen?.interaction != token {
            ownQueryBase = nil
            ownQuery = nil
            searchRequest = UUID()
            pendingSearch = false
        }
        var next = next
        if var picker = next.picker {
            picker.selectionBehavior = "reselect"
            picker.onSelectArgs = (picker.onSelectArgs ?? [:]).merging(["interaction": .string(token)]) { _, new in new }
            if picker.searchable, picker.query == nil {
                picker.searchable = false
                var presentation = picker.presentation ?? .init()
                let message = String(localized: "terminal.searchUnavailable", defaultValue: "Search is available in Terminal.")
                presentation.message = [presentation.message, message].compactMap { $0 }.joined(separator: " ")
                picker.presentation = presentation
            }
            next.picker = picker
        }
        screen = next
    }

    static func endReason(_ error: Error) -> String? {
        guard let error = error as? HelperError else { return nil }
        if ["closed", "moved"].contains(error.refusalReason ?? "") { return error.refusalReason }
        guard case .rpc(let object) = error, object.code == RPCErrorCode.refused,
              let detail = SurfaceValues.string(SurfaceValues.object(object.data)["detail"]),
              ["closed", "moved"].contains(detail) else { return nil }
        return detail
    }

    private static func refusalDetail(_ error: Error) -> String? {
        guard let error = error as? HelperError, case .rpc(let object) = error,
              object.code == RPCErrorCode.refused else { return nil }
        return SurfaceValues.string(SurfaceValues.object(object.data)["detail"])
    }

    private func waitForRead() async throws {
        try Task.checkCancellation()
        let deadline = ContinuousClock.now + .seconds(5)
        while reading {
            try Task.checkCancellation()
            guard phase != .ended, ContinuousClock.now < deadline else { throw Failure(message: SurfaceCopy.unconfirmed) }
            try await Task.sleep(for: .milliseconds(20))
        }
    }

    private func startPolling() {
        polling?.cancel()
        polling = Task { @MainActor [weak self, pollInterval] in
            while !Task.isCancelled {
                do { try await Task.sleep(for: pollInterval) } catch { return }
                guard let self, self.phase == .active else { return }
                if self.busy || self.reading || self.pendingSearch { continue }
                do { _ = try await self.read() }
                catch is CancellationError { return }
                catch {
                    guard self.phase != .ended else { return }
                    self.failure = SurfaceCopy.unconfirmed
                    await self.finish()
                    return
                }
            }
        }
    }

    private func finish(confirmed: Bool = false) async {
        guard phase != .ended else { await closingTask?.value; return }
        phase = .ended
        confirmedEnd = confirmed
        if confirmed { failure = nil }
        lifetime = UUID()
        searchRequest = UUID()
        polling?.cancel()
        polling = nil
        let owned = client
        client = nil
        closing = true
        let task = Task<Void, Never> { if let owned { await owned.close() } }
        closingTask = task
        await task.value
        closing = false
        closingTask = nil
        onEnd()
    }
}
