import SwiftUI
import TogoKit
import TogoTerminal

/// An image pasted into a tab's terminal. Compared by identity: pasting the same picture
/// twice is two attempts.
struct PastedImage: Equatable {
    let id = UUID()
    let tab: TabID
    let data: Data

    static func == (lhs: PastedImage, rhs: PastedImage) -> Bool {
        lhs.id == rhs.id
    }
}

extension AppStore {
    /// The Compose key opens the bar for its tab, or closes it when it is already open.
    func toggleCompose(_ id: TabID) {
        // Zen mode hides the compose bar; the Compose key waits until zen ends.
        guard !zen || composing == id else { return }
        composing = composing == id ? nil : id
        TutorialTip.workspace.done()
    }

    /// A pasted image goes to the compose bar's image flow, which uploads it or, on a machine
    /// that cannot take one, says so. The pasteboard is only read, never changed.
    func pasteImage(_ data: Data, into id: TabID) {
        composing = id
        pastedImage = PastedImage(tab: id, data: data)
    }

    /// The tab the compose bar writes to: the focused pane's while two are on screen.
    var composeTab: WorkspaceTab? {
        panesVisible ? focusedTab : selectedTab
    }

    /// The Dictate key: opens the tab's compose bar and starts a dictation that listens until
    /// the next tap. The words land in the bar; nothing reaches the session until Send.
    func dictate(_ id: TabID) {
        // Dictation listens in the compose bar, which zen mode keeps away.
        guard !zen else { return }
        if panesVisible {
            if id == workspace.secondary { workspace.focused = .secondary }
            if id == workspace.selected { workspace.focused = .primary }
        }
        composing = id
        DictationRequests.shared.set(DictationRequests.Request(tab: id, at: .now), for: self)
    }

    /// The tab whose compose bar the Dictate key asked to listen, until a bar takes it.
    var dictationRequest: TabID? {
        DictationRequests.shared.request(for: self)?.tab
    }

    /// Uses up the pending Dictate request. True only when it was for `id`, the tab shows in
    /// this window and the press is recent; whatever the answer, the request is gone, so it
    /// can never switch the microphone on later.
    func takeDictationRequest(for id: TabID, shown: Bool, now: ContinuousClock.Instant = .now) -> Bool {
        guard let request = DictationRequests.shared.request(for: self) else { return false }
        DictationRequests.shared.set(nil, for: self)
        return request.tab == id && shown && now - request.at <= DictationRequests.lifetime
    }

    /// Puts dictated words from Settings into the draft of the tab on screen and opens its bar.
    func insertDictation(_ words: String) {
        let clean = PasteText.sanitize(words)
        guard let tab = composeTab, !clean.isEmpty else { return }
        let draft = drafts[tab.id] ?? ""
        drafts[tab.id] = DictationModel.needsSpace(between: draft, and: clean) ? draft + " " + clean : draft + clean
        composing = tab.id
        path = []
        workspaceOpen = true
    }

    func imageSupport(for hostID: HostID) -> ImageSupport {
        guard let machine = machine(for: hostID), machine.helper != .missing else { return .none }
        return machine.capabilities.contains(Capability.upload) ? .available : .unsupported
    }
}

/// Dictate key presses waiting for a compose bar, one per window store. Kept beside the store
/// rather than in it so the store's own file stays as it is. Stores are held weakly, so a
/// closed window's request goes with it.
@MainActor @Observable final class DictationRequests {
    struct Request {
        var tab: TabID
        var at: ContinuousClock.Instant
    }

    /// A press older than this starts nothing.
    static let lifetime: Duration = .seconds(2)
    static let shared = DictationRequests()

    private final class Box {
        let request: Request
        init(_ request: Request) { self.request = request }
    }

    @ObservationIgnored private let pending = NSMapTable<AppStore, Box>.weakToStrongObjects()
    /// Bumped on every change so views reading a request are told when it changes.
    private var version = 0

    func request(for store: AppStore) -> Request? {
        access(keyPath: \.version)
        return pending.object(forKey: store)?.request
    }

    func set(_ request: Request?, for store: AppStore) {
        if let request { pending.setObject(Box(request), forKey: store) } else { pending.removeObject(forKey: store) }
        version += 1
    }
}

extension ComposeModel {
    /// Starts the dictation the Dictate key asked for, if it asked for this tab just now.
    func takeDictation(for id: TabID, from store: AppStore, shown: Bool) async {
        guard store.takeDictationRequest(for: id, shown: shown) else { return }
        await dictation?.toggle()
    }
}

/// The compose bar under the tab on screen while it is open, or the smart command line under a
/// tab showing its chat. Each tab's draft lives in the store until it is sent or the tab closes.
struct WorkspaceComposeBar: View {
    @Environment(\.windowID) private var window
    let store: AppStore
    let terminals: TerminalPool
    @State private var models: [TabID: ComposeModel] = [:]

    var body: some View {
        Group {
            // A tab showing its chat has the smart command line instead, always at its foot.
            if let tab = store.composeTab, store.layer(for: tab.id) == .chat,
               let model = store.harnessTabs.composer(for: tab.id) {
                SmartCommandLine(model: model)
                    .id(tab.id)
            } else if let tab = store.composeTab, store.composing == tab.id, terminals.shows(tab.id, in: window),
               let handle = terminals.handle(for: tab.id) {
                // With two panes on screen the bar says which one it writes to.
                ComposeBar(model: model(for: tab, handle: handle), focusOnAppear: true, target: store.composeTarget)
                    .id(tab.id)
            }
        }
        .onChange(of: store.dictationRequest, initial: true) {
            guard let id = store.dictationRequest else { return }
            guard let tab = store.workspace.tabs.first(where: { $0.id == id }), terminals.shows(id, in: window),
                  let handle = terminals.handle(for: id) else {
                // Not on screen here: drop the request rather than keep it for later.
                _ = store.takeDictationRequest(for: id, shown: false)
                return
            }
            // Taken here, before any await, so the request is used exactly once.
            guard store.takeDictationRequest(for: id, shown: true) else { return }
            let dictation = model(for: tab, handle: handle).dictation
            Task { await dictation?.toggle() }
        }
        .onChange(of: store.services.settings, initial: true) { _, settings in
            SpeechEngines.select(settings)
        }
        .onChange(of: store.services.settings.dictationLocale) { _, identifier in
            for model in models.values {
                model.dictation?.locale = SpeechEngines.locale(identifier)
            }
        }
        .onChange(of: store.pastedImage) {
            guard let pasted = store.pastedImage, let tab = store.workspace.tabs.first(where: { $0.id == pasted.tab }),
                  let handle = terminals.handle(for: tab.id) else { return }
            store.pastedImage = nil
            let model = model(for: tab, handle: handle)
            Task { await model.images.begin(pasted.data) }
        }
        .onChange(of: store.replyDrafts) {
            // A reply that was not delivered lands in an open bar too, after what is typed there.
            for (id, model) in models {
                guard let draft = store.takeReplyDraft(id) else { continue }
                model.text = model.text.isEmpty ? draft : model.text + "\n" + draft
            }
        }
    }

    private func model(for tab: WorkspaceTab, handle: TerminalHandle) -> ComposeModel {
        if let model = models[tab.id] { return model }
        let store = self.store
        let images = ImagePasteFlow(
            support: store.imageSupport(for: tab.hostID),
            upload: { name, data in
                try await store.services.upload(hostID: tab.hostID, sessionID: tab.sessionID, name: name, data: data)
            },
            paste: { handle.paste($0) }
        )
        let id = tab.id
        let draft = Binding(get: { store.drafts[id] ?? "" }, set: { store.drafts[id] = $0.isEmpty ? nil : $0 })
        let speech = DictationSpeech(
            engine: SpeechEngines.shared, locale: SpeechEngines.locale(store.services.settings.dictationLocale),
            history: DictationHistory()
        )
        // A reply that was not delivered waits here, after any draft already kept for the tab.
        if let reply = store.takeReplyDraft(id) {
            let kept = store.drafts[id] ?? ""
            store.drafts[id] = kept.isEmpty ? reply : kept + "\n" + reply
        }
        let model = ComposeModel(
            draft: draft, history: ComposeHistory(hostID: tab.hostID, sessionID: tab.sessionID), images: images,
            paste: { handle.paste($0) }, pressEnter: { handle.pressEnter() }, speech: speech
        )
        models[tab.id] = model
        return model
    }
}
