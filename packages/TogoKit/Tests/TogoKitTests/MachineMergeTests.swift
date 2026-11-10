import Foundation
import Testing
@testable import TogoKit

struct MachineMergeTests {
    private static let macKey = "SHA256:mac-host-key"
    private static let otherKey = "SHA256:other-host-key"

    private func record(
        _ name: String, host: String, user: String = "sam", key: String = "key-1", hostKeys: [String]? = nil,
        helper: String? = nil, routes: [String] = [], id: UUID = UUID(), addedAt: Date? = nil
    ) -> HostRecord {
        HostRecord(
            id: id,
            profile: HostProfile(
                id: id, name: name, endpoint: HostEndpoint(host: host, user: user), auth: .key(id: key),
                routes: [.manual(host)] + routes.map { HostRoute(host: $0, kind: .lan) }
            ),
            symbol: "laptopcomputer", hostKeys: hostKeys, helperHostID: helper, addedAt: addedAt
        )
    }

    private func makeStore(remote: MemoryBackend? = nil) -> (HostStore, SyncedRecordStore<HostRecord>) {
        let records = SyncedRecordStore<HostRecord>(
            prefix: "host.", local: MemoryBackend(), remote: remote, deviceID: "phone",
            clock: { Date(timeIntervalSince1970: 1_800_000_000) }, debounce: .seconds(60)
        )
        return (HostStore(records: records), records)
    }

    private static let byRecord: @Sendable (HostRecord) -> MachineIdentity = { MachineIdentity($0) }

    // MARK: Update

    /// Setting a synced machine up on this device saves under its own id: one machine, updated.
    @Test func savingUnderTheSameIDUpdatesTheMachine() async {
        let (store, _) = makeStore()
        let synced = record("Studio", host: "studio.local", key: "ipad-key", hostKeys: [Self.macKey])
        await store.save(synced)
        var setUp = synced
        setUp.profile.routes.append(HostRoute(host: "192.168.1.20", kind: .lan))
        setUp.helperHostID = "helper-1"

        let outcome = await store.save(setUp, identity: Self.byRecord)

        #expect(outcome == .saved(setUp))
        let hosts = await store.hosts()
        #expect(hosts.map(\.id) == [synced.id])
        #expect(hosts.first?.helperHostID == "helper-1")
    }

    /// The record set up on this device is the synced one under the same id: it gains the
    /// addresses and helper found here and keeps its name, address and sign-in.
    @Test func settingUpHereKeepsTheSyncedRecord() {
        let id = UUID()
        let synced = record("Studio", host: "studio.local", key: "ipad-key", id: id)
        var setUp = record("Sam's Mac", host: "192.168.1.20", key: "phone-key", id: id)
        setUp.helperPath = "/usr/local/bin/togo"
        setUp.noHelper = false

        let updated = MachineMerge.merged(synced, absorbing: setUp)

        #expect(updated.id == id)
        #expect(updated.profile.name == "Studio")
        #expect(updated.profile.endpoint.host == "studio.local")
        #expect(updated.profile.auth == .key(id: "ipad-key"))
        #expect(updated.profile.routes.map(\.host) == ["studio.local", "192.168.1.20"])
        #expect(updated.helperPath == "/usr/local/bin/togo")
        #expect(updated.mergedIDs == nil, "nothing was folded in: it is the same record")
    }

    // MARK: Dedupe on save

    @Test func addingAMachineWithAKnownHostKeyMergesIntoIt() async {
        let (store, _) = makeStore()
        let synced = record("Studio", host: "studio.local", key: "ipad-key", hostKeys: [Self.macKey], routes: ["10.0.0.4"])
        await store.save(synced)
        let added = record("Sam's Mac", host: "192.168.1.20", key: "phone-key", hostKeys: [Self.macKey], helper: "helper-1")

        let outcome = await store.save(added, identity: Self.byRecord)

        guard case .merged(let into) = outcome else {
            Issue.record("expected a merge, got \(outcome)")
            return
        }
        #expect(into.id == synced.id)
        #expect(into.profile.name == "Studio")
        #expect(into.profile.auth == .key(id: "ipad-key"))
        #expect(into.profile.routes.map(\.host) == ["studio.local", "10.0.0.4", "192.168.1.20"])
        #expect(into.helperHostID == "helper-1")
        #expect(into.mergedIDs == [added.id])
        #expect(await store.hosts() == [into])
    }

    @Test func addingAMachineWithAKnownHelperMergesIntoIt() async {
        let (store, _) = makeStore()
        let synced = record("Studio", host: "studio.local", helper: "helper-1")
        await store.save(synced)
        let added = record("Studio", host: "studio.tail0.ts.net", helper: "helper-1")

        let outcome = await store.save(added, identity: Self.byRecord)

        #expect(outcome == .merged(into: MachineMerge.merged(synced, absorbing: added)))
        #expect(await store.hosts().map(\.id) == [synced.id])
    }

    /// What this device trusts for an address counts as well as what the record carries.
    @Test func trustedHostKeysOnThisDeviceIdentifyARecord() async {
        let (store, _) = makeStore()
        let synced = record("Studio", host: "studio.local")
        await store.save(synced)
        let added = record("Studio", host: "192.168.1.20", hostKeys: [Self.macKey])
        let trusted = ["studio.local": [Self.macKey]]

        let outcome = await store.save(added) { MachineIdentity($0, trusted: trusted[$0.profile.endpoint.host] ?? []) }

        #expect(await store.hosts().count == 1)
        guard case .merged(let into) = outcome else {
            Issue.record("expected a merge")
            return
        }
        #expect(into.id == synced.id)
    }

    // MARK: No merge

    /// Two machines with the same name and different host keys stay two machines.
    @Test func sameNameDifferentHostsNeverMerge() async {
        let (store, _) = makeStore()
        let first = record("MacBook Pro", host: "192.168.1.20", hostKeys: [Self.macKey])
        await store.save(first)
        let second = record("MacBook Pro", host: "192.168.1.20", hostKeys: [Self.otherKey])

        #expect(await store.save(second, identity: Self.byRecord) == .saved(second))
        #expect(await store.mergeDuplicates(identity: Self.byRecord).isEmpty)
        #expect(Set(await store.hosts().map(\.id)) == [first.id, second.id])
    }

    @Test func recordsWithoutAnIdentityNeverMerge() {
        let first = record("Studio", host: "studio.local")
        let second = record("Studio", host: "studio.local")
        #expect(MachineMerge.duplicates(in: [first, second], identity: Self.byRecord, older: MachineMerge.addedFirst).isEmpty)
    }

    /// One host key serves every account on a machine; another account is another record.
    @Test func sameHostKeyForAnotherAccountDoesNotMerge() {
        let sam = record("Studio", host: "studio.local", user: "sam", hostKeys: [Self.macKey])
        let root = record("Studio admin", host: "studio.local", user: "root", hostKeys: [Self.macKey])
        #expect(!MachineIdentity(sam).isSameMachine(as: MachineIdentity(root)))
    }

    /// Two helpers are two machines even behind one host key.
    @Test func differentHelpersDoNotMerge() {
        let first = record("Studio", host: "studio.local", hostKeys: [Self.macKey], helper: "helper-1")
        let second = record("Studio", host: "studio.local", hostKeys: [Self.macKey], helper: "helper-2")
        #expect(MachineMerge.duplicates(in: [first, second], identity: Self.byRecord, older: MachineMerge.addedFirst).isEmpty)
    }

    // MARK: Cleanup

    @Test func cleanupKeepsTheOlderRecordAndDropsTheNewerEverywhere() async throws {
        let remote = MemoryBackend()
        let (store, records) = makeStore(remote: remote)
        await records.setSyncEnabled(true)
        let older = record(
            "Studio", host: "studio.local", key: "ipad-key", hostKeys: [Self.macKey], routes: ["10.0.0.4"],
            addedAt: Date(timeIntervalSince1970: 1_700_000_000)
        )
        let newer = record(
            "Studio", host: "192.168.1.20", key: "phone-key", helper: "helper-1", routes: ["studio.tail0.ts.net"],
            addedAt: Date(timeIntervalSince1970: 1_800_000_000)
        )
        await store.save(newer)
        await store.save(older)
        let trusted = ["192.168.1.20": [Self.macKey]]

        let merges = await store.mergeDuplicates { MachineIdentity($0, trusted: trusted[$0.profile.endpoint.host] ?? []) }

        #expect(merges.map(\.dropped) == [[newer.id]])
        let hosts = await store.hosts()
        #expect(hosts.map(\.id) == [older.id])
        let kept = try #require(hosts.first)
        #expect(kept.profile.auth == .key(id: "ipad-key"))
        #expect(Set(kept.profile.routes.map(\.host)) == ["studio.local", "10.0.0.4", "192.168.1.20", "studio.tail0.ts.net"])
        #expect(kept.helperHostID == "helper-1")
        #expect(kept.mergedIDs == [newer.id])
        // The newer record leaves through sync as a deletion, uploaded with the merged one.
        await records.flush()
        let tombstone = try #require(remote.data(forKey: "host.\(newer.id.uuidString)"))
        #expect(try JSONDecoder().decode(Stamped<HostRecord>.self, from: tombstone).value == nil)
        let uploaded = try #require(remote.data(forKey: "host.\(older.id.uuidString)"))
        #expect(try JSONDecoder().decode(Stamped<HostRecord>.self, from: uploaded).value == kept)
    }

    /// Records from before `addedAt` count as the oldest, so the one synced in stays.
    @Test func cleanupKeepsALegacyRecordOverANewOne() {
        let legacy = record("Studio", host: "studio.local", hostKeys: [Self.macKey])
        let added = record("Studio", host: "192.168.1.20", hostKeys: [Self.macKey], addedAt: Date())
        let found = MachineMerge.duplicates(in: [added, legacy], identity: Self.byRecord, older: MachineMerge.addedFirst)
        #expect(found.map(\.kept.id) == [legacy.id])
        #expect(found.map(\.dropped) == [[added.id]])
    }

    @Test func cleanupFoldsThreeRecordsIntoOne() {
        let a = record("Studio", host: "a.local", hostKeys: [Self.macKey], addedAt: Date(timeIntervalSince1970: 1))
        let b = record("Studio", host: "b.local", hostKeys: [Self.macKey], helper: "helper-1", addedAt: Date(timeIntervalSince1970: 2))
        let c = record("Studio", host: "c.local", helper: "helper-1", addedAt: Date(timeIntervalSince1970: 3))
        let found = MachineMerge.duplicates(in: [c, b, a], identity: Self.byRecord, older: MachineMerge.addedFirst)
        #expect(found.count == 1)
        #expect(found.first?.kept.id == a.id)
        #expect(found.first?.dropped == [b.id, c.id])
        #expect(found.first?.kept.mergedIDs == [b.id, c.id])
    }

    /// Addresses on another port would be tried on the kept record's port, so they stay out.
    @Test func mergeLeavesAnotherPortsAddressesOut() {
        let kept = record("Studio", host: "studio.local", hostKeys: [Self.macKey])
        var other = record("Studio", host: "gateway.example.com", hostKeys: [Self.macKey])
        other.profile.endpoint.port = 2222
        #expect(MachineMerge.merged(kept, absorbing: other).profile.routes == kept.profile.routes)
    }
}
