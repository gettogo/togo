import Foundation
import Testing
import TogoKit
@testable import Togo

/// One record per machine: setting a synced machine up here updates it, adding it again merges
/// into it, and a launch folds records that are one machine. Never by name alone.
@MainActor struct DuplicateMachineTests {
    private let now = Samples.now
    private static let macKey = "SHA256:mac-host-key"
    private let synced = HostID()
    private let added = HostID()

    private func record(
        _ id: HostID, _ name: String, host: String, key: String, addedAt: Date? = nil
    ) -> HostRecord {
        HostRecord(
            id: id,
            profile: HostProfile(id: id, name: name, endpoint: HostEndpoint(host: host, port: 22, user: "sam"), auth: .key(id: key)),
            symbol: "laptop", addedAt: addedAt
        )
    }

    /// `trusted` is what this device trusts per address, as the known hosts store would say.
    private func services(
        watches: Watches = Watches(), local: MemoryBackend = MemoryBackend(), trusted: [String: [String]] = [:]
    ) -> LiveServices {
        let now = self.now
        let syncSwitch = SyncSwitch(backend: MemoryBackend())
        return LiveServices(
            hostRecords: SyncedRecordStore(prefix: "host.", local: local, remote: nil, deviceID: "phone", clock: { now }),
            settingsRecords: SyncedRecordStore(prefix: "settings.", local: MemoryBackend(), remote: nil, deviceID: "phone", clock: { now }),
            syncSwitch: syncSwitch,
            deviceMachines: DeviceMachines(
                backend: MemoryBackend(), hasKey: { $0 == "phone-key" }, deviceKeyID: { "phone-key" }
            ),
            dependencies: LiveServices.Dependencies(
                watch: { record in
                    let watch = FakeWatch(record: record)
                    watches.made.withLock { $0[record.id] = watch }
                    return watch
                },
                helpers: NoHelper(),
                connect: { _, _, _, _ in { _, _, _ in throw NoHelper.Unreachable() } },
                now: { now },
                trustedHostKeys: { trusted[$0.host] ?? [] }
            )
        )
    }

    @Test func settingUpASyncedMachineUpdatesItsRecord() async throws {
        let services = services()
        await services.start()
        try await services.saveHost(record(synced, "Studio", host: "studio.local", key: "ipad-key"))
        var setUp = record(synced, "Studio", host: "192.168.1.20", key: "phone-key")
        setUp.profile.routes = [.manual("192.168.1.20")]

        try await services.setUpHere(setUp)

        let stored = await services.hostStore.hosts()
        #expect(stored.map(\.id) == [synced])
        let kept = try #require(stored.first)
        #expect(kept.profile.auth == .key(id: "ipad-key"), "the other devices keep their key")
        #expect(kept.profile.endpoint.host == "studio.local")
        #expect(kept.profile.routes.map(\.host) == ["studio.local", "192.168.1.20"])
        #expect(services.hostRecord(synced)?.profile.auth == .key(id: "phone-key"), "this device signs in with its own")
    }

    @Test func addingAMachineAlreadyHereMergesIntoIt() async throws {
        let trusted = ["studio.local": [Self.macKey], "192.168.1.20": [Self.macKey]]
        let services = services(trusted: trusted)
        await services.start()
        try await services.saveHost(record(synced, "Studio", host: "studio.local", key: "ipad-key"))

        let outcome = try await services.addHost(record(added, "Sam's MacBook Pro", host: "192.168.1.20", key: "phone-key"))

        guard case .merged(let kept) = outcome else {
            Issue.record("expected a merge, got \(outcome)")
            return
        }
        #expect(kept.id == synced)
        #expect(services.hosts.map(\.id) == [synced])
        #expect(services.hostRecord(synced)?.profile.auth == .key(id: "phone-key"))
        #expect(AppStore.alreadyAddedNotice(kept.profile.name) == "Studio is already in your machines")
    }

    @Test func launchFoldsDuplicatesIntoTheOlderRecord() async throws {
        let local = MemoryBackend()
        let earlier = services(local: local)
        await earlier.start()
        try await earlier.saveHost(record(synced, "Studio", host: "studio.local", key: "ipad-key"))
        try await earlier.saveHost(record(added, "Studio", host: "192.168.1.20", key: "phone-key", addedAt: now))
        #expect(await earlier.hostStore.hosts().count == 2)

        let watches = Watches()
        let trusted = ["studio.local": [Self.macKey], "192.168.1.20": [Self.macKey]]
        let services = services(watches: watches, local: local, trusted: trusted)
        await services.start()

        let stored = await services.hostStore.hosts()
        #expect(stored.map(\.id) == [synced])
        #expect(Set(stored.first?.profile.routes.map(\.host) ?? []) == ["studio.local", "192.168.1.20"])
        #expect(stored.first?.mergedIDs == [added])
        #expect(watches[added] == nil, "the dropped record never connects")
        #expect(services.hostRecord(synced)?.profile.auth == .key(id: "phone-key"), "this device's key choice moves over")
    }

    /// Another device merged a record into one this device has, and its deletion arrived before
    /// the merged record: the machine is still here, so what this device keeps for it moves over
    /// instead of being forgotten as for a removed machine.
    @Test func aMergeDeletionArrivingFirstIsNotARemoval() async throws {
        let local = MemoryBackend()
        let services = services(local: local)
        await services.start()
        try await services.saveHost(record(synced, "Studio", host: "studio.local", key: "ipad-key"))
        try await services.saveHost(record(added, "Studio", host: "192.168.1.20", key: "ipad-key"))
        services.deviceMachines.setUp(added, keyID: "phone-key")
        #expect(services.hosts.count == 2)

        let deletion = Stamped<HostRecord>(
            value: nil, updatedAt: now.addingTimeInterval(1), deviceID: "pad", mergedInto: synced.uuidString
        )
        local.set(try JSONEncoder().encode(deletion), forKey: "host.\(added.uuidString)")
        services.reconcile(await services.hostStore.hosts())

        #expect(services.hosts.map(\.id) == [synced])
        #expect(!services.deviceMachines.needsSetUp(synced, reach: .authNeeded), "this device's set-up moved to the kept record")
    }

    @Test func twoMachinesWithTheSameNameStayTwo() async throws {
        let trusted = ["192.168.1.20": [Self.macKey], "192.168.1.30": ["SHA256:another-mac"]]
        let services = services(trusted: trusted)
        await services.start()
        try await services.saveHost(record(synced, "MacBook Pro", host: "192.168.1.20", key: "phone-key"))

        let outcome = try await services.addHost(record(added, "MacBook Pro", host: "192.168.1.30", key: "phone-key"))

        guard case .saved = outcome else {
            Issue.record("expected a new machine, got \(outcome)")
            return
        }
        #expect(Set(services.hosts.map(\.id)) == [synced, added])
    }
}
