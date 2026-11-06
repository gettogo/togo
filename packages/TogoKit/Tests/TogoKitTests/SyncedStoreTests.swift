import Foundation
import Testing
@testable import TogoKit

private final class StoreClock: @unchecked Sendable {
    private let lock = NSLock()
    private var now: Date
    init(_ start: Date = Date(timeIntervalSince1970: 1_800_000_000)) { now = start }
    func advance(_ seconds: TimeInterval) { lock.withLock { now = now.addingTimeInterval(seconds) } }
    func read() -> Date { lock.withLock { now } }
}

private final class CountingBackend: KeyValueBackend, @unchecked Sendable {
    private let inner = MemoryBackend()
    private let lock = NSLock()
    private var writes = 0
    var writeCount: Int { lock.withLock { writes } }
    var externalChanges: AsyncStream<[String]> { inner.externalChanges }
    func data(forKey key: String) -> Data? { inner.data(forKey: key) }
    func allKeys() -> [String] { inner.allKeys() }
    func set(_ data: Data?, forKey key: String) {
        lock.withLock { writes += 1 }
        inner.set(data, forKey: key)
    }
}

private func record(_ name: String, auth: AuthMethodRef = .key(id: "key-1"), id: UUID = UUID()) -> HostRecord {
    HostRecord(
        id: id,
        profile: HostProfile(id: id, name: name, endpoint: HostEndpoint(host: "\(name).local", user: "dev"), auth: auth),
        symbol: "desktopcomputer"
    )
}

private func makeStore(
    local: MemoryBackend = MemoryBackend(),
    remote: MemoryBackend?,
    device: String,
    clock: StoreClock
) -> SyncedRecordStore<HostRecord> {
    SyncedRecordStore(
        prefix: "host.",
        local: local,
        remote: remote,
        deviceID: device,
        clock: { clock.read() },
        debounce: .seconds(60)
    )
}

private func stamp(_ record: HostRecord?, at date: Date, device: String) throws -> Data {
    try JSONEncoder().encode(Stamped(value: record, updatedAt: date, deviceID: device))
}

private func eventually(_ condition: @Sendable () async -> Bool) async -> Bool {
    for _ in 0..<200 {
        if await condition() { return true }
        try? await Task.sleep(for: .milliseconds(10))
    }
    return false
}

@Suite struct SyncedStoreTests {
    @Test func mergeKeepsNewerPerRecord() async throws {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let store = makeStore(remote: remote, device: "a", clock: clock)
        let id = UUID()
        await store.put(id.uuidString, record("old", id: id))

        let newer = record("new", id: id)
        remote.simulateRemote(["host.\(id.uuidString)": try stamp(newer, at: clock.read().addingTimeInterval(10), device: "b")])
        #expect(await eventually { await store.all()[id.uuidString] == newer })

        let stale = record("stale", id: id)
        remote.simulateRemote(["host.\(id.uuidString)": try stamp(stale, at: clock.read().addingTimeInterval(-10), device: "b")])
        try await Task.sleep(for: .milliseconds(100))
        #expect(await store.all()[id.uuidString] == newer)
    }

    @Test func equalTimestampsResolveByDeviceID() async throws {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let store = makeStore(remote: remote, device: "a", clock: clock)
        let id = UUID()
        await store.put(id.uuidString, record("mine", id: id))

        let theirs = record("theirs", id: id)
        remote.simulateRemote(["host.\(id.uuidString)": try stamp(theirs, at: clock.read(), device: "b")])
        #expect(await eventually { await store.all()[id.uuidString] == theirs })
    }

    @Test func tombstoneBeatsOlderEdit() async throws {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let store = makeStore(remote: remote, device: "a", clock: clock)
        let id = UUID()
        await store.put(id.uuidString, record("edited", id: id))

        remote.simulateRemote(["host.\(id.uuidString)": try stamp(nil, at: clock.read().addingTimeInterval(5), device: "b")])
        #expect(await eventually { await store.all().isEmpty })

        remote.simulateRemote(["host.\(id.uuidString)": try stamp(record("late", id: id), at: clock.read().addingTimeInterval(1), device: "b")])
        try await Task.sleep(for: .milliseconds(100))
        #expect(await store.all().isEmpty)
    }

    @Test func editAfterTombstoneRevives() async {
        let clock = StoreClock()
        let store = makeStore(remote: MemoryBackend(), device: "a", clock: clock)
        let id = UUID()
        await store.put(id.uuidString, record("one", id: id))
        clock.advance(1)
        await store.delete(id.uuidString)
        #expect(await store.all().isEmpty)

        clock.advance(1)
        let revived = record("two", id: id)
        await store.put(id.uuidString, revived)
        #expect(await store.all()[id.uuidString] == revived)
    }

    @Test func tombstonesExpireAfterThirtyDays() async throws {
        let clock = StoreClock()
        let local = MemoryBackend()
        let store = makeStore(local: local, remote: nil, device: "a", clock: clock)
        let id = UUID()
        await store.put(id.uuidString, record("gone", id: id))
        await store.delete(id.uuidString)
        #expect(local.data(forKey: "host.\(id.uuidString)") != nil)

        clock.advance(31 * 24 * 60 * 60)
        _ = await store.all()
        #expect(local.data(forKey: "host.\(id.uuidString)") == nil)
    }

    @Test func remoteChangeForOneRecordLeavesOthers() async throws {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let store = makeStore(remote: remote, device: "a", clock: clock)
        let kept = record("kept")
        let edited = record("edited")
        await store.put(kept.id.uuidString, kept)
        await store.put(edited.id.uuidString, edited)

        let renamed = {
            var copy = edited
            copy.symbol = "server.rack"
            return copy
        }()
        remote.simulateRemote(["host.\(edited.id.uuidString)": try stamp(renamed, at: clock.read().addingTimeInterval(3), device: "b")])

        #expect(await eventually { await store.all()[edited.id.uuidString] == renamed })
        #expect(await store.all()[kept.id.uuidString] == kept)
    }

    @Test func keysNeverInSyncedPayload() async throws {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let store = makeStore(remote: remote, device: "a", clock: clock)
        let secrets = MemorySecretStore()
        let privateKey = Data("-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXk=\n".utf8)
        let password = Data("hunter2-secret".utf8)
        try secrets.set(privateKey, for: "key-1", requireUserPresence: false)
        try secrets.set(password, for: "pass", requireUserPresence: false)

        let withKey = record("keyed", auth: .key(id: "key-1"))
        let withPassword = record("pass", auth: .password)
        await store.put(withKey.id.uuidString, withKey)
        await store.put(withPassword.id.uuidString, withPassword)
        await store.flush()

        let keys = remote.allKeys()
        #expect(keys.count == 2)
        for key in keys {
            let payload = try #require(remote.data(forKey: key))
            let text = String(decoding: payload, as: UTF8.self)
            #expect(!text.contains("BEGIN"))
            #expect(payload.range(of: privateKey) == nil)
            #expect(payload.range(of: password) == nil)
            #expect(payload.range(of: Data("b3BlbnNzaC1rZXk=".utf8)) == nil)
        }
    }

    @Test func burstOfPutsUploadsOnceWithTheLastValue() async throws {
        let clock = StoreClock()
        let remote = CountingBackend()
        let store = SyncedRecordStore<HostRecord>(
            prefix: "host.", local: MemoryBackend(), remote: remote, deviceID: "a",
            clock: { clock.read() }, debounce: .milliseconds(50)
        )
        let id = UUID()
        await store.put(id.uuidString, record("first", id: id))
        let second = record("second", id: id)
        await store.put(id.uuidString, second)

        let key = "host.\(id.uuidString)"
        #expect(await eventually {
            guard let data = remote.data(forKey: key) else { return false }
            return (try? JSONDecoder().decode(Stamped<HostRecord>.self, from: data))?.value == second
        })
        try await Task.sleep(for: .milliseconds(150))
        #expect(remote.writeCount == 1)
    }

    @Test func undecodableRemoteRecordIsLeftAlone() async throws {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let id = UUID()
        let key = "host.\(id.uuidString)"
        let future = Data(#"{"value":{"shape":"from a newer app"},"updatedAt":1,"deviceID":"b"}"#.utf8)
        remote.simulateRemote([key: future])
        let store = makeStore(remote: remote, device: "a", clock: clock)

        await store.put(id.uuidString, record("mine", id: id))
        await store.flush()
        #expect(remote.data(forKey: key) == future)
    }

    @Test func localEditBeatsRecordFromAFastClock() async throws {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let store = makeStore(remote: remote, device: "a", clock: clock)
        let id = UUID()
        let key = "host.\(id.uuidString)"
        let ahead = try stamp(record("ahead", id: id), at: clock.read().addingTimeInterval(3600), device: "b")
        remote.simulateRemote([key: ahead])
        #expect(await eventually { await store.all()[id.uuidString] != nil })

        let mine = record("mine", id: id)
        await store.put(id.uuidString, mine)
        remote.simulateRemote([key: ahead])
        try await Task.sleep(for: .milliseconds(100))
        #expect(await store.all()[id.uuidString] == mine)
    }

    @Test func syncOffWritesLocalOnly() async {
        let clock = StoreClock()
        let local = MemoryBackend()
        let remote = MemoryBackend()
        let store = makeStore(local: local, remote: remote, device: "a", clock: clock)
        await store.setSyncEnabled(false)
        let host = record("private")
        await store.put(host.id.uuidString, host)
        await store.flush()

        #expect(await store.all()[host.id.uuidString] == host)
        #expect(remote.allKeys().isEmpty)

        await store.setSyncEnabled(true)
        await store.flush()
        #expect(remote.allKeys() == ["host.\(host.id.uuidString)"])
    }

    @Test func remoteWritesWaitForTheDebounce() async {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let store = makeStore(remote: remote, device: "a", clock: clock)
        let host = record("slow")
        await store.put(host.id.uuidString, host)
        #expect(remote.allKeys().isEmpty)
        await store.flush()
        #expect(remote.allKeys().count == 1)
    }

    @Test func hostStoreListsSavedAndDropsRemoved() async {
        let clock = StoreClock()
        let hosts = HostStore(records: makeStore(remote: nil, device: "a", clock: clock))
        let beta = record("beta")
        let alpha = record("alpha")
        await hosts.save(beta)
        await hosts.save(alpha)
        #expect(await hosts.hosts().map(\.profile.name) == ["alpha", "beta"])

        await hosts.remove(beta.id)
        #expect(await hosts.hosts() == [alpha])
    }

    @Test func settingsDefaultsWhenKeysMissing() async throws {
        let clock = StoreClock()
        let settings = SettingsStore(
            records: SyncedRecordStore(
                prefix: "settings.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { clock.read() }
            ),
            syncSwitch: SyncSwitch(backend: MemoryBackend())
        )
        #expect(await settings.load() == AppSettings())

        let partial = try JSONDecoder().decode(AppSettings.self, from: Data(#"{"themeID":"paper"}"#.utf8))
        #expect(partial.themeID == "paper")
        #expect(partial.fontSize == 13)
        #expect(partial.gestures == .default)
        #expect(partial.keyBar == nil)
    }

    @Test func settingsRoundTripAndSyncSwitch() async {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let switchBackend = MemoryBackend()
        let settings = SettingsStore(
            records: SyncedRecordStore(
                prefix: "settings.", local: MemoryBackend(), remote: remote, deviceID: "a",
                clock: { clock.read() }, debounce: .seconds(60)
            ),
            syncSwitch: SyncSwitch(backend: switchBackend)
        )
        var value = AppSettings()
        value.themeID = "paper"
        value.syncEnabled = false
        await settings.save(value)
        #expect(await settings.load() == value)
        #expect(remote.allKeys().isEmpty)
        #expect(!SyncSwitch(backend: switchBackend).isOn)
    }

    @Test func launchWithSyncOffReadsNothingFromTheRemote() async throws {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let theirs = record("theirs")
        let theirKey = "host.\(theirs.id.uuidString)"
        remote.set(try stamp(theirs, at: clock.read(), device: "b"), forKey: theirKey)
        let store = SyncedRecordStore<HostRecord>(
            prefix: "host.", local: MemoryBackend(), remote: remote, deviceID: "a",
            clock: { clock.read() }, debounce: .seconds(60), syncEnabled: false
        )

        #expect(await store.all().isEmpty)
        let mine = record("mine")
        await store.put(mine.id.uuidString, mine)
        remote.simulateRemote([theirKey: try stamp(theirs, at: clock.read().addingTimeInterval(5), device: "b")])
        await store.flush()
        try await Task.sleep(for: .milliseconds(100))

        #expect(await store.all() == [mine.id.uuidString: mine])
        #expect(remote.allKeys() == [theirKey])
    }

    @Test func newerSettingsFromAnotherDeviceLeaveSyncOff() async throws {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let switchBackend = MemoryBackend()
        SyncSwitch(backend: switchBackend).set(false)
        var theirs = AppSettings()
        theirs.themeID = "paper"
        theirs.syncEnabled = true
        let payload = try JSONEncoder().encode(
            Stamped(value: theirs, updatedAt: clock.read().addingTimeInterval(60), deviceID: "b")
        )
        remote.set(payload, forKey: "settings.app")
        let syncSwitch = SyncSwitch(backend: switchBackend)
        let settings = SettingsStore(
            records: SyncedRecordStore(
                prefix: "settings.", local: MemoryBackend(), remote: remote, deviceID: "a",
                clock: { clock.read() }, debounce: .seconds(60), syncEnabled: syncSwitch.isOn
            ),
            syncSwitch: syncSwitch
        )

        let loaded = await settings.load()
        #expect(!loaded.syncEnabled)
        #expect(loaded.themeID == AppSettings().themeID)
    }

    @Test func syncSwitchNeverTravelsInTheSettingsRecord() async throws {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let local = MemoryBackend()
        let settings = SettingsStore(
            records: SyncedRecordStore(
                prefix: "settings.", local: local, remote: remote, deviceID: "a",
                clock: { clock.read() }, debounce: .seconds(60)
            ),
            syncSwitch: SyncSwitch(backend: MemoryBackend())
        )
        var value = AppSettings()
        value.themeID = "paper"
        await settings.save(value)

        let stored = try #require(local.data(forKey: "settings.app"))
        #expect(!String(decoding: stored, as: UTF8.self).contains("syncEnabled"))
    }

    @Test func unlockAndCommandChoicesStayOnThisDevice() async throws {
        let clock = StoreClock()
        let remote = MemoryBackend()
        let local = MemoryBackend()
        let deviceBackend = MemoryBackend()
        let machine = UUID()
        let records = SyncedRecordStore<AppSettings>(
            prefix: "settings.", local: local, remote: remote, deviceID: "a",
            clock: { clock.read() }, debounce: .seconds(60)
        )
        let settings = SettingsStore(
            records: records,
            syncSwitch: SyncSwitch(backend: MemoryBackend()), device: DeviceSettings(backend: deviceBackend)
        )
        var value = AppSettings()
        value.approveWithoutUnlock = true
        value.liveActivityCommandMachines = [machine]
        value.relayURL = URL(string: "https://relay.example.com")
        await settings.save(value)
        await records.flush()
        #expect(!remote.allKeys().isEmpty)

        let stored = String(decoding: try #require(local.data(forKey: "settings.app")), as: UTF8.self)
        #expect(!stored.contains("approveWithoutUnlock"))
        #expect(!stored.contains("liveActivityCommandMachines"))
        #expect(!stored.contains(machine.uuidString))
        #expect(stored.contains("relay.example.com"), "the relay is a shared choice")
        for key in remote.allKeys() {
            let text = String(decoding: remote.data(forKey: key) ?? Data(), as: UTF8.self)
            #expect(!text.contains("approveWithoutUnlock") && !text.contains(machine.uuidString))
        }
        #expect(await settings.load() == value, "read back from this device's store")

        var other = AppSettings()
        DeviceSettings(backend: MemoryBackend()).apply(to: &other)
        #expect(!other.approveWithoutUnlock)
        #expect(other.liveActivityCommandMachines.isEmpty)
    }

    @Test func settingsFromAnotherDeviceKeepThisDevicesUnlockChoice() async throws {
        let clock = StoreClock()
        let remote = MemoryBackend()
        // An older build, or a tampered record, that still carries the field.
        let updated = clock.read().addingTimeInterval(60).timeIntervalSinceReferenceDate
        remote.set(
            Data(#"{"value":{"themeID":"paper","approveWithoutUnlock":true},"updatedAt":\#(updated),"deviceID":"b"}"#.utf8),
            forKey: "settings.app"
        )
        let settings = SettingsStore(
            records: SyncedRecordStore(
                prefix: "settings.", local: MemoryBackend(), remote: remote, deviceID: "a",
                clock: { clock.read() }, debounce: .seconds(60)
            ),
            syncSwitch: SyncSwitch(backend: MemoryBackend()), device: DeviceSettings(backend: MemoryBackend())
        )
        let loaded = await settings.load()
        #expect(loaded.themeID == "paper")
        #expect(!loaded.approveWithoutUnlock)
    }
}

@Test func localHostsReadOnlyThisDeviceWithoutMerging() async throws {
    let clock = StoreClock()
    let local = MemoryBackend(), remote = MemoryBackend()
    let ada = record("ada"), bea = record("bea"), cyd = record("cyd")
    local.set(try stamp(bea, at: clock.read(), device: "phone"), forKey: "host.\(bea.id.uuidString)")
    local.set(try stamp(ada, at: clock.read(), device: "phone"), forKey: "host.\(ada.id.uuidString)")
    local.set(try stamp(nil, at: clock.read(), device: "phone"), forKey: "host.\(UUID().uuidString)")
    remote.set(try stamp(cyd, at: clock.read(), device: "pad"), forKey: "host.\(cyd.id.uuidString)")
    let records = makeStore(local: local, remote: remote, device: "phone", clock: clock)
    let hosts = HostStore(records: records)

    #expect(hosts.localHosts().map(\.profile.name) == ["ada", "bea"], "sorted, without removed or iCloud-only machines")
    #expect(local.allKeys().count == 3, "reading changes nothing")
    #expect(await hosts.hosts().map(\.profile.name) == ["ada", "bea", "cyd"], "the store's first use still merges")
}
