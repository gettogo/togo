import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct AgentOrderTests {
    private let agents = [
        AgentInfo(id: "codex", name: "Codex", installed: true),
        AgentInfo(id: "aider", name: "Aider", installed: true),
        AgentInfo(id: "claude", name: "Claude Code", installed: true),
        AgentInfo(id: "qwen", name: "Qwen Code", installed: true),
        AgentInfo(id: "pi", name: "Pi", installed: true),
    ]

    @Test func favoritesThenRecentThenByName() {
        let sorted = AgentOrder.sorted(agents, favorites: ["qwen", "codex"], recent: ["pi", "qwen"])
        #expect(sorted.map(\.id) == ["qwen", "codex", "pi", "aider", "claude"])
    }

    @Test func noPicksIsAlphabetical() {
        #expect(AgentOrder.sorted(agents, favorites: [], recent: []).map(\.id) == ["aider", "claude", "codex", "pi", "qwen"])
    }

    @Test func usedMovesToFrontAndCaps() {
        var recent: [String] = []
        for id in ["a", "b", "c", "d", "e", "f", "g", "b"] { recent = AgentOrder.used(id, in: recent) }
        #expect(recent == ["b", "g", "f", "e", "d", "c"])
    }

    @Test func toggledAddsAtEndAndRemoves() {
        #expect(AgentOrder.toggled("b", in: ["a"]) == ["a", "b"])
        #expect(AgentOrder.toggled("a", in: ["a", "b"]) == ["b"])
    }

    @Test func storedIdsAreCleaned() {
        #expect(AgentOrder.ids(from: ["a", "", "a", "b"]) == ["a", "b"])
        #expect(AgentOrder.ids(from: "a").isEmpty)
        #expect(AgentOrder.ids(from: nil).isEmpty)
    }

    @Test func libraryKeepsPicksInDefaults() throws {
        let suite = "agent-order-tests"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defaults.removePersistentDomain(forName: suite)
        let make = {
            AgentLibrary(
                presets: SyncedRecordStore<AgentPreset>(
                    prefix: AgentPreset.storePrefix, local: MemoryBackend(), remote: nil, deviceID: "t", clock: { Date() }
                ),
                launches: SyncedRecordStore<FolderLaunch>(
                    prefix: FolderLaunchMemory.storePrefix, local: MemoryBackend(), remote: nil, deviceID: "t", clock: { Date() }
                ),
                defaults: defaults
            )
        }
        let library = make()
        library.toggleFavorite("claude")
        library.used("codex")
        let again = make()
        #expect(again.favorites == ["claude"])
        #expect(again.recentAgents == ["codex"])
        defaults.removePersistentDomain(forName: suite)
    }
}
