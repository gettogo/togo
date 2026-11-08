import Foundation
import Testing
import TogoKit
import TogoPush
@testable import Togo

private let studio = "h_mmg42klgyqzwneiskrelxms3j7"
private let buildBox = "h_qqg42klgyqzwneiskrelxms3j7"
private let start = Date(timeIntervalSince1970: 1_793_707_200)
private let now = start.addingTimeInterval(60)

private func needsYou(_ requestId: String?, since: Date) -> TogoActivityAttributes.ContentState {
    .init(
        bib: "04", agent: "Claude Code", status: "needs_you", waiting: 1, since: Int(since.timeIntervalSince1970),
        requestId: requestId, command: nil
    )
}

@MainActor final class FakeActivity: LiveActivityHandle {
    let id: String
    let hostId: String
    let content: TogoActivityAttributes.ContentState
    let staleDate: Date?
    let pushTokens: AsyncStream<Data>
    let tokens: AsyncStream<Data>.Continuation
    var ended = false

    init(id: String, hostId: String, state: TogoActivityAttributes.ContentState, staleDate: Date? = nil) {
        self.id = id
        self.hostId = hostId
        content = state
        self.staleDate = staleDate
        (pushTokens, tokens) = AsyncStream.makeStream()
    }

    var state: TogoActivityAttributes.ContentState? { ended ? nil : content }

    func end() async { ended = true }
}

@MainActor final class FakeActivitySystem: LiveActivitySystem {
    let startTokens: AsyncStream<Data>
    let startContinuation: AsyncStream<Data>.Continuation
    let created: AsyncStream<any LiveActivityHandle>
    let createdContinuation: AsyncStream<any LiveActivityHandle>.Continuation
    var activities: [FakeActivity] = []
    var requested: [FakeActivity] = []

    init() {
        (startTokens, startContinuation) = AsyncStream.makeStream()
        (created, createdContinuation) = AsyncStream.makeStream()
    }

    var running: [any LiveActivityHandle] { activities.filter { !$0.ended } }

    func request(
        hostId: String, state: TogoActivityAttributes.ContentState, staleDate: Date?
    ) throws -> any LiveActivityHandle {
        let activity = FakeActivity(id: "a\(activities.count)", hostId: hostId, state: state, staleDate: staleDate)
        activities.append(activity)
        requested.append(activity)
        return activity
    }

    /// Ends every stream so `start()` returns once it has handled what was queued.
    func finish() {
        startContinuation.finish()
        createdContinuation.finish()
        for activity in activities { activity.tokens.finish() }
    }
}

@MainActor final class TokenLog {
    var calls: [(update: String?, start: String?)] = []
}

@MainActor
struct LiveActivityControllerTests {
    /// The widget reads the "show command text" machines where the app's device settings
    /// keep them, so the two keys must stay one.
    @Test func widgetReadsTheCommandChoiceTheAppSaves() throws {
        let defaults = try #require(UserDefaults(suiteName: "device-\(UUID().uuidString)"))
        let machine = UUID()
        var settings = AppSettings()
        settings.liveActivityCommandMachines = [machine]
        DeviceSettings(backend: DefaultsBackend(defaults: defaults, namespace: "device.")).save(settings)
        #expect(SharedSettings.readLiveActivityCommandMachines(defaults) == [machine])
    }

    let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)

    func pairing(_ hostId: String, name: String, command: Bool = false) -> Pairing {
        Pairing(
            hostId: hostId, hostKey: "k", relayURL: URL(string: "https://relay.example")!, machineName: name,
            appHostID: nil, pairedAt: start, tokenHash: "abcdef", liveActivityCommand: command
        )
    }

    func request(_ id: String, host: String, bib: String, seconds: TimeInterval, command: String? = nil) -> PendingRequest {
        PendingRequest(
            requestId: id, hostId: host, sessionId: "s1", bib: bib, agent: "Claude Code", host: nil,
            title: "Run a command?", subject: command.map { PushSubject(kind: "command", text: $0) }, options: [],
            receivedAt: start.addingTimeInterval(seconds), answered: nil
        )
    }

    func make(
        _ system: FakeActivitySystem, log: TokenLog, retired: Set<String> = []
    ) -> LiveActivityController {
        let pairings = PairingStore(directory: directory)
        pairings.put(pairing(studio, name: "Studio"))
        pairings.put(pairing(buildBox, name: "Build box", command: true))
        return LiveActivityController(
            pairings: pairings, system: system, clock: { now }, isRetired: { retired.contains($0) }
        ) { update, start in
            await MainActor.run { log.calls.append((update, start)) }
        }
    }

    @Test func tokensForwardedAsHex() async {
        let system = FakeActivitySystem()
        let log = TokenLog()
        let controller = make(system, log: log)
        let activity = FakeActivity(id: "a", hostId: studio, state: .init(
            bib: "04", agent: "Claude Code", status: "needs_you", waiting: 1, since: 0, requestId: nil, command: nil
        ))
        system.activities.append(activity)
        system.startContinuation.yield(Data([0x0A, 0xFF, 0x00]))
        system.createdContinuation.yield(activity)
        activity.tokens.yield(Data([0xDE, 0xAD, 0xBE, 0xEF]))
        system.finish()

        await controller.start()

        #expect(controller.startToken == "0aff00")
        #expect(controller.updateToken(hostId: studio) == "deadbeef")
        #expect(log.calls.contains { $0.update == "deadbeef" })
        #expect(log.calls.last?.start == "0aff00" || log.calls.contains { $0.start == "0aff00" })
    }

    @Test func startTokenForwardedOnce() async {
        let system = FakeActivitySystem()
        let log = TokenLog()
        let controller = make(system, log: log)
        system.startContinuation.yield(Data([0x01, 0x02]))
        system.startContinuation.yield(Data([0x01, 0x02]))
        system.finish()

        await controller.start()

        #expect(log.calls.count == 1)
        #expect(log.calls.first?.start == "0102")
        #expect(log.calls.first?.update == nil)
    }

    @Test func inAppStartOnlyWhenNoneLive() async throws {
        let system = FakeActivitySystem()
        let controller = make(system, log: TokenLog())
        system.activities.append(FakeActivity(id: "live", hostId: studio, state: .init(
            bib: "04", agent: "Claude Code", status: "working", waiting: 0, since: 0, requestId: nil, command: nil
        )))
        var answered = request("r_0", host: buildBox, bib: "09", seconds: 0)
        answered.answered = .approved
        let pending = [
            request("r_1", host: studio, bib: "04", seconds: 10),
            answered,
            request("r_2", host: buildBox, bib: "07", seconds: 30, command: "rm -rf build"),
            request("r_3", host: buildBox, bib: "08", seconds: 20),
        ]

        controller.startForPending(pending)

        #expect(system.requested.count == 1)
        let started = try #require(system.requested.first)
        #expect(started.hostId == buildBox)
        #expect(started.staleDate == start.addingTimeInterval(620))
        #expect(started.content.status == "needs_you")
        #expect(started.content.waiting == 2)
        #expect(started.content.bib == "08")
        #expect(started.content.requestId == "r_3")
        #expect(started.content.since == Int(start.addingTimeInterval(20).timeIntervalSince1970))

        controller.startForPending(pending)
        #expect(system.requested.count == 1)
    }

    @Test func commandOnlyForMachinesThatOptedIn() throws {
        let system = FakeActivitySystem()
        let controller = make(system, log: TokenLog())

        controller.startForPending([
            request("r_1", host: studio, bib: "04", seconds: 0, command: "git push --force"),
            request("r_2", host: buildBox, bib: "07", seconds: 0, command: "make deploy"),
        ])

        let byHost = Dictionary(uniqueKeysWithValues: system.requested.map { ($0.hostId, $0.content) })
        #expect(byHost[studio]?.command == nil)
        #expect(byHost[buildBox]?.command == "make deploy")
    }

    @Test func endsOnMachineRemoved() async {
        let system = FakeActivitySystem()
        let log = TokenLog()
        let controller = make(system, log: log)
        let state = TogoActivityAttributes.ContentState(
            bib: "04", agent: "Claude Code", status: "needs_you", waiting: 1, since: 0, requestId: "r_1", command: nil
        )
        let gone = FakeActivity(id: "a", hostId: studio, state: state)
        let kept = FakeActivity(id: "b", hostId: buildBox, state: state)
        system.activities = [gone, kept]
        gone.tokens.yield(Data([0xAB]))
        system.finish()
        await controller.start()

        await controller.machineRemoved(hostId: studio)

        #expect(gone.ended)
        #expect(!kept.ended)
        #expect(controller.updateToken(hostId: studio) == nil)
    }

    @Test func expiredRequestsStartNothing() {
        let system = FakeActivitySystem()
        let controller = make(system, log: TokenLog())

        controller.startForPending([request("r_1", host: studio, bib: "04", seconds: -600)])

        #expect(system.requested.isEmpty)
    }

    @Test func reconcileEndsActivitiesWhoseRequestIsOver() async {
        let system = FakeActivitySystem()
        let controller = make(system, log: TokenLog())
        var answered = request("r_answered", host: studio, bib: "04", seconds: 0)
        answered.answered = .approved
        let done = FakeActivity(id: "answered", hostId: studio, state: needsYou("r_answered", since: start))
        let unknownOld = FakeActivity(id: "old", hostId: "h_old", state: needsYou("r_gone", since: now.addingTimeInterval(-700)))
        let unknownNew = FakeActivity(id: "new", hostId: "h_new", state: needsYou("r_new", since: now.addingTimeInterval(-30)))
        let stillOpen = FakeActivity(id: "open", hostId: buildBox, state: needsYou("r_first", since: start))
        let working = FakeActivity(id: "working", hostId: "h_work", state: .init(
            bib: "02", agent: "Codex", status: "working", waiting: 0, since: 0, requestId: nil, command: nil
        ))
        system.activities = [done, unknownOld, unknownNew, stillOpen, working]

        await controller.reconcile([answered, request("r_next", host: buildBox, bib: "07", seconds: 20)])

        #expect(done.ended)
        #expect(unknownOld.ended)
        #expect(!unknownNew.ended, "a request the store never saw stays until its window closes")
        #expect(!stillOpen.ended, "the machine still has an open request")
        #expect(!working.ended)

        await controller.reconcile([], cleared: ["r_new"])
        #expect(unknownNew.ended)
    }

    @Test func appOpenedStartsOnlyWhenTurnedOn() async {
        let system = FakeActivitySystem()
        let controller = make(system, log: TokenLog())
        let pending = [request("r_1", host: studio, bib: "04", seconds: 0)]

        await controller.appOpened(pending, startingActivities: false)
        #expect(system.requested.isEmpty)

        await controller.appOpened(pending, startingActivities: true)
        #expect(system.requested.map(\.hostId) == [studio])
    }

    @Test func removedMachineActivitiesEnd() async {
        let system = FakeActivitySystem()
        let controller = make(system, log: TokenLog(), retired: ["h_removed"])
        let pushed = FakeActivity(id: "pushed", hostId: "h_removed", state: needsYou("r_1", since: now))
        let earlier = FakeActivity(id: "earlier", hostId: "h_removed", state: needsYou("r_0", since: now))
        let kept = FakeActivity(id: "kept", hostId: studio, state: needsYou("r_2", since: now))
        system.activities = [earlier, kept]
        system.createdContinuation.yield(pushed)
        system.finish()

        await controller.start()
        await controller.reconcile([request("r_2", host: studio, bib: "04", seconds: 50)])

        #expect(pushed.ended, "a removed machine's helper may still start one before it unpairs")
        #expect(earlier.ended)
        #expect(!kept.ended)
    }

    @Test func contentStateDecodesWithMissingFields() throws {
        let decoder = JSONDecoder()
        let empty = try decoder.decode(TogoActivityAttributes.ContentState.self, from: Data("{}".utf8))
        #expect(empty.agent == "shell")
        #expect(empty.status == "working")
        #expect(empty.waiting == 0)
        #expect(abs(TimeInterval(empty.since) - Date().timeIntervalSince1970) < 60)
        #expect(empty.bib == nil && empty.requestId == nil && empty.command == nil)

        let json = #"{"bib":"04","agent":"Codex","status":"needs_you","waiting":"two","since":1793707200,"color":"red"}"#
        let partial = try decoder.decode(TogoActivityAttributes.ContentState.self, from: Data(json.utf8))
        #expect(partial.bib == "04")
        #expect(partial.agent == "Codex")
        #expect(partial.status == "needs_you")
        #expect(partial.waiting == 0, "a field of the wrong type takes its default too")
        #expect(partial.since == 1_793_707_200)

        let full = needsYou("r_1", since: start)
        let round = try decoder.decode(TogoActivityAttributes.ContentState.self, from: JSONEncoder().encode(full))
        #expect(round == full)
    }
}
