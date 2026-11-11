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
            // Secondary values draw in the secondary level: the system gray is 3.3:1 on light
            // ink, mute is 5.5:1 and follows Increase Contrast. Section footers do not take the
            // level (they draw in the primary one); each is a `SectionFooter`.
            .foregroundStyle(Color.primary, Color.togoMute)
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
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.fold) private var fold
    @Environment(\.foldOrigin) private var anchor
    /// Where the screen sits across the window, which the fold is measured against.
    @State private var span = Span()

    func body(content: Content) -> some View {
        // width-ok: the reading page comes from the fold, which only a regular Duo has; the measured span decides its width.
        let page = sizeClass == .regular ? fold.readingPage(from: anchor, minX: span.minX, width: span.width) : nil
        content
            // width-ok: a maximum, not a layout choice: a narrower window is simply narrower than it.
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
                // The first measure places the column; later ones, such as a fold, move it on
                // `paneFlow` in this one explicit transaction. The measure is of the full-width
                // frame outside the column, so moving the column never measures again.
                var transaction = Transaction()
                if span.width == 0 {
                    transaction.disablesAnimations = true
                } else {
                    transaction.animation = Motion.animation(.paneFlow, reduce: reduceMotion)
                }
                withTransaction(transaction) { span = new }
            }
    }

    private struct Span: Equatable {
        var minX: CGFloat = 0
        var width: CGFloat = 0
    }
}

extension View {
    /// A toggle in a menu, drawn as the menu draws one: a checkmark. The app's switch style
    /// (`togoControls`) has no menu form, and a menu shows a toggle it cannot draw as a
    /// disabled item that does nothing.
    func menuToggle() -> some View {
        toggleStyle(.automatic)
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

/// A list section's footer in mute. A footer inside `groupedList()` draws in the list's primary
/// level, not its secondary one, so it would read as body text; mute is 5.5:1 on light ink, where
/// the system gray is 3.3:1, and follows Increase Contrast.
struct SectionFooter<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        content.foregroundStyle(Color.togoMute)
    }
}
