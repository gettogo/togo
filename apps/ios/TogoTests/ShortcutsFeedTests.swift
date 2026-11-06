import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor private final class FakeIndex {
    var calls: [(old: IntentSnapshot?, new: IntentSnapshot)] = []
    var succeeds = true

    func run(_ old: IntentSnapshot?, _ new: IntentSnapshot) -> Bool {
        calls.append((old, new))
        return succeeds
    }
}

@MainActor struct ShortcutsFeedTests {
    private func file() -> IntentSnapshotFile {
        IntentSnapshotFile(url: FileManager.default.temporaryDirectory.appending(path: "\(UUID().uuidString)/snapshot.json"))
    }

    @Test func snapshotKeepsNamesOnly() throws {
        var machines = Samples.controlCenter.machines
        let first = IntentSamples.snapshot
        #expect(first.sessions.map(\.bib) == ["01", "04", "02"])
        // A machine out of reach keeps the sessions it last reported.
        machines[1].reachability = .offline(since: Samples.now, reason: "No route to host")
        machines[1].sessions = []
        let later = first.applying(IntentSamples.view(machines[1]))
        #expect(later.sessions.filter { $0.hostID == Samples.build }.map(\.bib) == ["02"])
        // A machine that is gone takes its sessions with it, and a renamed one renames them.
        let fewer = first.with(machines: [IntentSnapshot.Machine(id: Samples.studio, name: "Studio Mac")])
        #expect(fewer.machines.map(\.name) == ["Studio Mac"])
        #expect(fewer.sessions.map(\.machine) == ["Studio Mac", "Studio Mac"])
        // A machine the snapshot does not hold yet adds nothing.
        #expect(IntentSnapshot().applying(IntentSamples.view(machines[0])) == IntentSnapshot())

        let file = file()
        file.write(first)
        #expect(file.read() == first)
        let text = try String(contentsOf: file.url, encoding: .utf8)
        for hidden in ["studio.local", "tail1234", "Editing routes.ts", "/Users/sam"] {
            #expect(!text.contains(hidden), "\(hidden)")
        }
        #if !targetEnvironment(simulator)
        let protection = try FileManager.default.attributesOfItem(atPath: file.url.path)[.protectionKey] as? FileProtectionType
        #expect(protection == .completeUntilFirstUserAuthentication)
        #endif
    }

    /// The feed follows what the services publish, so it needs no window at all.
    @Test func feedFollowsServices() async {
        let fake = FakeIndex()
        let file = file()
        let feed = ShortcutsFeed(file: file) { old, new in fake.run(old, new) }
        feed.machinesChanged(IntentSamples.machines)
        for section in Samples.controlCenter.machines {
            feed.published(IntentSamples.view(section))
        }
        await feed.settled()
        #expect(file.read() == IntentSamples.snapshot)
        // The first index of a launch rebuilds Spotlight; later ones only apply the change.
        #expect(fake.calls.first?.old == nil)
        #expect(fake.calls.last?.old != nil)

        feed.machinesChanged(Array(IntentSamples.machines.prefix(1)))
        await feed.settled()
        #expect(file.read().sessions.allSatisfy { $0.hostID == Samples.studio })
        #expect(fake.calls.last?.new.machines.map(\.name) == ["Studio"])
    }

    /// A change Spotlight refused is tried again from the last state it took.
    @Test func failedIndexIsRetried() async {
        let fake = FakeIndex()
        fake.succeeds = false
        let feed = ShortcutsFeed(file: file()) { old, new in fake.run(old, new) }
        feed.machinesChanged(IntentSamples.machines)
        await feed.settled()
        fake.succeeds = true
        feed.published(IntentSamples.view(Samples.controlCenter.machines[0]))
        await feed.settled()
        #expect(fake.calls.count == 2)
        #expect(fake.calls[1].old == nil)
        feed.published(IntentSamples.view(Samples.controlCenter.machines[1]))
        await feed.settled()
        #expect(fake.calls[2].old == fake.calls[1].new)
    }
}
