import Foundation
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
}
