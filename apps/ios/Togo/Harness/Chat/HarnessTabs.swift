import Foundation
import Observation
import TogoKit

/// The harness store and composer model of every open tab in one window. A tab's store runs
/// while the tab is open and the app is in the foreground; a closed tab's store stops and its
/// conversation is dropped, so memory follows the open tabs only.
@MainActor @Observable final class HarnessTabs {
    @ObservationIgnored let services: any HarnessServices
    private(set) var stores: [TabID: HarnessSessionStore] = [:]
    @ObservationIgnored private var composers: [TabID: CommandLineModel] = [:]
    /// The last descriptor each tab's session had, so a reopened tab or a fresh store shows its
    /// chat on the first frame and describes again in the background.
    @ObservationIgnored private var descriptors: [TabID: HarnessDescriptor] = [:]
    @ObservationIgnored private var active = true

    init(services: any HarnessServices) {
        self.services = services
    }

    func store(for id: TabID) -> HarnessSessionStore? {
        stores[id]
    }

    /// The composer model for a tab with a store, made on first use and kept with the store.
    func composer(for id: TabID) -> CommandLineModel? {
        if let model = composers[id] { return model }
        guard let store = stores[id] else { return nil }
        let model = CommandLineModel(store: store)
        composers[id] = model
        return model
    }

    /// Keeps one store per open tab. `capabilities` gives the helper capabilities of a tab's
    /// machine, nil while the machine is not listed yet; a tab waits for them. A tab whose
    /// machine gains harness.v1 gets a fresh store. `expectsChat` says whether a tab's session
    /// should have a chat, so its store shows ink rather than the terminal until it knows.
    func sync(tabs: [TabID], capabilities: (HostID) -> Set<String>?, expectsChat: @escaping (TabID) -> Bool = { _ in false }) {
        let open = Set(tabs)
        for id in stores.keys where !open.contains(id) {
            // Its descriptor stays, so reopening the tab shows its chat at once.
            remove(id)
        }
        for id in tabs {
            guard let caps = capabilities(id.hostID) else { continue }
            let subject = HarnessSubject(hostID: id.hostID, sessionID: id.sessionID, capabilities: caps)
            if let existing = stores[id] {
                guard subject.available, !existing.subject.available else { continue }
                remove(id)
            }
            let store = HarnessSessionStore(
                subject: subject, services: services, expectsChat: expectsChat(id),
                expectsChatLive: { expectsChat(id) }, cached: descriptors[id]
            )
            stores[id] = store
            if active { store.launch() }
        }
    }

    /// The descriptor kept for a tab, for tests and for a store made again.
    func cachedDescriptor(for id: TabID) -> HarnessDescriptor? {
        descriptors[id] ?? stores[id]?.descriptor.flatMap { $0.adapter == nil ? nil : $0 }
    }

    /// The app went to the background or came back. Connections close in the background, so
    /// the stores stop there and pick up from their cursors on return.
    func setActive(_ active: Bool) {
        guard active != self.active else { return }
        self.active = active
        for store in stores.values {
            if active { store.launch() } else { store.stop() }
        }
    }

    /// The window closed: every store stops and is dropped, closing its feed and helper.
    func stopAll() {
        for id in Array(stores.keys) { remove(id) }
    }

    private func remove(_ id: TabID) {
        if let described = stores[id]?.descriptor, described.adapter != nil { descriptors[id] = described }
        stores.removeValue(forKey: id)?.stop()
        composers[id] = nil
    }
}

/// One `HarnessTabs` per window store. Kept beside the store rather than in it, so the store's
/// own file stays as it is. A closed window's tabs are released by `release`; the weak keys
/// are only a backstop.
@MainActor final class HarnessTabsRegistry {
    static let shared = HarnessTabsRegistry()

    private let tabs = NSMapTable<AppStore, HarnessTabs>.weakToStrongObjects()

    func tabs(for store: AppStore) -> HarnessTabs {
        if let existing = tabs.object(forKey: store) { return existing }
        let made = HarnessTabs(services: store.harnessServices)
        tabs.setObject(made, forKey: store)
        return made
    }

    /// The window of `store` is gone: its stores stop now rather than whenever the map lets
    /// them go.
    func release(_ store: AppStore) {
        tabs.object(forKey: store)?.stopAll()
        tabs.removeObject(forKey: store)
    }
}
