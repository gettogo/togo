import Foundation
import TogoKit

/// Keeps the Shortcuts snapshot file and Spotlight in step with every saved machine. It reads
/// what the services publish, not any window's screen, so it serves the whole app.
@MainActor final class ShortcutsFeed {
    typealias Indexer = (IntentSnapshot?, IntentSnapshot) async -> Bool

    private let file: IntentSnapshotFile
    private let index: Indexer
    private(set) var snapshot: IntentSnapshot
    /// What Spotlight is known to hold; nil until the first full index of this launch.
    private var indexed: IntentSnapshot?
    private var indexing: Task<Void, Never>?

    init(file: IntentSnapshotFile, index: @escaping Indexer = IntentIndex.sync) {
        self.file = file
        self.index = index
        snapshot = file.read()
    }

    /// The saved machines changed: added, renamed or forgotten.
    func machinesChanged(_ machines: [IntentSnapshot.Machine]) {
        commit(snapshot.with(machines: machines))
    }

    /// A machine reported its sessions.
    func published(_ view: HostViewSnapshot) {
        commit(snapshot.applying(view))
    }

    /// Waits for the Spotlight work queued so far.
    func settled() async {
        await indexing?.value
    }

    private func commit(_ next: IntentSnapshot) {
        guard next != snapshot || indexed == nil else { return }
        snapshot = next
        file.write(next)
        let previous = indexing
        let index = self.index
        indexing = Task { [weak self] in
            await previous?.value
            guard let self else { return }
            // A failed step keeps the last state Spotlight took, so the next change retries it.
            if await index(self.indexed, next) { self.indexed = next }
        }
    }

    /// Hands intent links to `open`, the same place any other `togo://` link goes. Cheap, so it
    /// runs at launch: an intent that launched the app must find its way in.
    static func connectIntents(library: AgentLibrary, open: @escaping (DeepLink) -> Void) {
        let hub = IntentHub.local(presets: {
            await library.start()
            return library.presets
        })
        IntentHub.current = hub
        hub.connect(open)
    }

    /// Feeds the file and Spotlight from the services. Waits for the first screen.
    static func start(services: LiveServices) {
        guard let file = IntentSnapshotFile.shared else { return }
        let feed = ShortcutsFeed(file: file)
        services.onPublish = { feed.published($0) }
        Task {
            let machines = Observations {
                services.hostsLoaded ? services.hosts.map { IntentSnapshot.Machine(id: $0.id, name: $0.name) } : nil
            }
            for await list in machines {
                if let list { feed.machinesChanged(list) }
            }
        }
    }
}
