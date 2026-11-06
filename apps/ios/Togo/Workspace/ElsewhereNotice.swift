import SwiftUI

/// Where a tab's terminal would be while another window shows it. A terminal is on screen in
/// one place at a time; "Show here" brings it to this window.
struct ElsewhereNotice: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    private let onShowHere: () -> Void

    init(onShowHere: @escaping () -> Void) {
        self.onShowHere = onShowHere
    }

    var body: some View {
        VStack(spacing: 14) {
            VStack(spacing: 14) {
                Image(systemName: "macwindow.on.rectangle")
                    .font(.system(.largeTitle).weight(.light))
                    .foregroundStyle(Color.togoMute)
                    .accessibilityHidden(true)
                Text(WindowCopy.elsewhere)
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                    .multilineTextAlignment(.center)
            }
            .accessibilityElement(children: .combine)
            Button(WindowCopy.showHere, action: onShowHere)
                .buttonStyle(.primary(.fit))
                .padding(.top, 8)
        }
        .frame(maxWidth: dynamicTypeSize.isAccessibilitySize ? .infinity : 340)
        .padding(.horizontal, Layout.margin)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.togoInk)
    }
}

/// Opens the tab in a window of its own. Absent where the app has one window, such as on
/// iPhone; when the system turns the request down, nothing has changed.
struct OpenInWindowButton: View {
    @Environment(\.supportsMultipleWindows) private var supportsMultipleWindows
    @Environment(\.openWindow) private var openWindow
    private let tab: TabID

    init(tab: TabID) {
        self.tab = tab
    }

    var body: some View {
        if supportsMultipleWindows {
            Button(WindowCopy.openInNewWindow, systemImage: "macwindow.badge.plus") {
                openWindow(value: tab)
            }
        }
    }
}

enum WindowCopy {
    static let elsewhere = String(localized: "window.elsewhere", defaultValue: "Showing in another window")
    static let showHere = String(localized: "window.showHere", defaultValue: "Show here")
    static let openInNewWindow = String(localized: "window.openInNewWindow", defaultValue: "Open in New Window")
}

#if DEBUG
#Preview("Elsewhere") {
    ElsewhereNotice {}
}
#endif
