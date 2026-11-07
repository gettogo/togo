import Foundation
import Synchronization
import Testing
import TogoKit
import struct TogoPush.Pairing
@testable import Togo

@MainActor struct LiveServicesTests {
    private let now = Samples.now

    private func record(_ id: HostID, _ name: String, noHelper: Bool = false) -> HostRecord {
        HostRecord(
            id: id,
            profile: HostProfile(id: id, name: name, endpoint: HostEndpoint(host: "\(name).local", port: 22, user: "sam"), auth: .password),
            symbol: "desktop", noHelper: noHelper
        )
    }

    private func services(
        watches: Watches, helpers: NoHelper = NoHelper(), local: MemoryBackend = MemoryBackend(),
        remote: MemoryBackend? = nil, syncOn: Bool = true,
        forgotPasswords: ForgottenPasswords? = nil, reported: @escaping (HostID, Set<String>) -> Void = { _, _ in }
    ) -> LiveServices {
        let now = self.now
        let syncSwitch = SyncSwitch(backend: MemoryBackend())
        syncSwitch.set(syncOn)
        return LiveServices(
            hostRecords: SyncedRecordStore(
                prefix: "host.", local: local, remote: remote, deviceID: "a", clock: { now }, syncEnabled: syncOn
            ),
            settingsRecords: SyncedRecordStore(
                prefix: "settings.", local: MemoryBackend(), remote: remote, deviceID: "a", clock: { now }, syncEnabled: syncOn
            ),
            syncSwitch: syncSwitch,
            dependencies: LiveServices.Dependencies(
                watch: { record in
                    let watch = FakeWatch(record: record)
                    watches.made.withLock { $0[record.id] = watch }
                    watches.history.withLock { $0.append(watch) }
                    return watch
                },
                helpers: helpers,
                connect: { _, _, _, _ in { _, _, _ in throw NoHelper.Unreachable() } },
                signIn: { record, prompter in
                    _ = await prompter.answer(.password(user: record.profile.endpoint.user, host: record.profile.endpoint.host))
                },
                forgetPassword: { id in forgotPasswords?.ids.withLock { $0.append(id) } },
                now: { now },
                reported: reported
            )
        )
    }

    private func eventually(_ condition: () -> Bool) async {
        for _ in 0..<200 where !condition() {
            try? await Task.sleep(for: .milliseconds(5))
        }
    }

    /// A tab restored at launch asks for its terminal while `start()` is still deferred; its
    /// machine is listed from this device, so it waits instead of failing as unknown.
    @Test func aTerminalAskedForBeforeTheStoresLoadWaitsForItsMachine() async throws {
        let local = MemoryBackend()
        let earlier = services(watches: Watches(), local: local)
        await earlier.start()
        try await earlier.saveHost(record(Samples.studio, "Studio"))
        let services = services(watches: Watches(), local: local)
        services.showLocalHosts()
        #expect(services.hosts.map(\.id) == [Samples.studio])
        let arrived = Mutex<[HostID]>([])
        services.onMachineArrived = { id in arrived.withLock { $0.append(id) } }
        let waiting = services.terminal(hostID: Samples.studio, sessionID: "s1", transport: nil, userStarted: true)
        let missing = services.terminal(hostID: Samples.build, sessionID: "s2", transport: nil, userStarted: true)
        var phases = waiting.phases.makeAsyncIterator()
        #expect(await phases.next() == .connecting)

        await services.start()
        #expect(arrived.withLock { $0 } == [Samples.studio])
        var missingPhases = missing.phases.makeAsyncIterator()
        #expect(await missingPhases.next() == .connecting)
        #expect(await missingPhases.next() == .failed(LiveServiceError.unknownMachine.localizedDescription))
        let late = services.terminal(hostID: Samples.build, sessionID: "s3", transport: nil, userStarted: true)
        var latePhases = late.phases.makeAsyncIterator()
        #expect(await latePhases.next() == .failed(LiveServiceError.unknownMachine.localizedDescription))
    }

    @Test func snapshotMapping() async throws {
        let watches = Watches()
        let services = services(watches: watches)
        await services.start()
        try await services.saveHost(record(Samples.studio, "Studio"))
        #expect(services.hosts.map(\.name) == ["Studio"])
        let watch = try #require(watches[Samples.studio])

        let session = Session(
            id: "s7", bib: "07", name: "app", agent: "claude", cwd: "/Users/sam/src/app", createdAt: now.addingTimeInterval(-600),
            status: .needsYou, activity: "Wants to run npm test", attachedClients: 0, managed: true
        )
        let approval = Approval(
            requestId: "r1", sessionId: "s7", bib: nil, agent: "claude", source: .hook, title: "Run npm test?",
            detail: "npm test", snippet: "", options: [ApprovalOption(n: 1, label: "Yes", role: .approve)],
            allowText: false, createdAt: now, expiresAt: nil
        )
        watch.sink.yield(HostSnapshot(
            hostID: Samples.studio, reach: .online, helper: .present(version: "0.3.0"), sessions: [session],
            approvals: [approval], lastSeen: now, capabilities: [Capability.events, Capability.upload]
        ))

        var stream = services.snapshots().makeAsyncIterator()
        let mapped = try #require(await stream.next())
        #expect(mapped.reachability == .online)
        #expect(mapped.helper == .present(version: "0.3.0", live: true))
        #expect(mapped.capabilities.contains(Capability.upload))
        let row = try #require(mapped.sessions.first)
        #expect(row.status == .needsYou)
        #expect(row.agentName == "Claude Code")
        #expect(row.statusSince == now)
        let card = try #require(mapped.requests.first)
        #expect(card.id == "r1")
        #expect(card.sessionID == "s7")
        #expect(card.bib == "07")
        #expect(card.title == "Run npm test?")
        #expect(card.phase == .open)

        let store = AppStore(services: services)
        store.apply(mapped)
        #expect(store.controlCenter.machines.first?.sessions.first?.status == .needsYou)
        #expect(store.controlCenter.requests.map(\.id) == ["r1"])
        await store.answer("r1", .option(1))
        #expect(watch.ops.withLock { $0 } == ["answer r1"])
    }

    @Test func closedRequestsMapToTheirOutcome() {
        var mapper = SnapshotMapper()
        var snapshot = HostSnapshot(hostID: Samples.studio, reach: .online)
        snapshot.closed = [
            ClosedApproval(requestId: "desk", resolution: .desk),
            ClosedApproval(requestId: "late", resolution: .expired),
            ClosedApproval(requestId: "gone", resolution: .gone),
            ClosedApproval(requestId: "phone", resolution: .phone),
        ]
        let mapped = mapper.map(snapshot, noHelper: false, now: now)
        #expect(mapped.closed == [
            "desk": .answeredElsewhere, "late": .expired, "gone": .expired, "phone": .answeredElsewhere,
        ])
    }

    @Test func missingKeyMapsToItsOwnState() {
        let snapshot = HostSnapshot(hostID: Samples.studio, reach: .keyMissing)
        #expect(SnapshotMapper.reachability(snapshot, now: now) == .keyMissing)
    }

    @Test func launchWithSyncOffMergesNothingFromICloud() async throws {
        let remote = MemoryBackend()
        let theirs = record(Samples.studio, "Studio")
        remote.set(
            try JSONEncoder().encode(Stamped(value: theirs, updatedAt: now, deviceID: "b")),
            forKey: "host.\(Samples.studio.uuidString)"
        )
        let services = services(watches: Watches(), remote: remote, syncOn: false)
        await services.start()
        #expect(services.hosts.isEmpty)
        #expect(!services.settings.syncEnabled)
    }

    @Test func userStartedHelperCallsMayAskAndTheBackgroundFetchNever() async throws {
        let watches = Watches()
        let helpers = NoHelper(asks: true)
        let services = services(watches: watches, helpers: helpers)
        await services.start()
        try await services.saveHost(record(Samples.studio, "Studio"))
        let watch = try #require(watches[Samples.studio])

        watch.sink.yield(HostSnapshot(hostID: Samples.studio, reach: .online, helper: .present(version: "0.3.0")))
        await eventually { helpers.mayAsk.withLock { $0 } == [false] }
        #expect(helpers.mayAsk.withLock { $0 } == [false])

        let prompts = services.attachPrompts
        let fetch = Task { try await services.agents(hostID: Samples.studio) }
        await eventually { prompts.current != nil }
        let question = try #require(prompts.current)
        #expect(question.hostID == Samples.studio)
        prompts.cancel(question.id)
        await #expect(throws: NoHelper.Unreachable.self) { try await fetch.value }
        #expect(helpers.mayAsk.withLock { $0 } == [false, true])
        #expect(prompts.current == nil)
    }

    @Test func helperCallsInTheBackgroundNeverAsk() async throws {
        let helpers = NoHelper(asks: true)
        let services = services(watches: Watches(), helpers: helpers)
        await services.start()
        try await services.saveHost(record(Samples.studio, "Studio"))
        services.setForeground(false)
        await #expect(throws: NoHelper.Unreachable.self) {
            try await services.capture(hostID: Samples.studio, sessionID: "s1", lines: 10)
        }
        #expect(services.attachPrompts.current == nil)
    }

    @Test func removingAMachineForgetsItsSavedPassword() async throws {
        let forgotten = ForgottenPasswords()
        let services = services(watches: Watches(), forgotPasswords: forgotten)
        await services.start()
        try await services.saveHost(record(Samples.studio, "Studio"))
        try await services.removeHost(Samples.studio)
        #expect(forgotten.ids.withLock { $0 } == [Samples.studio])
    }

    @Test func statusSinceMovesOnlyWithTheStatus() {
        var mapper = SnapshotMapper()
        func snapshot(_ status: SessionStatus) -> HostSnapshot {
            HostSnapshot(hostID: Samples.studio, reach: .online, sessions: [Session(
                id: "s1", bib: "01", name: "api", agent: "codex", cwd: "/", createdAt: now.addingTimeInterval(-9_000),
                status: status, activity: nil, attachedClients: 0, managed: true
            )])
        }
        #expect(mapper.map(snapshot(.working), noHelper: false, now: now).sessions[0].statusSince == now)
        let later = now.addingTimeInterval(60)
        #expect(mapper.map(snapshot(.working), noHelper: false, now: later).sessions[0].statusSince == now)
        #expect(mapper.map(snapshot(.done), noHelper: false, now: later).sessions[0].statusSince == later)

        var fresh = SnapshotMapper()
        let first = fresh.map(snapshot(.done), noHelper: true, now: now)
        #expect(first.sessions[0].statusSince == now.addingTimeInterval(-9_000))
        #expect(first.helper == .missing)
    }

    @Test func foregroundPropagates() async throws {
        let watches = Watches()
        let services = services(watches: watches)
        await services.start()
        try await services.saveHost(record(Samples.studio, "Studio"))
        try await services.saveHost(record(Samples.build, "Build box", noHelper: true))
        let both = watches.all
        #expect(both.count == 2)
        await eventually { both.allSatisfy { $0.foregrounds.withLock { $0 } == [true] } }

        services.setForeground(false)
        await eventually { both.allSatisfy { $0.foregrounds.withLock { $0 }.last == false } }
        #expect(both.allSatisfy { $0.foregrounds.withLock { $0 } == [true, false] })

        services.setForeground(true)
        await eventually { both.allSatisfy { $0.foregrounds.withLock { $0 }.last == true } }
        #expect(both.allSatisfy { $0.foregrounds.withLock { $0 } == [true, false, true] })
    }

    @Test func removalStopsTheMonitorAndForgetsTheMachine() async throws {
        let watches = Watches()
        let helpers = NoHelper()
        let services = services(watches: watches, helpers: helpers)
        var removed: [HostID] = []
        services.onMachineRemoved = { removed.append($0) }
        await services.start()
        try await services.saveHost(record(Samples.studio, "Studio"))
        let watch = try #require(watches[Samples.studio])

        try await services.removeHost(Samples.studio)
        #expect(services.hosts.isEmpty)
        #expect(removed == [Samples.studio])
        await eventually { watch.stopped.withLock { $0 } && helpers.dropped.withLock { $0 } == [Samples.studio] }
        #expect(watch.stopped.withLock { $0 })
        #expect(helpers.dropped.withLock { $0 } == [Samples.studio])
        await #expect(throws: LiveServiceError.self) {
            try await services.kill(hostID: Samples.studio, sessionID: "s1")
        }
    }

    @Test func editReconnectsWithTheNewProfile() async throws {
        let watches = Watches()
        let helpers = NoHelper()
        let services = services(watches: watches, helpers: helpers)
        var changed: [HostID] = []
        services.onMachineChanged = { changed.append($0) }
        await services.start()
        try await services.saveHost(record(Samples.studio, "Studio"))
        let old = try #require(watches[Samples.studio])

        var edited = record(Samples.studio, "Studio")
        edited.profile.endpoint = HostEndpoint(host: "studio.tail1234.ts.net", port: 2222, user: "sam")
        try await services.saveHost(edited)
        let new = try #require(watches[Samples.studio])
        #expect(new !== old)
        #expect(new.record.profile.endpoint.host == "studio.tail1234.ts.net")
        #expect(changed == [Samples.studio])
        #expect(services.hostRecord(Samples.studio) == edited)
        await eventually { old.stopped.withLock { $0 } && !helpers.dropped.withLock { $0 }.isEmpty }
        #expect(old.stopped.withLock { $0 })
        #expect(helpers.dropped.withLock { $0 } == [Samples.studio])
        #expect(watches.history.withLock { $0.count } == 2)
    }

    @Test func signInAsksThenRefreshesThatMachinesMonitor() async throws {
        let watches = Watches()
        let services = services(watches: watches)
        await services.start()
        try await services.saveHost(record(Samples.studio, "Studio"))
        try await services.saveHost(record(Samples.build, "Build box"))
        let prompter = CountingPrompter()
        try await services.signIn(hostID: Samples.studio, prompter: prompter)
        #expect(prompter.asked.withLock { $0 } == 1)
        #expect(watches[Samples.studio]?.refreshes.withLock { $0 } == 1)
        #expect(watches[Samples.build]?.refreshes.withLock { $0 } == 0)
    }
}

@MainActor extension LiveServicesTests {
    // MARK: Lock-screen answers

    private func approval(_ id: String) -> Approval {
        Approval(
            requestId: id, sessionId: "s7", bib: nil, agent: "claude", source: .hook, title: "Run npm test?",
            detail: "npm test", snippet: "", options: [ApprovalOption(n: 1, label: "Yes", role: .approve)],
            allowText: false, createdAt: now, expiresAt: nil
        )
    }

    private func pairing(_ machine: UUID?) -> Pairing {
        Pairing(
            hostId: "h_studio", hostKey: "", relayURL: URL(string: "https://relay.example.test")!, machineName: "Studio",
            appHostID: machine, pairedAt: now, tokenHash: "000000", liveActivityCommand: false
        )
    }

    @Test func answersFromTheLockScreenOverTheLiveChannel() async throws {
        let watches = Watches()
        let services = services(watches: watches)
        await services.start()
        try await services.saveHost(record(Samples.studio, "Studio"))
        let watch = try #require(watches[Samples.studio])
        #expect(!services.hasLiveChannel(pairing(Samples.studio)))

        watch.sink.yield(HostSnapshot(hostID: Samples.studio, reach: .online, sessions: []))
        await eventually { services.hasLiveChannel(pairing(Samples.studio)) }
        #expect(services.hasLiveChannel(pairing(Samples.studio)))
        #expect(!services.hasLiveChannel(pairing(nil)))

        try await services.answer(pairing(Samples.studio), requestId: "r1", decision: .approve)
        #expect(watch.ops.withLock { $0 } == ["answer r1"])
    }

    @Test func reportsOpenRequestsOnceAfterComingForward() async throws {
        let watches = Watches()
        var reports: [Set<String>] = []
        let services = services(watches: watches) { _, open in reports.append(open) }
        await services.start()
        try await services.saveHost(record(Samples.studio, "Studio"))
        let watch = try #require(watches[Samples.studio])

        watch.sink.yield(HostSnapshot(hostID: Samples.studio, reach: .online, sessions: [], approvals: [approval("r1")]))
        await eventually { reports.count == 1 }
        services.setForeground(false)
        services.setForeground(true)
        watch.sink.yield(HostSnapshot(hostID: Samples.studio, reach: .online, sessions: [], approvals: []))
        await eventually { reports.count == 2 }
        watch.sink.yield(HostSnapshot(hostID: Samples.studio, reach: .online, sessions: [], approvals: [approval("r2")]))
        try await Task.sleep(for: .milliseconds(100))

        #expect(reports == [["r1"], []])
    }
}
