import SwiftUI
import TogoKit

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
        composing = composing == id ? nil : id
        TutorialTip.workspace.done()
    }

    /// A pasted image goes to the compose bar's image flow, which uploads it or, on a machine
    /// that cannot take one, says so. The pasteboard is only read, never changed.
    func pasteImage(_ data: Data, into id: TabID) {
        composing = id
        pastedImage = PastedImage(tab: id, data: data)
    }

    func imageSupport(for hostID: HostID) -> ImageSupport {
        guard let machine = machine(for: hostID), machine.helper != .missing else { return .none }
        return machine.capabilities.contains(Capability.upload) ? .available : .unsupported
    }
}

/// The compose bar under the tab on screen while it is open. Each tab's draft lives in the
/// store until it is sent or the tab closes.
struct WorkspaceComposeBar: View {
    let store: AppStore
    let terminals: TerminalPool
    @State private var models: [TabID: ComposeModel] = [:]

    var body: some View {
        Group {
            if let tab = store.selectedTab, store.composing == tab.id, let handle = terminals.handle(for: tab.id) {
                ComposeBar(model: model(for: tab, handle: handle), focusOnAppear: true)
                    .id(tab.id)
            }
        }
        .onChange(of: store.pastedImage) {
            guard let pasted = store.pastedImage, let tab = store.workspace.tabs.first(where: { $0.id == pasted.tab }),
                  let handle = terminals.handle(for: tab.id) else { return }
            store.pastedImage = nil
            let model = model(for: tab, handle: handle)
            Task { await model.images.begin(pasted.data) }
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
        let model = ComposeModel(
            draft: draft, history: ComposeHistory(hostID: tab.hostID, sessionID: tab.sessionID), images: images,
            paste: { handle.paste($0) }, pressEnter: { handle.pressEnter() }
        )
        models[tab.id] = model
        return model
    }
}
