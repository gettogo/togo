import SwiftUI

/// The detail beside the sidebar when no session is on screen.
struct EmptyDetail: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    /// Nil while there is no machine to start a session on.
    var newSession: (() -> Void)?

    var body: some View {
        VStack(spacing: 14) {
            Image(systemName: "terminal")
                .font(.system(.largeTitle).weight(.light))
                .foregroundStyle(Color.togoMute)
                .accessibilityHidden(true)
            Text(EmptyDetailCopy.title)
                .font(.title3.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .multilineTextAlignment(.center)
            if let newSession {
                Button(action: newSession) {
                    HStack(spacing: 8) {
                        Text(EmptyDetailCopy.newSession)
                        // The shortcut, named where the action is.
                        Text(verbatim: "⌘T")
                            .foregroundStyle(Color.togoInk.opacity(0.6))
                            .accessibilityHidden(true)
                    }
                }
                .buttonStyle(.primary(.fit))
                .padding(.top, 8)
                .hoverEffect(.highlight)
                .accessibilityHint(EmptyDetailCopy.shortcutHint)
            }
        }
        .frame(maxWidth: dynamicTypeSize.isAccessibilitySize ? .infinity : 340)
        .padding(.horizontal, Layout.margin)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.togoInk)
    }
}

enum EmptyDetailCopy {
    static let title = String(localized: "sidebar.empty.title", defaultValue: "Pick a session, or start one.")
    static let newSession = String(localized: "sidebar.empty.newSession", defaultValue: "New Session")
    static let shortcutHint = String(localized: "sidebar.empty.shortcut", defaultValue: "Command T")
}

#if DEBUG
#Preview("Empty detail") {
    EmptyDetail(newSession: {})
}
#endif
