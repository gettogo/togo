import Foundation
import Testing
import TogoKit
@testable import Togo

/// Ports and forwards only; every git and file call fails.
@MainActor private final class PortServices: ReviewServices {
    var list = ReviewFixtures.ports
    var forwardFailure: (any Error)?
    private(set) var portCalls = 0
    private(set) var forwardCalls: [Int] = []
    private(set) var stopped: [Int] = []
    /// Added to every local port handed out, to make a reopened forward land somewhere new.
    var shift = 0
    private var sinks: [AsyncStream<(localPort: Int, message: String)>.Continuation] = []

    /// A forward reporting something on its own, as `LiveReviewServices` does.
    func emit(localPort: Int, _ message: String) {
        for sink in sinks { sink.yield((localPort, message)) }
    }

    func local(_ remote: Int) -> Int { 50_000 + remote % 1_000 + shift }

    func ports(_ s: ReviewSubject) async throws -> [ListeningPort] {
        portCalls += 1
        return list
    }

    func forward(_ s: ReviewSubject, port: ListeningPort) async throws -> ForwardInfo {
        forwardCalls.append(port.port)
        if let forwardFailure { throw forwardFailure }
        let local = local(port.port)
        // Deliberately not 127.0.0.1: the model builds the address it loads itself.
        return ForwardInfo(remote: port, localPort: local, url: URL(string: "http://localhost:\(local)/")!)
    }

    func stopForward(localPort: Int) async { stopped.append(localPort) }
    func forwardEvents() -> AsyncStream<(localPort: Int, message: String)> {
        let (stream, sink) = AsyncStream.makeStream(of: (localPort: Int, message: String).self)
        sinks.append(sink)
        return stream
    }

    func status(_ s: ReviewSubject) async throws -> GitStatus { throw ReviewUnavailable() }
    func diff(_ s: ReviewSubject, _ source: DiffSource) async throws -> GitDiff { throw ReviewUnavailable() }
    func read(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> FsReadResult {
        throw ReviewUnavailable()
    }
    func listDir(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> ListDirResult {
        throw ReviewUnavailable()
    }
    func log(_ s: ReviewSubject, ref: String?, limit: Int) async throws -> [GitCommit] { throw ReviewUnavailable() }
    func branches(_ s: ReviewSubject) async throws -> GitBranches { throw ReviewUnavailable() }
    func stage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus { throw ReviewUnavailable() }
    func unstage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus { throw ReviewUnavailable() }
    func discard(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus { throw ReviewUnavailable() }
    func commit(_ s: ReviewSubject, message: String, amend: Bool, head: String?) async throws -> GitCommitResult {
        throw ReviewUnavailable()
    }
}

@MainActor struct PortsModelTests {
    private let services = PortServices()

    private func model(poll: Duration = .seconds(60), idle: Duration = .seconds(600)) -> PortsModel {
        let subject = ReviewSubject(
            hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", cwd: "/Users/sam/src/api",
            managed: true, capabilities: [Capability.git, Capability.ports]
        )
        return PortsModel(subject: subject, services: services, pollInterval: poll, idleLimit: idle, copyLink: { _ in })
    }

    private func eventually(_ condition: () -> Bool) async {
        let deadline = ContinuousClock.now + .seconds(3)
        while !condition(), ContinuousClock.now < deadline {
            try? await Task.sleep(for: .milliseconds(10))
        }
    }

    private var node: ListeningPort { ReviewFixtures.ports[0] }

    @Test func manualPortValidated() async {
        let model = model()
        for bad in ["", "0", "65536", "-1", "80a", "3.5", "99999999999999999999"] {
            await model.openManual(bad)
            #expect(model.manualError == PortsCopy.manualRange, "\(bad)")
            #expect(model.previewURL == nil)
        }
        #expect(services.forwardCalls.isEmpty)
        await model.openManual(" 4000 ")
        #expect(model.manualError == nil)
        #expect(services.forwardCalls == [4000])
        #expect(model.previewURL == URL(string: "http://127.0.0.1:50000/"))
        #expect(model.ports.contains { $0.port == 4000 })
    }

    @Test func openSetsPreviewURL() async {
        let model = model()
        await model.refresh()
        await model.open(node)
        #expect(model.previewURL == URL(string: "http://127.0.0.1:50000/"))
        guard case .open(let info)? = model.states[3000] else {
            Issue.record("not open")
            return
        }
        #expect(info.localPort == 50_000)
    }

    @Test func wildcardAndLoopbackMapping() {
        let wildcard = ListeningPort(port: 5173, address: "0.0.0.0", pid: 1, command: "vite")
        #expect(ForwardTarget(wildcard)?.host == "127.0.0.1")
        for address in ["127.0.0.1", "0.0.0.0", "*", "::", "::1", "[::1]"] {
            let port = ListeningPort(port: 3000, address: address, pid: 1, command: "node")
            #expect(PortsModel.isPreviewable(port), "\(address)")
            #expect(PortsModel.addressNote(port) == nil, "\(address)")
        }
    }

    @Test func nonLoopbackListedButNotPreviewable() async {
        let lan = ListeningPort(port: 9229, address: "192.168.1.5", pid: 77, command: "node")
        services.list = ReviewFixtures.ports + [lan]
        let model = model()
        await model.refresh()
        #expect(model.ports.contains(lan))
        #expect(!PortsModel.isPreviewable(lan))
        #expect(PortsModel.addressNote(lan) == "Listening on 192.168.1.5 only, so it can't be previewed from here.")
        await model.open(lan)
        #expect(services.forwardCalls.isEmpty)
        #expect(model.previewURL == nil)
    }

    @Test func rowLabelInWords() {
        #expect(PortsCopy.rowLabel(node) == "3000, node, pid 4121")
        #expect(PortsCopy.detail(node) == "node, pid 4121")
        let hostile = ListeningPort(port: 3000, address: "127.0.0.1", pid: 7, command: "no\u{202E}de\u{1B}[2J")
        #expect(PortsCopy.detail(hostile) == "no⟨U+202E⟩de⟨U+001B⟩[2J, pid 7")
    }

    @Test func dismissStopsForward() async {
        let model = model()
        await model.open(node)
        // What the sheet's binding does when the user taps Done or swipes it away.
        model.preview = nil
        await eventually { !services.stopped.isEmpty }
        #expect(services.stopped == [50_000])
        #expect(model.states[3000] == nil)
        #expect(model.previewURL == nil)
    }

    @Test func dismissKeepsForwardWithCopiedLink() async {
        var copied: URL?
        let subject = ReviewSubject(
            hostID: Samples.studio, sessionID: "s-01", bib: nil, title: "api", cwd: "/", managed: true,
            capabilities: [Capability.ports]
        )
        let model = PortsModel(subject: subject, services: services, copyLink: { copied = $0 })
        await model.copyLink(node)
        #expect(copied == URL(string: "http://127.0.0.1:50000/"))
        await model.open(node)
        #expect(services.forwardCalls == [3000])
        model.preview = nil
        try? await Task.sleep(for: .milliseconds(50))
        #expect(services.stopped.isEmpty)
        model.disappear()
        await eventually { services.stopped == [50_000] }
        #expect(services.stopped == [50_000])
    }

    @Test func portGoneMarksRow() async {
        let model = model()
        await model.refresh()
        await model.open(node)
        services.list = Array(ReviewFixtures.ports.dropFirst())
        await model.refresh()
        #expect(model.ports.contains(node))
        #expect(model.gone == [3000])
        #expect(model.previewURL != nil)
        model.preview = nil
        await eventually { !model.ports.contains(node) }
        #expect(!model.ports.contains(node))
    }

    @Test func forwardReopensOnForeground() async {
        let model = model()
        await model.open(node)
        model.setForeground(false)
        services.emit(localPort: 50_000, "The preview stopped. Open it again to keep going.")
        await Task.yield()
        try? await Task.sleep(for: .milliseconds(50))
        model.setForeground(true)
        await eventually { services.forwardCalls.count == 2 }
        await eventually { if case .open = model.states[3000] { true } else { false } }
        guard case .open(let info)? = model.states[3000] else {
            Issue.record("not reopened: \(String(describing: model.states[3000]))")
            return
        }
        #expect(info.localPort == 50_000)
        #expect(model.previewURL == URL(string: "http://127.0.0.1:50000/"))
    }

    @Test func reopenOnNewLocalPortIsNewPreview() async {
        let model = model()
        await model.open(node)
        let before = model.preview
        model.setForeground(false)
        services.emit(localPort: 50_000, "The preview stopped. Open it again to keep going.")
        try? await Task.sleep(for: .milliseconds(50))
        services.shift = 7
        model.setForeground(true)
        await eventually { model.previewURL == URL(string: "http://127.0.0.1:50007/") }
        #expect(model.previewURL == URL(string: "http://127.0.0.1:50007/"))
        // A different identity makes the sheet load the page again at the new address.
        #expect(model.preview?.id != before?.id)
        #expect(services.stopped.isEmpty)
    }

    @Test func twoModelsOnOneServicesBothHearEvents() async {
        let fake = FakeReviewServices()
        let subject = ReviewSubject(
            hostID: Samples.studio, sessionID: "s-01", bib: nil, title: "api", cwd: "/", managed: true,
            capabilities: [Capability.ports]
        )
        let first = PortsModel(subject: subject, services: fake, copyLink: { _ in })
        let second = PortsModel(subject: subject, services: fake, copyLink: { _ in })
        await first.open(node)
        await second.open(node)
        let local = 50_000
        fake.endForward(localPort: local, message: ReviewCopy.nothingListening(3000))
        await eventually { first.notes[3000] != nil && second.notes[3000] != nil }
        #expect(first.notes[3000] == ReviewCopy.nothingListening(3000))
        #expect(second.notes[3000] == ReviewCopy.nothingListening(3000))
        first.disappear()
        second.disappear()
        // A third model after the others stopped listening still hears events.
        let third = PortsModel(subject: subject, services: fake, copyLink: { _ in })
        await third.open(node)
        fake.endForward(localPort: local, message: ReviewCopy.nothingListening(3000))
        await eventually { third.notes[3000] != nil }
        #expect(third.notes[3000] == ReviewCopy.nothingListening(3000))
    }

    @Test func manualPortOnLanOnlyAddressExplained() async {
        services.list = [ListeningPort(port: 9229, address: "192.168.1.5", pid: 77, command: "node")]
        let model = model()
        await model.refresh()
        await model.openManual("9229")
        #expect(model.manualError == "Listening on 192.168.1.5 only, so it can't be previewed from here.")
        #expect(services.forwardCalls.isEmpty)
    }

    @Test func backgroundPausesPollingAndKeepsForwards() async {
        let model = model(poll: .milliseconds(20))
        model.appear()
        await model.open(node)
        model.setForeground(false)
        try? await Task.sleep(for: .milliseconds(30))
        let calls = services.portCalls
        try? await Task.sleep(for: .milliseconds(150))
        #expect(services.portCalls == calls)
        #expect(services.stopped.isEmpty)
        #expect(model.previewURL != nil)
        model.setForeground(true)
        await eventually { services.portCalls > calls }
        #expect(services.portCalls > calls)
        model.disappear()
    }

    @Test func refusedUpstreamShownInline() async {
        let model = model()
        await model.open(node)
        services.emit(localPort: 50_000, ReviewCopy.nothingListening(3000))
        await eventually { model.notes[3000] != nil }
        #expect(model.notes[3000] == "Nothing is listening on 3000 any more")
        guard case .open? = model.states[3000] else {
            Issue.record("refused upstream closed the forward")
            return
        }
        services.emit(localPort: 50_000, "The preview stopped. Open it again to keep going.")
        await eventually { model.states[3000] == .failed("The preview stopped. Open it again to keep going.") }
        #expect(model.states[3000] == .failed("The preview stopped. Open it again to keep going."))
    }

    @Test func forwardFailureShownInline() async {
        services.forwardFailure = ReviewFixtureError.offline
        let model = model()
        await model.open(node)
        #expect(model.states[3000] == .failed("The machine didn't answer."))
        #expect(model.previewURL == nil)
    }

    @Test func idleStopsForwards() async {
        let model = model(idle: .milliseconds(60))
        await model.open(node)
        await eventually { !services.stopped.isEmpty }
        #expect(services.stopped == [50_000])
        #expect(model.previewURL == nil)
        #expect(model.states[3000] == nil)
        #expect(model.notes[3000] == PortsCopy.idleStopped)
    }

    @Test func activityPostponesIdleStop() async {
        let model = model(idle: .milliseconds(150))
        await model.open(node)
        for _ in 0..<4 {
            try? await Task.sleep(for: .milliseconds(60))
            model.touch()
        }
        #expect(services.stopped.isEmpty)
        await eventually { !services.stopped.isEmpty }
        #expect(services.stopped == [50_000])
    }

    @Test func pollsOnlyWhileVisible() async {
        let model = model(poll: .milliseconds(20))
        try? await Task.sleep(for: .milliseconds(60))
        #expect(services.portCalls == 0)
        model.appear()
        await eventually { services.portCalls >= 3 }
        #expect(services.portCalls >= 3)
        model.disappear()
        try? await Task.sleep(for: .milliseconds(30))
        let calls = services.portCalls
        try? await Task.sleep(for: .milliseconds(150))
        #expect(services.portCalls == calls)
    }

    @Test func leavingScreenStopsForwards() async {
        let model = model()
        model.appear()
        await model.open(node)
        model.disappear()
        await eventually { services.stopped == [50_000] }
        #expect(services.stopped == [50_000])
        #expect(model.previewURL == nil)
    }
}
