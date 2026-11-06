import Observation
import SwiftUI

/// Where the workspace's chrome goes: above the terminal, over it for a moment in zen mode, or
/// nowhere. In zen the terminal keeps its full height whether or not the chrome shows.
struct WorkspaceChrome: Equatable {
    /// The header and tab strip sit above the terminal, which is laid out below them.
    var inset: Bool
    /// The header and tab strip lie over the top of the terminal without resizing it.
    var overlay: Bool
    /// A strip along the top edge that shows the chrome when tapped.
    var revealStrip: Bool

    init(zen: Bool, revealed: Bool) {
        inset = !zen
        overlay = zen && revealed
        revealStrip = zen && !revealed
    }
}

/// The chrome shown for a moment in zen mode, after a tap on the strip along the top edge.
@MainActor @Observable final class ZenChrome {
    static let revealDuration: Duration = .seconds(3)
    static let stripHeight: CGFloat = 44

    private(set) var revealed = false
    /// Held on screen, such as while its find field is open. Letting go starts the three seconds.
    private(set) var pinned = false
    @ObservationIgnored private var hiding: Task<Void, Never>?
    @ObservationIgnored private let wait: @MainActor (Duration) async -> Void

    init(wait: @escaping @MainActor (Duration) async -> Void = { try? await Task.sleep(for: $0) }) {
        self.wait = wait
    }

    /// Shows the chrome for three seconds, starting the three seconds again if it is showing.
    func reveal() {
        hiding?.cancel()
        revealed = true
        guard !pinned else { return }
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
        guard zen || (workspaceOpen && !workspaceBehindBoard) else { return }
        zen.toggle()
        if zen { composing = nil }
    }
}

extension AppStore {
    /// The board is the regular-width detail, so the workspace is open but not on screen.
    var workspaceBehindBoard: Bool {
        boardShown && workspaceInline
    }
}

/// The strip along the top edge in zen mode: invisible, 44 pt tall, the chrome on a tap.
struct ZenRevealStrip: View {
    let reveal: () -> Void

    var body: some View {
        Color.clear
            .frame(maxWidth: .infinity)
            .frame(height: ZenChrome.stripHeight)
            .contentShape(.rect)
            .onTapGesture(perform: reveal)
            .accessibilityElement()
            .accessibilityLabel(ZenCopy.showControls)
            .accessibilityAddTraits(.isButton)
            .accessibilityAction { reveal() }
    }
}
