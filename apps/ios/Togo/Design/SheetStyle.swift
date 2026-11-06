import SwiftUI

/// The three ways the app presents a sheet. Every `.sheet` picks one, so a sheet looks the same
/// on every screen: floating at a partial height on a phone, a centred card in a regular window.
enum SheetKind: Sendable {
    /// A flow or form: New session, Add or Edit machine, Import hosts, Commit.
    case form
    /// One short question: a sign-in prompt, a comment. Sized to its content.
    case prompt
    /// Reading material that wants the room: Settings beside the sidebar.
    case page
}

enum SheetStyle {
    /// Where a form sheet opens on a phone: tall, with margins on every side so it floats.
    static let formStart = PresentationDetent.fraction(0.8)
    /// A prompt's height until its content has been measured.
    static let promptFallback: CGFloat = 340
}

extension View {
    /// Presentation for sheet content. Apply it inside the `.sheet` closure. `detent` lets the
    /// content move a form sheet between its heights, as the folder browser does.
    func togoSheet(_ kind: SheetKind, detent: Binding<PresentationDetent>? = nil) -> some View {
        modifier(TogoSheet(kind: kind, detent: detent))
    }
}

private struct TogoSheet: ViewModifier {
    let kind: SheetKind
    let detent: Binding<PresentationDetent>?
    @State private var ownDetent = SheetStyle.formStart
    @State private var measured: CGFloat?

    func body(content: Content) -> some View {
        switch kind {
        case .form:
            // Detents shape the sheet in a compact window; the form sizing gives the centred
            // card in a regular one, where the system ignores detents.
            content
                .presentationDetents([SheetStyle.formStart, .large], selection: detent ?? $ownDetent)
                .presentationSizing(.form)
        case .prompt:
            content
                // The question's own height, bars included, read from the scroll view inside.
                .onScrollGeometryChange(for: CGFloat.self) { geometry in
                    geometry.contentSize.height + geometry.contentInsets.top + geometry.contentInsets.bottom
                } action: { _, height in
                    measured = height.rounded(.up)
                }
                .presentationDetents([.height(measured ?? SheetStyle.promptFallback)])
                .presentationSizing(.form.fitted(horizontal: false, vertical: true))
        case .page:
            content
                .presentationDetents([.large])
                .presentationSizing(.page)
        }
    }
}
