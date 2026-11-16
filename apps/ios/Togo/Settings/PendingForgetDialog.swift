import SwiftUI

/// Forgetting abandons unfinished revocation. The target is consumed once, only after
/// confirmation; canceling leaves the removal task intact.
struct PendingRemovalForget: Equatable {
    private(set) var pending: PendingRemoval?

    mutating func request(_ removal: PendingRemoval) { pending = removal }
    mutating func cancel() { pending = nil }
    mutating func confirm(_ forget: (String) -> Void) {
        guard let target = pending else { return }
        pending = nil
        forget(target.id)
    }
}

struct PendingForgetDialog: ViewModifier {
    @Binding var model: PendingRemovalForget
    let namespace: String
    let forget: (String) -> Void

    func body(content: Content) -> some View {
        content.coordinated("settings.forgetRemoval.\(namespace)", .alert,
            isPresented: Binding { model.pending != nil } set: { if !$0 { model.cancel() } }
        ) { view, shown in
            view.alert(
                settingsText("settings.machines.forget.title", "Forget \(model.pending?.name ?? "")?"),
                isPresented: shown, presenting: model.pending
            ) { _ in
                Button(settingsText("settings.machines.forgetNow", "Forget Now"), role: .destructive) {
                    model.confirm(forget)
                }
                Button(settingsText("settings.confirm.cancel", "Cancel"), role: .cancel) { model.cancel() }
            } message: { removal in
                switch removal.kind {
                case .unpair:
                    Text(settingsText("settings.machines.forget.pairing",
                        "Togo will stop retrying. Notifications may continue until you run togo devices revoke on \(removal.name)."))
                case .key(let comment, _):
                    Text(settingsText("settings.machines.forget.key",
                        "Togo will stop retrying. This device's SSH key may remain on \(removal.name). Remove the line ending in \(comment) from ~/.ssh/authorized_keys on that machine."))
                }
            }
        }
    }
}
