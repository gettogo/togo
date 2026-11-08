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
        /// The tab's machine is not among the saved ones: retrying cannot help, adding it can.
        case gone(String)
        /// The machine showed another key: retrying cannot help, reviewing the key can.
        case hostKeyChanged(String)
        /// This device has no network; the link starts again when it is back.
        case deviceOffline

        /// The kinds that show the paws walking toward a live link.
        var isWalking: Bool {
            switch self {
            case .connecting, .waking: true
            default: false
            }
        }

        /// Nil while the terminal can be shown. A failure on a machine the monitor already
        /// knows is away reads as offline, which tells the user what to do about it. One on a
        /// machine no longer saved (`listed` false) offers to add it instead of trying again.
        init?(link: LinkPhase, machine: MachineSection?, name: String? = nil, listed: Bool = true) {
            let name = machine?.name ?? name ?? ""
            switch link {
            case .connecting: self = .connecting(name)
            case .waking: self = .waking(name)
            case .live, .roaming, .reconnecting: return nil
            case .signedOut: self = .signedOut(name)
            case .failed(let reason):
                if !listed {
                    self = .gone(name)
                } else if case .hostKeyChanged? = machine?.reachability {
                    self = .hostKeyChanged(name)
                } else if case .deviceOffline? = machine?.reachability {
                    self = .deviceOffline
                } else if case .offline? = machine?.reachability {
                    self = .offline(name)
                } else {
                    self = .failed(machine: name, reason: reason)
                }
            }
        }
    }

    let kind: Kind
    /// The link just went live: all four paws are lit before the terminal shows.
    var arrived = false
    let retry: () -> Void
    var addMachine: () -> Void = {}
    var editMachine: () -> Void = {}

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
            // Every touch on the cover is its own; none reaches the terminal underneath.
            .contentShape(Rectangle())
    }

    @ViewBuilder private var content: some View {
        switch kind {
        case .connecting(let machine):
            PawWalk(WorkspaceCopy.connectingTo(machine), ending: arrived ? .arrived : .walking)
        case .waking(let machine):
            PawWalk(WorkspaceCopy.wakingMachine(machine), ending: arrived ? .arrived : .walking)
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
        case .gone(let machine):
            StateView(
                kind: .error, title: WorkspaceCopy.failed(machine), message: WorkspaceCopy.machineGone,
                action: .init(WorkspaceCopy.addMachine, run: addMachine)
            )
        case .offline(let machine):
            StateView(
                kind: .offline, title: WorkspaceCopy.offline(machine), message: WorkspaceCopy.offlineMessage,
                action: .init(WorkspaceCopy.tryAgain, run: retry)
            )
        case .hostKeyChanged(let machine):
            StateView(
                kind: .error, title: WorkspaceCopy.keyChanged(machine), message: WorkspaceCopy.keyChangedMessage,
                action: .init(WorkspaceCopy.reviewKey, run: editMachine)
            )
        case .deviceOffline:
            StateView(
                kind: .offline, title: WorkspaceCopy.deviceOffline(DeviceName.model),
                message: WorkspaceCopy.deviceOfflineMessage, action: .init(WorkspaceCopy.tryAgain, run: retry)
            )
        }
    }
}

/// Shows the overlay for a tab and, when a walking link goes live, keeps it for one more step
/// with every paw lit before the terminal underneath shows. A failure swaps to the error state
/// at once.
struct ConnectingOverlayHost: View {
    let kind: ConnectingOverlay.Kind?
    var addMachine: () -> Void = {}
    var editMachine: () -> Void = {}
    let retry: () -> Void
    @State private var arrivedKind: ConnectingOverlay.Kind?

    var body: some View {
        Group {
            if let kind {
                ConnectingOverlay(kind: kind, retry: retry, addMachine: addMachine, editMachine: editMachine)
            } else if let arrivedKind {
                ConnectingOverlay(kind: arrivedKind, arrived: true, retry: retry, addMachine: addMachine, editMachine: editMachine)
            }
        }
        // motion: state change — connecting gives way to the terminal (or what went wrong) in place.
        .motion(.fade, value: kind ?? arrivedKind)
        .onChange(of: kind) { old, new in
            guard new == nil, let old, old.isWalking else {
                arrivedKind = nil
                return
            }
            arrivedKind = old
            AccessibilityNotification.Announcement(String(localized: "Connected")).post()
            Task {
                try? await Task.sleep(for: PawWalk.step)
                arrivedKind = nil
            }
        }
    }
}
