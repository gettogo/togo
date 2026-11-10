import Foundation
import Synchronization
import Testing
@testable import TogoKit

struct SettingsStoreTests {
    @Test func deviceSettingsNeverSync() async throws {
        let remote = MemoryBackend()
        let local = MemoryBackend()
        let deviceBackend = MemoryBackend()
        let records = SyncedRecordStore<AppSettings>(
            prefix: "settings.", local: local, remote: remote, deviceID: "a",
            clock: { Date(timeIntervalSinceReferenceDate: 0) }, debounce: .seconds(60)
        )
        let settings = SettingsStore(
            records: records,
            syncSwitch: SyncSwitch(backend: MemoryBackend()), device: DeviceSettings(backend: deviceBackend)
        )
        var value = AppSettings()
        value.dictationLocale = "pt-BR"
        value.dictationEngine = .whisper
        value.whisperModel = "base.en-q5"
        await settings.save(value)
        await records.flush()

        let stored = String(decoding: try #require(local.data(forKey: "settings.app")), as: UTF8.self)
        for text in ["dictation", "whisper", "pt-BR", "base.en-q5"] {
            #expect(!stored.contains(text))
        }
        for key in remote.allKeys() {
            let text = String(decoding: remote.data(forKey: key) ?? Data(), as: UTF8.self)
            #expect(!text.contains("pt-BR") && !text.contains("whisper"))
        }
        #expect(await settings.load() == value, "read back from this device's store")

        var other = AppSettings()
        DeviceSettings(backend: MemoryBackend()).apply(to: &other)
        #expect(other.dictationLocale == nil)
        #expect(other.dictationEngine == .apple)
        #expect(other.whisperModel == nil)
    }

    /// A change to this device's own choices alone must not stamp the synced record, or it
    /// would win over a newer edit from another device that has not arrived yet.
    @Test func deviceOnlyChangeKeepsAnotherDevicesNewerEdit() async throws {
        let start = Date(timeIntervalSince1970: 1_800_000_000)
        let now = Mutex(start)
        let remote = MemoryBackend(), local = MemoryBackend(), deviceBackend = MemoryBackend()
        func store() -> SyncedRecordStore<AppSettings> {
            SyncedRecordStore(
                prefix: "settings.", local: local, remote: remote, deviceID: "b",
                clock: { now.withLock { $0 } }, debounce: .seconds(60)
            )
        }
        let records = store()
        let settings = SettingsStore(
            records: records, syncSwitch: SyncSwitch(backend: MemoryBackend()), device: DeviceSettings(backend: deviceBackend)
        )
        var value = AppSettings()
        value.doneSilent = true
        await settings.save(value)
        await records.flush()

        // Another device picks a theme; its record reaches iCloud but not this device yet.
        let edited = start.addingTimeInterval(5).timeIntervalSinceReferenceDate
        remote.set(
            Data(#"{"value":{"themeID":"paper"},"updatedAt":\#(edited),"deviceID":"a"}"#.utf8), forKey: "settings.app"
        )
        // Here, only this device's dictation language changes.
        now.withLock { $0 = start.addingTimeInterval(10) }
        value.dictationLocale = "pt-BR"
        await settings.save(value)

        let relaunched = SettingsStore(
            records: store(), syncSwitch: SyncSwitch(backend: MemoryBackend()), device: DeviceSettings(backend: deviceBackend)
        )
        let loaded = await relaunched.load()
        #expect(loaded.themeID == "paper")
        #expect(loaded.dictationLocale == "pt-BR")
        #expect(loaded.doneSilent)
    }
}
