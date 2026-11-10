import SwiftUI

extension View {
    /// Presents `item` the way `AdaptivePresentation` says for `content` and the room the
    /// window has: a popover, a sheet of the right size, a full-screen cover or an inspector. The
    /// one way the harness screens, the command palette, mission control's screens and the guide
    /// are presented.
    ///
    /// `anchored`: this view is the control the user tapped, so a popover can point at it.
    /// `hostsInspector`: an inspector may share the window with this view.
    func harnessPresent<Item: Identifiable, Shown: View>(
        item: Binding<Item?>, as content: PresentationContent, anchored: Bool = false, hostsInspector: Bool = false,
        name: String? = nil, onDismiss: (() -> Void)? = nil, @ViewBuilder body: @escaping (Item) -> Shown
    ) -> some View {
        modifier(AdaptivePresenter(
            item: item, content: content, anchored: anchored, hostsInspector: hostsInspector,
            onDismiss: onDismiss, name: name ?? "harness.\(content.rawValue)", make: body
        ))
    }

    /// `harnessPresent(item:)` for content without a value.
    func harnessPresent<Shown: View>(
        isPresented: Binding<Bool>, as content: PresentationContent, anchored: Bool = false, hostsInspector: Bool = false,
        name: String? = nil, onDismiss: (() -> Void)? = nil, @ViewBuilder body: @escaping () -> Shown
    ) -> some View {
        let item = Binding<PresentedFlag?>(
            get: { isPresented.wrappedValue ? PresentedFlag() : nil },
            set: { isPresented.wrappedValue = $0 != nil }
        )
        return harnessPresent(
            item: item, as: content, anchored: anchored, hostsInspector: hostsInspector, name: name, onDismiss: onDismiss
        ) { _ in body() }
    }

    /// The sheet chrome for `style`: detents and sizing only, never a painted background, so the
    /// system's glass and its opaque full-height state stay (`L5`, `S9`).
    func presentationChrome(_ style: PresentationStyle) -> some View {
        modifier(PresentationChrome(style: style))
    }
}

/// The one value an `isPresented` presentation carries.
struct PresentedFlag: Identifiable, Hashable {
    var id: Int { 0 }
}

private struct AdaptivePresenter<Item: Identifiable, Presented: View>: ViewModifier {
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.windowWidth) private var windowWidth
    @Binding var item: Item?
    let content: PresentationContent
    let anchored: Bool
    let hostsInspector: Bool
    let onDismiss: (() -> Void)?
    let name: String
    let make: (Item) -> Presented

    /// The style chosen when it opened, kept until it closes (as the review keeps its own).
    @State private var opened: PresentationStyle?

    /// What shows: the style it opened with while it is up, else the style now. A fold or an
    /// unfold under an open presentation never hands it to another kind (a sheet dismissed and
    /// a popover or cover presented in the same update) and never changes a live sheet's
    /// detents or sizing; the system adapts the one it has (a popover becomes a sheet when
    /// compact, an inspector becomes a sheet, a page sheet fills a compact width).
    private var style: PresentationStyle { opened ?? liveStyle }

    /// The style now. A detail with no stack to push into (it is the root of what opens) is
    /// shown as the small form it then is.
    private var liveStyle: PresentationStyle {
        let chosen = AdaptivePresentation.style(
            for: content, sizeClass: sizeClass, containerWidth: windowWidth,
            anchored: anchored, hostsInspector: hostsInspector
        )
        guard chosen == .push || chosen == .menu || chosen == .confirmationDialog else { return chosen }
        return AdaptivePresentation.style(
            for: .smallForm, sizeClass: sizeClass, containerWidth: windowWidth, anchored: anchored
        )
    }

    func body(content view: Content) -> some View {
        let style = self.style
        // The coordinator owns when it shows; the policy says as what.
        CoordinatedSlot(
            view, name: name, kind: PresentationCoordinator.kind(of: style), yields: content == .sideContext,
            wanted: item != nil, release: { if item != nil { item = nil } }
        ) { view, shown, closed, inside in
            presentations(view, style: style, shown: shown, closed: closed, inside: inside)
        }
        .onChange(of: item != nil, initial: true) { _, open in
            opened = AdaptivePresentation.kept(opened, live: liveStyle, open: open)
        }
    }

    @ViewBuilder private func presentations(
        _ view: some View, style: PresentationStyle, shown: Bool, closed: @escaping () -> Void, inside: PresentedContent
    ) -> some View {
        let presented = view
            .popover(item: gated(style, shown: shown, closed: closed) { $0 == .popover }, arrowEdge: .bottom) { item in
                inside(make(item)
                    .frame(minWidth: 360, idealWidth: 420, minHeight: 420, idealHeight: 560)
                    // A compact width the popover lands in after all (a fold) gets a sheet: the
                    // system adapts the same presentation, nothing is handed over.
                    .presentationCompactAdaptation(.sheet))
            }
            .sheet(item: gated(style, shown: shown, closed: closed, \.isSheet), onDismiss: onDismiss) { item in
                inside(make(item).presentationChrome(style))
            }
            .fullScreenCover(item: gated(style, shown: shown, closed: closed) { $0 == .fullScreenCover }, onDismiss: onDismiss) { item in
                inside(make(item))
            }
        // Only a host beside main content gets the inspector column at all.
        if hostsInspector {
            presented.inspector(isPresented: Binding(
                get: { style == .inspector && shown },
                set: { isShown in if !isShown, style == .inspector { closed() } }
            )) {
                if let item { inside(make(item)) }
            }
        } else {
            presented
        }
    }

    /// The item, for the presentation `matches` picks. Only a close from that presentation
    /// clears it; the style is kept while it is up, so no other one takes it over.
    private func gated(
        _ style: PresentationStyle, shown: Bool, closed: @escaping () -> Void,
        _ matches: @escaping (PresentationStyle) -> Bool
    ) -> Binding<Item?> {
        Binding(
            get: { matches(style) && shown ? item : nil },
            set: { value in
                guard matches(style), value == nil else { return }
                closed()
            }
        )
    }
}

private struct PresentationChrome: ViewModifier {
    let style: PresentationStyle

    func body(content: Content) -> some View {
        switch style {
        case .mediumSheet:
            content
                .presentationDetents([.medium, .large])
                .presentationDragIndicator(.visible)
                .presentationContentInteraction(.scrolls)
        case .formSheet:
            content
                .presentationDetents([.large])
                .presentationSizing(.form)
        case .pageSheet:
            // Page-sized, so the sheet keeps a regular width and a split view inside shows its
            // columns; a form sheet would be compact inside.
            content
                .presentationDetents([.large])
                .presentationSizing(.page)
        case .fullHeightSheet:
            content
                .presentationDetents([.large])
                .presentationDragIndicator(.visible)
        default:
            content
        }
    }
}
