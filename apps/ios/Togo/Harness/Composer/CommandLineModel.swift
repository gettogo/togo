import Foundation
import Observation
import TogoKit

/// The smart command line's state: the draft, the / and @ menus, the mode chip and the
/// prompts queued while the agent works.
@MainActor @Observable final class CommandLineModel {
    /// What the menu above the field lists.
    enum Menu: Equatable {
        case none
        case commands([HarnessCompletion])
        case files([HarnessCompletion])
    }

    var draft = ""
    private(set) var menu: Menu = .none
    /// The mode chip's current mode id and choices, from the descriptor.
    private(set) var mode = ""
    private(set) var modes: [HarnessChoice] = []
    /// The agent is working, so a send queues and Interrupt shows.
    private(set) var working = false

    @ObservationIgnored let store: HarnessSessionStore

    init(store: HarnessSessionStore) {
        self.store = store
    }

    /// Updates the menu for the draft: a leading "/" lists commands, an "@" word lists files.
    func draftChanged() async {}

    func choose(_ completion: HarnessCompletion) {}

    func submit() async {}

    func interrupt() async {}

    func setMode(_ id: String) async {}
}
