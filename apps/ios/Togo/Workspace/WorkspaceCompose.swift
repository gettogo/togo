import PhotosUI
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
    /// The Attach key: the photo picker for the tab's terminal, the picture then sent to its
    /// machine and its path typed. Zen mode keeps the key away.
    func attach(_ id: TabID) {
        guard !zen else { return }
        focusPane(showing: id)
        attaching = id
        tipDone(.workspace)
    }

    /// A pasted image goes to the tab's image flow, which uploads it or, on a machine that
    /// cannot take one, says so. The pasteboard is only read, never changed.
    func pasteImage(_ data: Data, into id: TabID) {
        focusPane(showing: id)
        pastedImage = PastedImage(tab: id, data: data)
    }

    /// The tab the workspace's input belongs to: the focused pane's while two are on screen.
    var composeTab: WorkspaceTab? {
        panesVisible ? focusedTab : selectedTab
    }

    /// The Dictate key: listens until the next tap, then types the words into the tab's
    /// terminal. Nothing presses Enter; the session's own prompt holds them.
    func dictate(_ id: TabID) {
        // Zen mode keeps the input tools away.
        guard !zen else { return }
        focusPane(showing: id)
        DictationRequests.shared.set(DictationRequests.Request(tab: id, at: .now), for: self)
    }

    /// A key pressed in the other pane's terminal moves focus there, so what it starts shows.
    private func focusPane(showing id: TabID) {
        guard panesVisible else { return }
        if id == workspace.secondary {
            focus(.secondary)
        } else if id == workspace.selected {
            focus(.primary)
        }
    }

    /// The tab whose input tools the Dictate key asked to listen, until they take it.
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

    /// Dictated words from Settings wait for the tab on screen (`pendingInput`): its chat's
    /// composer takes them, or its terminal has them typed. The workspace comes forward.
    func insertDictation(_ words: String) {
        let clean = PasteText.sanitize(words)
        guard let tab = composeTab, !clean.isEmpty else { return }
        let waiting = pendingInput[tab.id] ?? ""
        pendingInput[tab.id] = DictationModel.needsSpace(between: waiting, and: clean) ? waiting + " " + clean : waiting + clean
        path = []
        workspaceOpen = true
    }

    func imageSupport(for hostID: HostID) -> ImageSupport {
        guard let machine = machine(for: hostID), machine.helper != .missing else { return .none }
        return machine.capabilities.contains(Capability.upload) ? .available : .unsupported
    }
}

/// Dictate key presses waiting for a terminal's input tools, one per window store. Kept beside the store
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

extension TerminalInputModel {
    /// Starts the dictation the Dictate key asked for, if it asked for this tab just now.
    func takeDictation(for id: TabID, from store: AppStore, shown: Bool) async {
        guard store.takeDictationRequest(for: id, shown: shown) else { return }
        await dictation?.toggle()
    }
}

/// The input under the tab on screen. A tab showing its chat has the smart command line, always
/// at its foot. A tab showing its terminal has no composer: the terminal takes the typing, with
/// the key bar on the keyboard, and only the status of its input tools (dictation, a picture on
/// its way) floats over its foot while they work. Words waiting for a tab (`pendingInput`) go
/// to its composer or are typed into its terminal.
struct WorkspaceComposeBar: View {
    @Environment(\.windowID) private var window
    @Environment(\.loopsPaused) private var loopsPaused
    let store: AppStore
    let terminals: TerminalPool
    @State private var tools: [TabID: TerminalInputModel] = [:]
    @State private var photo: PhotosPickerItem?
    /// The tab the open photo picker sends to.
    @State private var picking: TabID?

    var body: some View {
        ZStack(alignment: .bottom) {
            // While the chat is still expected (the loader) nothing shows: no composer that
            // cannot type, no terminal status that is about to go (S4).
            // Under the terminal layer the composer keeps its place, only hidden, so the bar's
            // height, and with it the chat's frame, never changes when the layers switch: the
            // chat's text stays put while it crossfades instead of sliding by the composer's
            // height. The terminal reaches under the bar (`WorkspacePanes`), so the hidden
            // composer takes nothing from it.
            if let tab = store.composeTab, let model = chatComposer(for: tab.id) {
                let shown = SessionInput(store.layer(for: tab.id)) == .composer
                SmartCommandLine(model: model)
                    // The terminal under the chat, for the composer's terminal peek.
                    .onAppear { model.peekTerminal = .live { [terminals] in terminals.handle(for: tab.id) } }
                    // The palette and the screens it opens follow links as the chat does.
                    .environment(\.openURL, OpenURLAction { [store] url in store.openChatLink(url, from: tab.id) })
                    // Under the chat's centred column on a wide window, not at the pane's edge.
                    .frame(maxWidth: ChatLayout.composerWidth)
                    .frame(maxWidth: .infinity)
                    .modifier(ComposerMargins())
                    // A little air above the keyboard, and above the home indicator.
                    .padding(.bottom, ComposerMargins.bottom)
                    // motion: cause → effect — fades with the chat in the toggle's own
                    // transaction; opacity only, so nothing above it moves.
                    .opacity(shown ? 1 : 0)
                    // Hidden, it takes no touches, keys, focus or VoiceOver, and its loops hold.
                    .disabled(!shown)
                    .allowsHitTesting(shown)
                    .accessibilityHidden(!shown)
                    .environment(\.loopsPaused, loopsPaused || !shown)
                    .id(tab.id)
                    // motion: state change — another tab's composer replaces this one as a cut.
                    .transition(.identity)
            }
            if let id = terminalTab, let model = tools[id] {
                // Over the terminal's last rows, never resizing it: the grid and the remote keep
                // their size while dictation listens or a picture goes.
                TerminalInputStatus(model: model)
                    .frame(maxWidth: ChatLayout.composerWidth)
                    .frame(maxWidth: .infinity)
                    .modifier(ComposerMargins())
                    .padding(.bottom, ComposerMargins.bottom)
                    .id(id)
                    // motion: state change — another tab's status replaces this one as a cut.
                    .transition(.identity)
            }
        }
        .photosPicker(isPresented: pickerShown, selection: $photo, matching: .images)
        .onChange(of: photo) { _, item in
            guard let item, let id = picking, let model = tools[id] else { return }
            photo = nil
            picking = nil
            Task { await model.images.load { try await item.loadTransferable(type: Data.self) } }
        }
        .onChange(of: store.attaching, initial: true) { _, id in
            guard let id else { return }
            store.attaching = nil
            guard let model = model(for: id) else { return }
            if model.images.picksPhotos {
                picking = id
            } else {
                model.images.explainUnsupported()
            }
        }
        .onChange(of: terminalTab) { old, _ in
            // A terminal that leaves the screen, or goes under its chat, stops listening; what
            // was heard is still typed.
            if let old { tools[old]?.dictation?.relinquish() }
        }
        .onChange(of: store.dictationRequest, initial: true) {
            guard let id = store.dictationRequest else { return }
            guard terminals.shows(id, in: window), let model = model(for: id) else {
                // Not on screen here: drop the request rather than keep it for later.
                _ = store.takeDictationRequest(for: id, shown: false)
                return
            }
            // Taken here, before any await, so the request is used exactly once.
            guard store.takeDictationRequest(for: id, shown: true) else { return }
            let dictation = model.dictation
            Task { await dictation?.toggle() }
        }
        .onChange(of: store.services.settings, initial: true) { _, settings in
            SpeechEngines.select(settings)
        }
        .onChange(of: store.services.settings.dictationLocale) { _, identifier in
            for model in tools.values {
                model.dictation?.locale = SpeechEngines.locale(identifier)
            }
        }
        .onChange(of: store.pastedImage) {
            guard let pasted = store.pastedImage, let model = model(for: pasted.tab) else { return }
            store.pastedImage = nil
            Task { await model.images.begin(pasted.data) }
        }
        .onChange(of: waitingKey, initial: true) { deliverPendingInput() }
    }

    /// The tab whose terminal takes the typing on screen here: the focused one, showing its
    /// terminal layer in this window.
    private var terminalTab: TabID? {
        guard let tab = store.composeTab, SessionInput(store.layer(for: tab.id)) == .terminal,
              terminals.shows(tab.id, in: window) else { return nil }
        return tab.id
    }

    private var pickerShown: Binding<Bool> {
        Binding(get: { picking != nil }, set: { if !$0 { picking = nil } })
    }

    /// The chat's composer for a tab whose session has a chat, whichever layer shows.
    private func chatComposer(for id: TabID) -> CommandLineModel? {
        guard store.harness(for: id)?.chatAvailable == true, store.layer(for: id) != .pending else { return nil }
        return store.harnessTabs.composer(for: id)
    }

    // MARK: Waiting words

    /// What decides whether waiting words can go: each waiting tab's layer and link.
    private var waitingKey: [String] {
        store.pendingInput.keys.sorted { $0.sessionID < $1.sessionID }.map { id in
            "\(id.hostID.uuidString)/\(id.sessionID)/\(store.layer(for: id).rawValue)/\(isLive(id))"
        }
    }

    /// Words waiting for a tab on screen here go to the input it shows: the chat's composer,
    /// after what is typed there, or the terminal once it is live. Typed as a paste, never with
    /// Enter, so they wait at the session's prompt. A tab still deciding keeps them.
    private func deliverPendingInput() {
        for id in Array(store.pendingInput.keys) where terminals.shows(id, in: window) {
            switch store.layer(for: id) {
            case .chat:
                guard let composer = chatComposer(for: id), let text = store.takePendingInput(id) else { continue }
                composer.draft = composer.draft.isEmpty ? text : composer.draft + "\n" + text
            case .terminal:
                guard isLive(id), let handle = terminals.handle(for: id), let text = store.takePendingInput(id) else { continue }
                handle.paste(text)
            case .pending:
                continue
            }
        }
    }

    private func isLive(_ id: TabID) -> Bool {
        switch store.workspace.tabs.first(where: { $0.id == id })?.link {
        case .live?, .roaming?: true
        default: false
        }
    }

    // MARK: Tools

    /// The tab's input tools, made the first time they are needed. Nil when the tab is gone or
    /// has no terminal attached.
    private func model(for id: TabID) -> TerminalInputModel? {
        if let model = tools[id] { return model }
        guard let tab = store.workspace.tabs.first(where: { $0.id == id }), let handle = terminals.handle(for: id) else {
            return nil
        }
        let store = self.store
        // The tools outlive the terminal they were made for: Try again, a transport change or a
        // tab let go past the pool's limit attaches a new one. Text goes to the terminal the
        // tab has when it is typed, never to a stopped one that would drop it.
        let current: @MainActor () -> TerminalHandle = { [terminals] in terminals.handle(for: id) ?? handle }
        let images = ImagePasteFlow(
            support: store.imageSupport(for: tab.hostID),
            upload: { name, data in
                try await store.services.upload(hostID: tab.hostID, sessionID: tab.sessionID, name: name, data: data)
            },
            paste: { current().paste($0) }
        )
        let speech = DictationSpeech(
            engine: SpeechEngines.shared, locale: SpeechEngines.locale(store.services.settings.dictationLocale),
            history: DictationHistory()
        )
        let model = TerminalInputModel(images: images, paste: { current().paste($0) }, speech: speech)
        tools[id] = model
        return model
    }
}

/// The same breathing room on both sides of the composer, on every layout: the iPhone's margin.
/// A safe-area inset is not a gap: beside the split view's sidebar the detail column's leading
/// inset is the sidebar itself, so a composer that trusted it sat flush against the divider.
/// A vertical bar or a sensor housing keeps its own inset, and the margin comes on top.
struct ComposerMargins: ViewModifier {
    static let margin: CGFloat = 12
    /// The gap between the composer and the keyboard or the bottom safe area.
    static let bottom: CGFloat = 8

    func body(content: Content) -> some View {
        content.padding(.horizontal, Self.margin)
    }
}
