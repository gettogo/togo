import Foundation
import Testing
@testable import TogoKit

struct FutureRecordTests {
    private func futureSettings() throws -> Data {
        let data = try JSONEncoder().encode(Stamped(value: AppSettings(), updatedAt: Date(), deviceID: "newer"))
        var object = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
        var value = try #require(object["value"] as? [String: Any])
        value["transport"] = "future-transport"
        object["value"] = value
        return try JSONSerialization.data(withJSONObject: object)
    }

    private func store(_ backend: MemoryBackend) -> SyncedRecordStore<AppSettings> {
        SyncedRecordStore(prefix: "settings.", local: backend, remote: nil, deviceID: "older", clock: { Date() })
    }

    @Test func savingDefaultsCannotOverwriteARecordFromANewerApp() async throws {
        let local = MemoryBackend()
        let original = try futureSettings()
        local.set(original, forKey: "settings.app")
        let records = store(local)
        #expect(await records.all().isEmpty)
        var changed = AppSettings()
        changed.fontSize = 20
        await records.put("app", changed)
        #expect(local.data(forKey: "settings.app") == original)
    }

    @Test func aMergeCannotOverwriteAnUnreadableDestination() async throws {
        let local = MemoryBackend()
        let records = store(local)
        await records.put("old", AppSettings())
        let old = local.data(forKey: "settings.old")
        let future = try futureSettings()
        local.set(future, forKey: "settings.future")
        await records.put("future", AppSettings(), deleting: ["old"])
        #expect(local.data(forKey: "settings.future") == future)
        #expect(local.data(forKey: "settings.old") == old)
    }

    @Test func unreadableMergeMemberPreventsAPartialMerge() async throws {
        let local = MemoryBackend()
        let records = store(local)
        await records.put("kept", AppSettings())
        let old = local.data(forKey: "settings.kept")
        let future = try futureSettings()
        local.set(future, forKey: "settings.future")
        var changed = AppSettings()
        changed.themeID = "changed"
        await records.put("kept", changed, deleting: ["future"])
        #expect(local.data(forKey: "settings.kept") == old)
        #expect(local.data(forKey: "settings.future") == future)
    }
}
