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

/// In book posture the column keeps to one page, the wider one, so no line of text or control
/// crosses the fold.
private struct ScreenMargins: ViewModifier {
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.fold) private var fold
    @Environment(\.foldOrigin) private var anchor
    /// Where the screen sits across the window, which the fold is measured against.
    @State private var span = Span()

    func body(content: Content) -> some View {
        let page = sizeClass == .regular ? fold.readingPage(from: anchor, minX: span.minX, width: span.width) : nil
        content
            .frame(maxWidth: sizeClass == .regular ? Layout.readableWidth : .infinity, alignment: .leading)
            .padding(.horizontal, Layout.margin)
            .frame(maxWidth: page.map { $0.upperBound - $0.lowerBound } ?? .infinity)
            .padding(.leading, page?.lowerBound ?? 0)
            .frame(maxWidth: .infinity, alignment: page == nil ? .center : .leading)
            // Only the horizontal span, so scrolling does not measure again.
            .onGeometryChange(for: Span.self) { proxy in
                let frame = proxy.frame(in: .global)
                return Span(minX: frame.minX, width: frame.width)
            } action: { new in
                // The first measure places the column; later ones, such as a fold, move it.
                var transaction = Transaction()
                transaction.disablesAnimations = span.width == 0
                withTransaction(transaction) { span = new }
            }
            .motion(.paneFlow, value: page)
    }

    private struct Span: Equatable {
        var minX: CGFloat = 0
        var width: CGFloat = 0
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
