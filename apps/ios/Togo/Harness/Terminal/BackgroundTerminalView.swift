import SwiftUI
import TogoKit

/// One background process, by its row or item id: the card with its live tail, and the way to
/// it. A process with a pane of its own opens a live terminal there; one without (an exec, a
/// background shell the agent keeps) shows its output as a Live list. Push it on a navigation
/// stack, as mission control does.
struct BackgroundTerminalView: View {
    let store: HarnessSessionStore
    let itemID: String
    /// Attaches a live terminal to the process; mission control passes
    /// `AppStore.openBackgroundTerminal`.
    let openTerminal: (TerminalTarget) -> Void

    var body: some View {
        Group {
            if let job = store.backgroundJob(itemID) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        BackgroundJobCard(job: job) { open(job) }
                            .background(Color.togoRaised, in: .rect(cornerRadius: 14, style: .continuous))
                        if let screen = job.outputScreen {
                            HarnessScreenLoader(
                                store: store, screenID: screen, openScreen: { _ in }, open: { _ in },
                                showTerminal: { store.layer = .terminal }
                            )
                            .frame(minHeight: 240)
                        }
                    }
                    .padding(16)
                    .frame(maxWidth: 720)
                    .frame(maxWidth: .infinity)
                }
                .navigationTitle(job.title)
                .toolbar {
                    if let target = job.terminal {
                        ToolbarItem(placement: .primaryAction) {
                            Button(TerminalSyncCopy.openTerminal, systemImage: "terminal") { openTerminal(target) }
                                .accessibilityHint(TerminalSyncCopy.openTerminalHint)
                        }
                    }
                }
            } else {
                ContentUnavailableView {
                    Label(TerminalSyncCopy.backgroundGone, systemImage: "terminal")
                }
                .foregroundStyle(Color.togoMute)
            }
        }
        .navigationBarTitleDisplayMode(.inline)
        .background(Color.togoInk)
    }

    private func open(_ job: BackgroundJob) {
        if let target = job.terminal {
            openTerminal(target)
        } else if job.outputScreen == nil {
            store.layer = .terminal
        }
    }
}
