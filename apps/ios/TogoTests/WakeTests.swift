import Foundation
import Synchronization
import Testing
import TogoKit
@testable import Togo

@MainActor struct WakeTests {
    private let now = Samples.now
    private let id = Samples.studio

    private static let info = HostInfo(
        interfaces: [HostInterface(name: "en0", mac: "A4:83:E7:00:11:22", ipv4: ["192.168.1.20"])], wakeOnLan: true
    )
    private static let target = WakeTarget(
        mac: "a4:83:e7:00:11:22", broadcast: ["192.168.1.255", "255.255.255.255"], enabled: true
    )

    private func record(wake: WakeTarget? = nil) -> HostRecord {
        HostRecord(
            id: id,
            profile: HostProfile(id: id, name: "Studio", endpoint: HostEndpoint(host: "studio.local", port: 22, user: "sam"), auth: .password),
            symbol: "desktop", wake: wake
        )
    }

    private func snapshot(
        info: HostInfo? = WakeTests.info, capabilities: Set<String> = [Capability.events, Capability.hostInfo]
    ) -> HostSnapshot {
        var snapshot = HostSnapshot(
            hostID: id, reach: .online, helper: .present(version: "0.3.0"), lastSeen: now, capabilities: capabilities
        )
        snapshot.hostInfo = info
        return snapshot
    }

    private func machine(_ reachability: Reachability, wake: WakeTarget?) -> MachineSection {
        MachineSection(
            id: id, name: "Studio", symbol: .desktop, reachability: reachability, helper: .present(version: "0.3.0", live: true),
            sessions: [], lastSeen: now, wake: wake
        )
    }

    private func services(watches: Watches, wakeEnabled: Bool = true) -> LiveServices {
        let now = self.now
        let sync = SyncSwitch(backend: MemoryBackend())
        sync.set(true)
        return LiveServices(
            hostRecords: SyncedRecordStore(prefix: "host.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { now }, syncEnabled: true),
            settingsRecords: SyncedRecordStore(prefix: "settings.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { now }, syncEnabled: true),
            syncSwitch: sync,
            dependencies: LiveServices.Dependencies(
                watch: { record in
                    let watch = FakeWatch(record: record)
                    watches.made.withLock { $0[record.id] = watch }
                    watches.history.withLock { $0.append(watch) }
                    return watch
                },
                helpers: NoHelper(),
                connect: { _, _, _, _ in { _, _, _ in throw NoHelper.Unreachable() } },
                signIn: { _, _ in },
                forgetPassword: { _ in },
                now: { now },
                reported: { _, _ in },
                wakeEnabled: wakeEnabled
            )
        )
    }

    private func eventually(_ condition: () -> Bool) async {
        for _ in 0..<200 where !condition() {
            try? await Task.sleep(for: .milliseconds(5))
        }
    }

    // MARK: Storing the target

    @Test func storesTargetFromHostInfo() async throws {
        let updated = try #require(WakeLearning.updated(record(), from: snapshot()))
        #expect(updated.wake == Self.target)
        #expect(WakeLearning.updated(updated, from: snapshot()) == nil)
    }

    @Test func nothingStoredWithoutTheCapabilityOrAUsableInterface() {
        #expect(WakeLearning.updated(record(), from: snapshot(capabilities: [Capability.events])) == nil)
        #expect(WakeLearning.updated(record(), from: snapshot(info: nil)) == nil)
        #expect(WakeLearning.updated(record(), from: snapshot(info: HostInfo(interfaces: []))) == nil)
        let stored = record(wake: Self.target)
        #expect(WakeLearning.updated(stored, from: snapshot(info: HostInfo(interfaces: []))) == nil)
    }

    @Test func aChangedAddressReplacesTheStoredTarget() throws {
        let moved = HostInfo(
            interfaces: [HostInterface(name: "en0", mac: "a4:83:e7:00:11:22", ipv4: ["10.0.0.5"])], wakeOnLan: false
        )
        let updated = try #require(WakeLearning.updated(record(wake: Self.target), from: snapshot(info: moved)))
        #expect(updated.wake == WakeTarget(mac: "a4:83:e7:00:11:22", broadcast: ["10.0.0.255", "255.255.255.255"], enabled: false))
    }

    @Test func liveServicesSavesTheTargetWithoutRestartingTheMonitor() async throws {
        let watches = Watches()
        let services = services(watches: watches)
        await services.start()
        try await services.saveHost(record())
        let watch = try #require(watches[id])
        watch.sink.yield(snapshot())
        await eventually { services.hostRecord(id)?.wake == Self.target }
        #expect(services.hostRecord(id)?.wake == Self.target)
        #expect(watches.history.withLock { $0.count } == 1)
        #expect(!watch.stopped.withLock { $0 })
    }

    /// While the app cannot send wake packets, no machine's hardware address is learned or synced.
    @Test func nothingLearnedWhileWakeIsOff() async throws {
        let watches = Watches()
        let services = services(watches: watches, wakeEnabled: false)
        await services.start()
        try await services.saveHost(record())
        let watch = try #require(watches[id])
        watch.sink.yield(snapshot())
        await eventually { services.latest[id] != nil }
        for _ in 0..<5 { await Task.yield() }
        #expect(services.hostRecord(id)?.wake == nil)
    }

    @Test func learningKeepsAConcurrentEdit() async throws {
        let watches = Watches()
        let services = services(watches: watches)
        await services.start()
        try await services.saveHost(record())
        var renamed = record()
        renamed.profile.name = "Renamed"
        try await services.saveHost(renamed)
        await services.learnWake(id, Self.target)
        let stored = try #require(services.hostRecord(id))
        #expect(stored.profile.name == "Renamed")
        #expect(stored.wake == Self.target)
    }

    @Test func aMachineThatComesBackClearsTheWakeNote() {
        let store = store(wake: Self.target) { _ in }
        store.wakeNotes[id] = WakeCopy.sent("Studio")
        store.apply(HostViewSnapshot(
            hostID: id, reachability: .online, helper: .present(version: "0.3.0", live: true), sessions: [], requests: [], at: now
        ))
        #expect(store.wakeNotes[id] == nil)
    }

    // MARK: The button

    @Test func buttonOnlyOfflineWithTarget() {
        let offline = Reachability.offline(since: now, reason: "Asleep")
        #expect(WakeCopy.showsButton(for: machine(offline, wake: Self.target), enabled: true))
        #expect(!WakeCopy.showsButton(for: machine(offline, wake: nil), enabled: true))
        #expect(!WakeCopy.showsButton(for: machine(offline, wake: Self.target), enabled: false))
        for other: Reachability in [.online, .connecting, .waking, .authNeeded, .keyMissing, .failed("x")] {
            #expect(!WakeCopy.showsButton(for: machine(other, wake: Self.target), enabled: true))
        }
    }

    @Test func buildFlagIsOffByDefault() {
        #expect(!WakeFeature.isEnabled)
    }

    // MARK: Sending

    private func store(wake: WakeTarget?, send: @escaping @Sendable (WakeTarget) async throws -> Void) -> AppStore {
        let state = ControlCenterState(machines: [machine(.offline(since: now, reason: "Asleep"), wake: wake)], loaded: true)
        let store = AppStore(services: FakeAppServices(hosts: Samples.hosts), controlCenter: state)
        store.sendWake = send
        return store
    }

    @Test func sendingSaysWhereItWorks() async {
        let sent = Mutex<[WakeTarget]>([])
        let store = store(wake: Self.target) { target in sent.withLock { $0.append(target) } }
        await store.wake(id)
        #expect(sent.withLock { $0 } == [Self.target])
        #expect(store.wakeNotes[id] == "Sent a wake signal. This works on the same network as Studio.")
    }

    @Test func offOnTheMachineSendsNothing() async {
        let sent = Mutex(0)
        var off = Self.target
        off.enabled = false
        let store = store(wake: off) { _ in sent.withLock { $0 += 1 } }
        await store.wake(id)
        #expect(sent.withLock { $0 } == 0)
        #expect(store.wakeNotes[id] == "Wake for network access is off on Studio.")
    }

    @Test func aFailedSendIsSaidInWords() async {
        let store = store(wake: Self.target) { _ in throw WakeError.failed }
        await store.wake(id)
        #expect(store.wakeNotes[id]?.hasPrefix("Couldn't send a wake signal to Studio.") == true)
    }

    @Test func noTargetSendsNothing() async {
        let store = store(wake: nil) { _ in Issue.record("sent") }
        await store.wake(id)
        #expect(store.wakeNotes[id] == nil)
    }
}
