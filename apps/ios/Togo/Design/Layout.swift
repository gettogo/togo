import SwiftUI

/// Spacing shared by every screen, so a new screen lines up with the others without picking
/// its own numbers.
enum Layout {
    /// Side margin for screens that are not lists. Lists keep the system's insets.
    static let margin: CGFloat = 20
    /// Widest a column of text gets in a regular-width window, close to UIKit's readable width.
    static let readableWidth: CGFloat = 672
}

extension View {
    /// The side margin for a non-list screen, and a readable column in a wide window.
    func screenMargins() -> some View {
        modifier(ScreenMargins())
    }

    /// An inset-grouped list on the app background, with rows on grouped cards.
    func groupedList() -> some View {
        listStyle(.insetGrouped)
            .scrollContentBackground(.hidden)
            .background(Color.togoInk)
            .listRowSeparatorTint(Color.togoLine)
    }

    /// Puts a section's rows on the grouped card surface.
    func groupedRows() -> some View {
        listRowBackground(Color.togoGrouped)
            .listRowSeparatorTint(Color.togoLine)
    }

    /// The app's tint for bars and plain controls, with switches left on the system color: a
    /// snow track under a white knob reads as off.
    func togoControls() -> some View {
        tint(Color.togoSnow)
            .toggleStyle(.systemSwitch)
    }
}

private struct ScreenMargins: ViewModifier {
    @Environment(\.horizontalSizeClass) private var sizeClass

    func body(content: Content) -> some View {
        content
            .frame(maxWidth: sizeClass == .regular ? Layout.readableWidth : .infinity, alignment: .leading)
            .padding(.horizontal, Layout.margin)
            .frame(maxWidth: .infinity)
    }
}

struct SystemSwitchToggleStyle: ToggleStyle {
    func makeBody(configuration: Configuration) -> some View {
        Toggle(configuration)
            .toggleStyle(.switch)
            .tint(Color(uiColor: .systemGreen))
    }
}

extension ToggleStyle where Self == SystemSwitchToggleStyle {
    static var systemSwitch: Self { Self() }
}
