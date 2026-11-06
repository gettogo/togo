import SwiftUI
import TogoTerminal

/// The detail beside the sidebar when no session is on screen. The shortcut for a new session
/// lives on the sidebar's plus button, where the system's ⌘ list finds it.
struct EmptyDetail: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.hardwareKeyboard) private var keyboard
    var action: EmptyDetailAction = .newSession
    /// The sidebar is hidden, so this screen also offers to show it.
    var sidebarHidden = false
    let run: (EmptyDetailAction) -> Void
    var showSidebar: () -> Void = {}

    var body: some View {
        VStack(spacing: 14) {
            VStack(spacing: 14) {
                Image(systemName: "terminal")
                    .font(.system(.largeTitle).weight(.light))
                    .foregroundStyle(Color.togoMute)
                    .accessibilityHidden(true)
                Text(action == .newSession ? EmptyDetailCopy.title : EmptyDetailCopy.noMachines)
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                    .multilineTextAlignment(.center)
                if action == .newSession {
                    Text(EmptyDetailCopy.message)
                        .font(.body)
                        .foregroundStyle(Color.togoMute)
                        .multilineTextAlignment(.center)
                }
            }
            .accessibilityElement(children: .combine)
            VStack(spacing: 4) {
                Button(action == .newSession ? EmptyDetailCopy.newSession : EmptyDetailCopy.addMachine) { run(action) }
                    .buttonStyle(.primary(.fit))
                    .hoverEffect(.highlight)
                    .accessibilityHint(action == .newSession ? keyboard.hint(EmptyDetailCopy.shortcutHint) ?? "" : "")
                // The shortcut is named only where there is a keyboard to press it on.
                if action == .newSession, let hint = keyboard.hint(EmptyDetailCopy.shortcut) {
                    Text(hint)
                        .font(.footnote)
                        .foregroundStyle(Color.togoMute)
                        .accessibilityHidden(true)
                }
                if sidebarHidden {
                    Button(EmptyDetailCopy.showSidebar, action: showSidebar)
                        .buttonStyle(.secondary(.fit))
                }
            }
            .padding(.top, 8)
        }
        .frame(maxWidth: dynamicTypeSize.isAccessibilitySize ? .infinity : 340)
        .padding(.horizontal, Layout.margin)
        .centredInDetail()
        // Behind the floating sidebar, the empty canvas carries on under the glass.
        .background { Color.togoInk.backgroundExtensionEffect() }
    }
}

enum EmptyDetailCopy {
    static let title = String(localized: "sidebar.empty.title", defaultValue: "No session open")
    static let message = String(
        localized: "sidebar.empty.message", defaultValue: "Pick a session in the sidebar, or start a new one."
    )
    static let newSession = String(localized: "sidebar.empty.newSession", defaultValue: "New session")
    static let shortcut = String(localized: "sidebar.empty.shortcut", defaultValue: "or press ⌘T")
    static let shortcutHint = String(localized: "sidebar.empty.shortcutHint", defaultValue: "Command T")
    static let showSidebar = String(localized: "sidebar.empty.showSidebar", defaultValue: "Show sidebar")
    static let noMachines = String(localized: "sidebar.empty.noMachines", defaultValue: "No machines")
    static let addMachine = String(localized: "sidebar.empty.addMachine", defaultValue: "Add a machine")
}

#if DEBUG
#Preview("Empty detail") {
    EmptyDetail { _ in }
}
#endif
