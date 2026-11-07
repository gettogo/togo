import SwiftUI

extension View {
    /// In UI tests (`-ui-probes`), an invisible element `presentation.probe` whose value is the
    /// coordinator's summary, so a flow asserts on what is presented, never on pixels. Every
    /// presented surface carries one too, since the frontmost modal hides the window's.
    func presentationProbe() -> some View {
        modifier(PresentationProbe())
    }
}

private struct PresentationProbe: ViewModifier {
    @Environment(\.presentationCoordinator) private var coordinator

    func body(content: Content) -> some View {
        #if DEBUG
        if UITestLaunch.current.probes, let coordinator {
            content.overlay(alignment: .topLeading) {
                Color.clear
                    .frame(width: 1, height: 1)
                    .allowsHitTesting(false)
                    .accessibilityElement()
                    .accessibilityLabel(Text(verbatim: "presentation"))
                    .accessibilityValue(Text(verbatim: coordinator.summary))
                    .accessibilityIdentifier("presentation.probe")
            }
        } else {
            content
        }
        #else
        content
        #endif
    }
}
