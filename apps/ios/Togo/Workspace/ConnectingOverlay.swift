import SwiftUI

/// Covers a tab's terminal until its link is live, and explains a link that gave up. A
/// reconnect does not cover the terminal: the screen stays readable, dimmed, while it retries.
struct ConnectingOverlay: View {
    enum Kind: Equatable {
        case connecting(String)
        case waking(String)
        case failed(machine: String, reason: String)
        case offline(String)
        case signedOut(String)

        /// Nil while the terminal can be shown. A failure on a machine the monitor already
        /// knows is away reads as offline, which tells the user what to do about it.
        init?(link: LinkPhase, machine: MachineSection?, name: String? = nil) {
            let name = machine?.name ?? name ?? ""
            switch link {
            case .connecting: self = .connecting(name)
            case .waking: self = .waking(name)
            case .live, .roaming, .reconnecting: return nil
            case .signedOut: self = .signedOut(name)
            case .failed(let reason):
                if case .offline? = machine?.reachability {
                    self = .offline(name)
                } else {
                    self = .failed(machine: name, reason: reason)
                }
            }
        }
    }

    let kind: Kind
    let retry: () -> Void

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
    }

    @ViewBuilder private var content: some View {
        switch kind {
        case .connecting(let machine):
            PawWalk(WorkspaceCopy.connectingTo(machine))
        case .waking(let machine):
            PawWalk(WorkspaceCopy.wakingMachine(machine))
        case .failed(let machine, let reason):
            StateView(
                kind: .error, title: WorkspaceCopy.failed(machine), message: reason,
                action: .init(WorkspaceCopy.tryAgain, run: retry)
            )
        case .signedOut(let machine):
            StateView(
                kind: .error, title: WorkspaceCopy.failed(machine), message: WorkspaceCopy.signInUnfinished(machine),
                action: .init(WorkspaceCopy.signInAgain, run: retry)
            )
        case .offline(let machine):
            StateView(
                kind: .offline, title: WorkspaceCopy.offline(machine), message: WorkspaceCopy.offlineMessage,
                action: .init(WorkspaceCopy.tryAgain, run: retry)
            )
        }
    }
}
