import SwiftUI
import TogoKit
import UIKit

/// The questions a row asks before acting: the kill confirmation and the rename field. Set a
/// target to ask; the list, the sidebar, swipe actions and ⌘⌫ all ask the same way.
struct SessionDialogs: ViewModifier {
    let store: AppStore
    @Binding var kill: SessionRow?
    @Binding var rename: SessionRow?
    @State private var name = ""

    func body(content: Content) -> some View {
        content
            .coordinated("session.kill", .alert, isPresented: presented($kill)) { view, shown in
                view.alert(
                    kill.map { SessionActions.killConfirmation(for: $0).title } ?? "",
                    isPresented: shown, presenting: kill
                ) { row in
                    Button(ControlCenterCopy.kill, role: .destructive) { Task { await store.kill(row) } }
                    Button(ControlCenterCopy.cancel, role: .cancel) {}
                } message: { row in
                    Text(SessionActions.killConfirmation(for: row).message)
                }
            }
            .coordinated("session.rename", .alert, isPresented: presented($rename)) { view, shown in
                view.alert(
                    rename.map { ControlCenterCopy.renameTitle(SessionActions.label($0)) } ?? "",
                    isPresented: shown, presenting: rename
                ) { row in
                    TextField(ControlCenterCopy.name, text: $name)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    Button(ControlCenterCopy.rename) { Task { await store.rename(row, to: name) } }
                    Button(ControlCenterCopy.cancel, role: .cancel) {}
                }
            }
            .onChange(of: rename) { _, row in
                if let row { name = row.name }
            }
            .onChange(of: kill != nil || rename != nil) { _, open in store.dialogOpen = open }
    }

    private func presented<T>(_ target: Binding<T?>) -> Binding<Bool> {
        Binding { target.wrappedValue != nil } set: { if !$0 { target.wrappedValue = nil } }
    }
}

extension AppStore {
    /// Runs a row, card or machine command. `open` shows a session; `kill` and `rename` ask
    /// first; `tools` reach the helper for Run again and restoring.
    func run(
        _ command: SessionCommand, tools: any MachineTools, open: (SessionRow, TransportPreference?) -> Void,
        kill: (SessionRow) -> Void, rename: (SessionRow) -> Void
    ) {
        switch command {
        case .open(let row, let transport):
            open(row, transport)
        case .rename(let row):
            rename(row)
        case .toggleMute(let row):
            Task { await setMute(row, !row.muted) }
        case .copyPath(let row):
            UIPasteboard.general.string = row.cwd
        case .kill(let row):
            kill(row)
        case .runAgain(let row):
            Task { await runAgain(row, tools: tools) }
        case .restore(let hostID, let session, let resume):
            Task { await restore(session, on: hostID, resume: resume, tools: tools) }
        case .dismissRestorable(let hostID, let session):
            Task { await dismissRestorable(session, on: hostID, tools: tools) }
        case .review(let row, let tab):
            review(row, tab: tab)
        case .newSession(let hostID):
            sheet = .newSession(hostID)
        case .retry:
            TutorialTip.offline.done()
            Task { await services.refresh() }
        case .signIn(let hostID):
            sheet = .signIn(hostID)
        case .setUpHere(let hostID):
            sheet = .setUpHere(hostID)
        case .wake(let hostID):
            Task { await wake(hostID) }
        case .installHelper(let hostID), .edit(let hostID):
            sheet = .editMachine(hostID)
        }
    }
}
