import Foundation
import TogoKit
import TogoTerminal

extension TerminalNativeInteraction {
    /// Panel links open on this device, never by typing the desktop's shortcut.
    /// Revalidate the offered action after a refresh or a handback.
    func openPanelLink(_ action: String, args: [String: JSONValue]?,
                       open: (URL) -> Void) -> HarnessActionResult {
        guard phase == .active, !busy, !closing, let screen,
              screen.primitive == .detail, let token = screen.interaction,
              action.hasPrefix("terminal.openLink."),
              let offered = screen.detail?.actions.first(where: { $0.id == action }),
              args == offered.args, args?["interaction"] == .string(token),
              case .string(let text) = args?["url"],
              let url = URL(string: text), LinkPolicy.allows(url),
              url.user == nil, url.password == nil else {
            return .init(result: .failed, message: SurfaceCopy.inputChanged)
        }
        open(url)
        // A browser opening is not evidence that sign-in finished.
        return .init(result: .typed)
    }
}
