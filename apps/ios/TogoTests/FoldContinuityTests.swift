import CoreGraphics
import Foundation
import Synchronization
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

/// Folding, unfolding and resizing a window rearranges the panes without touching the sessions:
/// the same terminals stay attached, nothing reconnects and the host hears one new size.
@MainActor struct FoldContinuityTests {
    private let services = FakeAppServices()
    private let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private let four = TabID(hostID: Samples.studio, sessionID: "s-04")

    private static let folded = CGSize(width: 466, height: 678)
    private static let open = CGSize(width: 951, height: 669)
    private static let book = FoldInfo.from(active: CGRect(x: 463, y: 0, width: 25, height: 669), inactive: nil)

    private func store() -> AppStore {
        let store = AppStore(services: services, controlCenter: Samples.controlCenter)
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.open(hostID: four.hostID, sessionID: four.sessionID)
        store.select(one)
        store.openBeside(four)
        return store
    }

    /// Folded, then opened as a book, then folded again.
    private func foldTwice(_ host: PanesHost, check: () -> Void) {
        host.resize(to: Self.folded, sizeClass: .compact)
        check()
        host.resize(to: Self.open, sizeClass: .regular, fold: Self.book)
        check()
        host.resize(to: Self.folded, sizeClass: .compact)
        check()
    }

    @Test func foldKeepsTerminalController() throws {
        let store = store()
        let pool = TerminalPool()
        let host = PanesHost(store: store, terminals: pool, size: Self.folded, sizeClass: .compact)
        defer { host.close() }
        let controller = try #require(pool.handle(for: one)?.engineView)
        #expect(!store.panesVisible)

        host.resize(to: Self.open, sizeClass: .regular, fold: Self.book)
        #expect(store.panesVisible)
        #expect(pool.handle(for: one)?.engineView === controller)
        #expect(controller.view.window === host.window)
        #expect(host.visibleFrame(of: one) != nil)
        #expect(host.visibleFrame(of: four) != nil)

        host.resize(to: Self.folded, sizeClass: .compact)
        #expect(!store.panesVisible)
        #expect(pool.handle(for: one)?.engineView === controller)
        #expect(host.visibleFrame(of: one) != nil)
    }

    @Test func foldKeepsLinkAttempts() {
        let store = store()
        let pool = TerminalPool()
        let host = PanesHost(store: store, terminals: pool, size: Self.open, sizeClass: .regular, fold: Self.book)
        defer { host.close() }
        let attempts = services.terminalsMade.count
        #expect(attempts == 2)
        foldTwice(host) {
            #expect(services.terminalsMade.count == attempts)
            #expect(services.stoppedTerminals.isEmpty)
        }
    }

    @Test func draftSurvivesArrangementChange() {
        let store = store()
        let host = PanesHost(store: store, terminals: TerminalPool(), size: Self.open, sizeClass: .regular, fold: Self.book)
        defer { host.close() }
        store.drafts[one] = "half a thought"
        store.drafts[four] = "and another"
        store.composing = one
        foldTwice(host) {
            #expect(store.drafts[one] == "half a thought")
            #expect(store.drafts[four] == "and another")
            #expect(store.composing == one)
            #expect(store.composeTab?.id == one)
        }
    }

    @Test func secondaryRestoredAfterUnfold() {
        let store = store()
        let host = PanesHost(store: store, terminals: TerminalPool(), size: Self.open, sizeClass: .regular, fold: Self.book)
        defer { host.close() }
        store.focus(.secondary)

        // Folded: the focused pane fills the outer display; the other waits in state.
        host.resize(to: Self.folded, sizeClass: .compact)
        #expect(store.workspace.selected == four)
        #expect(store.workspace.secondary == one)
        #expect(store.focusedTab?.id == four)
        // The panes flow into place with a spring; let it finish.
        host.settle(for: 0.6)
        #expect(host.visibleFrame(of: four)?.width == Self.folded.width)

        // Opened: both come back where they were, focus included.
        host.resize(to: Self.open, sizeClass: .regular, fold: Self.book)
        #expect(store.workspace.selected == one)
        #expect(store.workspace.secondary == four)
        #expect(store.workspace.focused == .secondary)
        #expect(store.panesVisible)
    }

    @Test func secondaryStaysPutWhenTheUserMovesOnWhileFolded() {
        let store = store()
        let five = TabID(hostID: Samples.studio, sessionID: "s-05")
        let host = PanesHost(store: store, terminals: TerminalPool(), size: Self.open, sizeClass: .regular, fold: Self.book)
        defer { host.close() }
        store.focus(.secondary)
        host.resize(to: Self.folded, sizeClass: .compact)
        store.open(hostID: five.hostID, sessionID: five.sessionID)
        host.resize(to: Self.open, sizeClass: .regular, fold: Self.book)
        #expect(store.workspace.selected == five)
        #expect(store.workspace.secondary == one)
    }

    @Test func foldSendsOneResize() async throws {
        let transport = SizeRecordingTransport()
        let clock = ManualClock()
        let link = TerminalLink(
            connect: { _, _, _ in
                try await transport.connect()
                return Attachment(transport: transport, kind: .ssh, notes: [], connection: Self.connection())
            },
            path: StillPath(), clock: clock, cols: 80, rows: 24
        )
        await link.start()
        for await state in link.states {
            if case .live = state { break }
        }

        // The fold animation reports a new size every frame for 350 ms, then holds still.
        var width = 80
        for _ in stride(from: 0, to: 350, by: 16) {
            width += 2
            await link.resize(cols: width, rows: 30)
            await Task.yield()
            clock.advance(.milliseconds(16))
        }
        #expect(await eventually { clock.waiting == 1 })
        #expect(await transport.sizes.isEmpty)
        clock.advance(.milliseconds(200))

        #expect(await eventually { await link.resizesSent == 1 })
        try await Task.sleep(for: .milliseconds(50))
        #expect(await link.resizesSent == 1)
        #expect(await transport.sizes.count == 1)
        #expect(await transport.sizes.last?.cols == width)
        await link.stop()
    }

    nonisolated private static func connection() -> SSHConnection {
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession()
        let profile = HostProfile(
            id: UUID(), name: "test", endpoint: HostEndpoint(host: "127.0.0.1", port: 1, user: "u"), auth: .password
        )
        return SSHConnection(
            profile: profile, keys: KeychainKeyStore(secrets: secrets, biometrics: biometrics), secrets: secrets,
            biometrics: biometrics,
            hostKeys: FileHostKeyStore(url: FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)),
            prompter: CountingPrompter()
        )
    }
}

/// A transport that connects at once and records every size it is sent.
private actor SizeRecordingTransport: ByteTransport {
    nonisolated let incoming = ByteStream()
    nonisolated let state: AsyncStream<TransportState>
    private let sink: AsyncStream<TransportState>.Continuation
    private(set) var sizes: [(cols: Int, rows: Int)] = []
    private(set) var connected = false

    init() {
        (state, sink) = AsyncStream<TransportState>.makeStream()
    }

    func connect() async throws {
        connected = true
        sink.yield(.connected)
    }

    func send(_ data: Data) async throws {}

    func resize(cols: Int, rows: Int) async throws {
        sizes.append((cols, rows))
    }

    func close() async {
        sink.yield(.closed(.requested))
        incoming.finish()
    }
}

private final class StillPath: NetworkPathSource {
    let changes = AsyncStream<Void> { _ in }
}

/// Time that moves only when the test says. A sleep returns once `advance` passes its deadline
/// and throws when its task is cancelled first.
final class ManualClock: LinkClock {
    private struct Sleeper {
        let id: UUID
        let deadline: Duration
        let continuation: CheckedContinuation<Void, any Error>
    }

    private struct State {
        var offset: Duration = .zero
        var sleepers: [Sleeper] = []
        var cancelled: Set<UUID> = []
    }

    private let state = Mutex(State())
    private let base = ContinuousClock.now

    var now: ContinuousClock.Instant { base + state.withLock { $0.offset } }

    /// Sleeps that have not finished yet.
    var waiting: Int { state.withLock { $0.sleepers.count } }

    func sleep(for duration: Duration) async throws {
        let id = UUID()
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
                let early = state.withLock { state -> Bool in
                    if state.cancelled.remove(id) != nil { return true }
                    state.sleepers.append(Sleeper(id: id, deadline: state.offset + duration, continuation: continuation))
                    return false
                }
                if early { continuation.resume(throwing: CancellationError()) }
            }
        } onCancel: {
            let sleeper = state.withLock { state -> Sleeper? in
                guard let index = state.sleepers.firstIndex(where: { $0.id == id }) else {
                    state.cancelled.insert(id)
                    return nil
                }
                return state.sleepers.remove(at: index)
            }
            sleeper?.continuation.resume(throwing: CancellationError())
        }
    }

    func advance(_ duration: Duration) {
        let due = state.withLock { state -> [Sleeper] in
            state.offset += duration
            let due = state.sleepers.filter { $0.deadline <= state.offset }
            state.sleepers.removeAll { $0.deadline <= state.offset }
            return due
        }
        for sleeper in due {
            sleeper.continuation.resume()
        }
    }
}

/// Polls until `condition` holds; the link works on its own tasks.
@MainActor private func eventually(_ timeout: Duration = .seconds(3), _ condition: () async -> Bool) async -> Bool {
    let deadline = ContinuousClock.now + timeout
    while ContinuousClock.now < deadline {
        if await condition() { return true }
        try? await Task.sleep(for: .milliseconds(5))
    }
    return await condition()
}
