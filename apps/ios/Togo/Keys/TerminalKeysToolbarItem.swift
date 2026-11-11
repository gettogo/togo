import SwiftUI
import TogoTerminal

/// The controller as the vertical bar's own capsule, after the session items. Custom views
/// default to horizontal placement; explicitly opt this square control into the vertical bar.
/// It keeps the same UIKit face and touch handling as the terminal's controller.
struct TerminalKeysToolbarItem: ToolbarContent {
    let keys: TerminalKeysHost

    var body: some ToolbarContent {
        if #available(iOS 27.1, *) {
            ToolbarSpacer(.fixed, placement: .primaryAction)
            ToolbarItem(placement: .primaryAction) {
                DockControllerView(keys: keys)
            }
            .axisBehavior(.verticalPreferred)
            // A gesture surface cannot retain flicks/repeat inside an overflow menu. Keep
            // it ahead of ordinary actions, even when the keyboard shortens the native dock.
            .visibilityPriority(DockPriority.tier(.keys).priority)
        } else {
            ToolbarItem(placement: .primaryAction) {
                DockControllerView(keys: keys)
            }
        }
    }
}

/// The controller face as a bar item, wired to whichever terminal is focused.
struct DockControllerView: UIViewRepresentable {
    let keys: TerminalKeysHost

    func makeUIView(context: Context) -> ControllerFace {
        let face = ControllerFace()
        face.accessibilityIdentifier = "workspace.terminalKeys"
        keys.attachDockFace(face)
        return face
    }

    func updateUIView(_ face: ControllerFace, context: Context) {
        keys.attachDockFace(face)
    }

    func sizeThatFits(_ proposal: ProposedViewSize, uiView: ControllerFace, context: Context) -> CGSize? {
        CGSize(width: ControllerFace.side, height: ControllerFace.side)
    }
}
