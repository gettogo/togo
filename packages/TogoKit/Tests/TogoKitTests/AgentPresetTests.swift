import Foundation
import Testing
import TogoKit

private func store<Value>(
    _ prefix: String, remote: MemoryBackend?, device: String, local: MemoryBackend = MemoryBackend()
) -> SyncedRecordStore<Value> {
    SyncedRecordStore(
        prefix: prefix, local: local, remote: remote, deviceID: device,
        clock: { Date(timeIntervalSince1970: 1_800_000_000) }, debounce: .seconds(60)
    )
}

@Suite struct AgentPresetTests {
    private let yolo = AgentPreset(
        id: UUID(uuidString: "6F1C0A6E-2D2B-4B7E-9E44-0C8E5B0C1A01")!,
        name: "Claude, no prompts", agent: "claude", program: nil, args: ["--dangerously-skip-permissions"]
    )
    private let kimi = AgentPreset(
        id: UUID(uuidString: "6F1C0A6E-2D2B-4B7E-9E44-0C8E5B0C1A02")!,
        name: "Kimi", agent: AgentPreset.custom, program: "kimi", args: ["--yolo"]
    )

    @Test func presetSyncsWithoutKeys() async throws {
        let remote = MemoryBackend()
        let phone: SyncedRecordStore<AgentPreset> = store(AgentPreset.storePrefix, remote: remote, device: "phone")
        let pad: SyncedRecordStore<AgentPreset> = store(AgentPreset.storePrefix, remote: remote, device: "pad")
        await phone.put(yolo.id.uuidString, yolo)
        await phone.put(kimi.id.uuidString, kimi)
        await phone.flush()

        await pad.setSyncEnabled(false)
        await pad.setSyncEnabled(true)
        let synced = await pad.all()
        #expect(synced[yolo.id.uuidString] == yolo)
        #expect(synced[kimi.id.uuidString]?.args == ["--yolo"])

        // Only preset records reach iCloud, and each carries the launch fields and nothing else.
        let keys = remote.allKeys()
        #expect(keys.count == 2)
        #expect(keys.allSatisfy { $0.hasPrefix(AgentPreset.storePrefix) && $0.utf8.count <= 64 })
        for key in keys {
            let data = try #require(remote.data(forKey: key))
            let object = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
            let value = try #require(object["value"] as? [String: Any])
            #expect(Set(value.keys).isSubset(of: ["id", "name", "agent", "program", "args"]))
        }
    }

    @Test func presetLaunchParams() {
        #expect(yolo.launch(cwd: "/src", name: nil) == CreateSessionParams(
            agent: "claude", cwd: "/src", args: ["--dangerously-skip-permissions"]
        ))
        #expect(kimi.launch(cwd: "/src", name: "k") == CreateSessionParams(
            agent: "custom", cwd: "/src", name: "k", program: "kimi", args: ["--yolo"]
        ))
        var bare = yolo
        bare.args = []
        #expect(bare.launch(cwd: nil, name: nil).args == nil)
        #expect(kimi.command == "kimi")
        #expect(yolo.command == "claude")
    }

    @Test func presetDecodesWithoutOptionalFields() throws {
        let data = Data(#"{"id":"6F1C0A6E-2D2B-4B7E-9E44-0C8E5B0C1A01","name":"C","agent":"claude"}"#.utf8)
        let preset = try JSONDecoder().decode(AgentPreset.self, from: data)
        #expect(preset.args == [])
        #expect(preset.program == nil)
    }

    @Test func folderMemoryKeyedByHostAndCwd() async throws {
        let remote = MemoryBackend()
        let records: SyncedRecordStore<FolderLaunch> = store(FolderLaunchMemory.storePrefix, remote: remote, device: "phone")
        let memory = FolderLaunchMemory(store: records)
        let studio = UUID(), build = UUID()
        let deep = "/Users/sam/src/" + String(repeating: "a-long-folder-name/", count: 20)

        await memory.remember(yolo.id, host: studio, cwd: "/Users/sam/src/app")
        await memory.remember(kimi.id, host: build, cwd: "/Users/sam/src/app")
        await memory.remember(kimi.id, host: studio, cwd: deep)

        #expect(await memory.preset(host: studio, cwd: "/Users/sam/src/app") == yolo.id)
        #expect(await memory.preset(host: studio, cwd: "/Users/sam/src/app/") == yolo.id)
        #expect(await memory.preset(host: build, cwd: "/Users/sam/src/app") == kimi.id)
        #expect(await memory.preset(host: studio, cwd: deep) == kimi.id)
        #expect(await memory.preset(host: studio, cwd: "/Users/sam/src/site") == nil)
        #expect(FolderLaunchMemory.key(host: studio, cwd: "/a") == "\(studio.uuidString)|/a")

        await memory.remember(nil, host: build, cwd: "/Users/sam/src/app")
        #expect(await memory.preset(host: build, cwd: "/Users/sam/src/app") == nil)

        // iCloud keys are capped at 64 bytes, so a long folder still gets a short stored key.
        await records.flush()
        #expect(remote.allKeys().allSatisfy { $0.utf8.count <= 64 && $0.hasPrefix(FolderLaunchMemory.storePrefix) })
        #expect(!remote.allKeys().contains { $0.contains("Users") })
    }

    @Test func forgettingAPresetClearsItsFolders() async {
        let records: SyncedRecordStore<FolderLaunch> = store(FolderLaunchMemory.storePrefix, remote: nil, device: "phone")
        let memory = FolderLaunchMemory(store: records)
        let host = UUID()
        await memory.remember(kimi.id, host: host, cwd: "/a")
        await memory.remember(kimi.id, host: host, cwd: "/b")
        await memory.remember(yolo.id, host: host, cwd: "/c")
        await memory.forget(preset: kimi.id)
        #expect(await memory.preset(host: host, cwd: "/a") == nil)
        #expect(await memory.preset(host: host, cwd: "/b") == nil)
        #expect(await memory.preset(host: host, cwd: "/c") == yolo.id)
        #expect(await records.all().count == 1)
    }

    /// A registry preset is checked under the command the helper's registry runs for it.
    @Test func registryCommandsMatchTheHelper() throws {
        let source = try String(
            contentsOf: URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
                .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
                .appending(path: "helper/internal/agents/agents.go"),
            encoding: .utf8
        )
        let entry = /\{ID: "([a-z0-9-]+)", Name: "[^"]*", Command: "([^"]+)"/
        let helper = Dictionary(uniqueKeysWithValues: source.matches(of: entry).map { (String($0.1), String($0.2)) })
        #expect(helper.count >= 4)
        #expect(AgentPreset.registryCommands == helper)
        for (id, command) in helper {
            #expect(AgentPreset(name: id, agent: id).command == command)
        }
        #expect(kimi.command == "kimi")
    }

    /// The app names every registry agent as the helper does and knows no agent it lacks.
    @Test func catalogNamesMatchTheHelper() throws {
        let source = try String(
            contentsOf: URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
                .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
                .appending(path: "helper/internal/agents/agents.go"),
            encoding: .utf8
        )
        let entry = /\{ID: "([a-z0-9-]+)", Name: "([^"]*)"/
        let helper = source.matches(of: entry).map { "\($0.1)=\($0.2)" }
        #expect(helper.count >= 20)
        #expect(AgentCatalog.entries.map { "\($0.id)=\($0.name)" } == helper)
        #expect(AgentCatalog.mark("shell") == .symbol("terminal"))
        #expect(AgentCatalog.mark(AgentPreset.custom) == .symbol("terminal"))
        #expect(AgentCatalog.mark("newagent") == .monogram("Ne"))
    }
}
