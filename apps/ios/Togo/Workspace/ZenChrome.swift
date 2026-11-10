import Observation
import SwiftUI

/// What the workspace's chrome shows. The bar and the rows under it keep their place in every
/// mode, so zen never changes the terminal's frame and the grid is never refitted for it: zen
/// only empties them, leaving one bar item that brings the rest back for a moment.
struct WorkspaceChrome: Equatable {
    /// The bar's items and the rows above the terminal are on screen.
    var shown: Bool
    /// The bar holds only "Show controls".
    var revealItem: Bool

    init(zen: Bool, revealed: Bool) {
        shown = !zen || revealed
        revealItem = zen && !revealed
    }
}

/// The chrome shown for a moment in zen mode, after a tap on "Show controls".
@MainActor @Observable final class ZenChrome {
    static let revealDuration: Duration = .seconds(3)

    private(set) var revealed = false
    /// Held on screen, such as while its find field is open. Letting go starts the three seconds.
    private(set) var pinned = false
    @ObservationIgnored private var hiding: Task<Void, Never>?
    @ObservationIgnored private let wait: @MainActor (Duration) async -> Void

    init(wait: @escaping @MainActor (Duration) async -> Void = { try? await Task.sleep(for: $0) }) {
        self.wait = wait
    }

    /// Shows the chrome for three seconds, starting the three seconds again if it is showing.
    /// With VoiceOver on it stays until zen ends: three seconds is not enough to reach the items.
    func reveal() {
        hiding?.cancel()
        revealed = true
        guard !pinned, !UIAccessibility.isVoiceOverRunning else { return }
        hiding = Task { [weak self, wait] in
            await wait(Self.revealDuration)
            guard !Task.isCancelled, let self, !self.pinned else { return }
            self.revealed = false
        }
    }

    func pin(_ held: Bool) {
        guard held != pinned else { return }
        pinned = held
        if held {
            hiding?.cancel()
            hiding = nil
            revealed = true
        } else if revealed {
            reveal()
        }
    }

    func hide() {
        hiding?.cancel()
        hiding = nil
        pinned = false
        revealed = false
    }
}

enum ZenCopy {
    static let zenMode = String(localized: "zen.mode", defaultValue: "Zen mode")
    static let showControls = String(localized: "zen.showControls", defaultValue: "Show controls")
}

extension AppStore {
    /// ⌃⌘F and "Zen mode" in the workspace menu. Hides the header, tab strip and compose bar of
    /// this window's workspace; a draft stays where it was.
    func toggleZen() {
        guard zen || workspaceOpen else { return }
        zen.toggle()
        if zen { composing = nil }
    }
}
