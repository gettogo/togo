import Foundation
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor struct ConnectionProgressTests {
    private func event(_ id: UUID, host: UUID, kind: HostRoute.Kind = .tailscale,
                       outcome: ConnectionAttemptEvent.Outcome = .started, purpose: ConnectionPurpose = .requests) -> ConnectionAttemptEvent {
        .init(id: id, hostID: host, stage: .route(kind), outcome: outcome,
              started: .now - .seconds(3), elapsedMilliseconds: outcome == .started ? 0 : 3000, purpose: purpose)
    }

    @Test func overlappingRoutesAndMachinesRetireByAttemptNotByHost() async throws {
        let progress = ConnectionProgress(), host = UUID(), other = UUID(), a = UUID(), b = UUID(), c = UUID()
        progress.report(event(a, host: host))
        progress.report(event(b, host: host, kind: .lan))
        progress.report(event(c, host: other))
        try await until { progress.summary(for: host)?.kinds.count == 2 && progress.summary(for: other) != nil }
        progress.report(event(a, host: host, outcome: .failed))
        try await until { progress.summary(for: host)?.kinds == [.lan] }
        progress.report(event(a, host: host, outcome: .cancelled)) // late duplicate cannot clear b
        progress.report(event(b, host: host, kind: .lan, outcome: .succeeded))
        try await until { progress.summary(for: host) == nil }
        #expect(progress.summary(for: other) != nil)
        progress.report(event(c, host: other, outcome: .cancelled))
        try await until { progress.summary(for: other) == nil }
    }

    @Test func aTerminalAttemptCannotBeMistakenForTheNativeChatsConnection() async throws {
        let progress = ConnectionProgress(), host = UUID(), attempt = UUID()
        progress.report(event(attempt, host: host, purpose: .terminal))
        try await until { progress.summary(for: host) != nil }
        #expect(progress.summary(for: host, purpose: .requests) == nil)
        #expect(progress.summary(for: host, purpose: .terminal) != nil)
        progress.report(event(attempt, host: host, outcome: .cancelled, purpose: .terminal))
        try await until { progress.summary(for: host) == nil }
    }

    @Test func aBurstEndingBeforeTheUiReadsDoesNotLeaveAGhostAttempt() async throws {
        let progress = ConnectionProgress(), host = UUID()
        for _ in 0..<300 {
            let id = UUID()
            progress.report(event(id, host: host))
            progress.report(event(id, host: host, outcome: .failed))
        }
        // Place a live marker last so the consumer must have read the current snapshot.
        let marker = UUID()
        progress.report(event(marker, host: host, kind: .lan))
        try await until { progress.summary(for: host)?.kinds == [.lan] }
        progress.report(event(marker, host: host, kind: .lan, outcome: .cancelled))
        try await until { progress.summary(for: host) == nil }
    }

    @Test func diagnosticsKeepRouteOutcomesAndElapsedTimeWithoutAddresses() async throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: "connection-\(UUID())")
        let recorder = DiagnosticsRecorder(directory: directory), host = UUID(), id = UUID()
        defer { try? FileManager.default.removeItem(at: directory) }
        recorder.connectionAttempt(event(id, host: host))
        recorder.connectionAttempt(event(id, host: host, outcome: .succeeded))
        await recorder.settle()
        let events = await recorder.log.events().filter { $0.name == "connection_attempt" }
        #expect(events.count == 2)
        #expect(events.last?.fields["route"] == .word("tailscale"))
        #expect(events.last?.fields["phase"] == .word("connected"))
        #expect(events.last?.fields["purpose"] == .word("requests"))
        #expect(events.last?.fields["ms"] == .int(3000))
        let attempt = try #require(events.first?.fields["attempt"])
        #expect(attempt == events.last?.fields["attempt"])
        #expect(events.last?.fields.keys.contains("host") == false)
        #expect(events.last?.fields.keys.contains("user") == false)
    }

    @Test func theClockIsMonotonicAndCachedStatusIsExplicitlyLastKnown() {
        let now = ContinuousClock.now
        let summary = ConnectionProgress.Summary(started: now, kinds: [.tailscale], preparing: false, origin: Samples.now)
        #expect(summary.seconds(at: now + .seconds(12)) == 12)
        #expect(summary.label.contains("Tailscale"))
        #expect(ControlCenterCopy.lastKnown("Idle") == "Last known: Idle")
        #expect(ControlCenterCopy.lastKnown("Needs You") == "Last known: Needs You")
        #expect(RosterSection.caption(.connecting, helper: .checking, lastSeen: nil, now: .now) == ControlCenterCopy.connecting)
    }

    @Test(arguments: [240.0, 688.0])
    func progressWrapsInCompactAndWideHeadings(width: Double) {
        let summary = ConnectionProgress.Summary(started: .now, kinds: [.tailscale], preparing: false, origin: Samples.now)
        let host = UIHostingController(rootView: RosterMachineHeading(name: "Development Machine", count: 6,
            caption: nil, progress: summary).environment(\.dynamicTypeSize, .accessibility5))
        let size = host.sizeThatFits(in: CGSize(width: width, height: 2000))
        #expect(size.width <= width + 0.5 && size.height > 0)
    }

    private func until(_ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(2)
        while !condition(), ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
        try #require(condition())
    }
}

@MainActor struct InitialHarnessRetryTests {
    @Test func aSuccessfulDescribeEndsTheColdPhaseBeforeHistoryLoads() async throws {
        let stream = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        services.feeds = [.init(stream: stream.stream)]
        let store = HarnessSessionStore(subject: ChatTests.subject, services: services,
                                        retry: (.milliseconds(5), .milliseconds(20)))
        store.launch()
        defer { store.stop(); stream.continuation.finish() }
        let deadline = ContinuousClock.now + .seconds(2)
        while !store.hasConnected, ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
        try #require(store.hasConnected)
        #expect(!store.feed.isLoaded)
        var delay: Duration = .milliseconds(5)
        #expect(await store.pause(&delay))
        #expect(delay == .milliseconds(10))
    }

    @Test func onlyTheInitialPauseIsFlatAndAnOpenedFeedKeepsExponentialBackoff() async throws {
        let store = HarnessSessionStore(subject: ChatTests.subject, services: FakeHarnessServices(descriptor: ChatTests.claude),
                                        retry: (.milliseconds(5), .milliseconds(20)))
        var delay: Duration = .milliseconds(5)
        for _ in 0..<4 {
            #expect(await store.pause(&delay))
            #expect(delay == .milliseconds(5))
        }
        store.feed.load(try HarnessFixtures.open())
        #expect(await store.pause(&delay))
        #expect(delay == .milliseconds(10))
        #expect(await store.pause(&delay))
        #expect(delay == .milliseconds(20))
    }
}
