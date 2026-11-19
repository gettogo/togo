import SwiftUI

/// Reports fixture and transcript failures to the UI test even if the app catches an RPC error.
struct StrictReplayProbe: ViewModifier {
    let composition: AppComposition

    func body(content: Content) -> some View {
        #if DEBUG
        if let verdict = composition.strictReplayVerdict {
            content.overlay(alignment: .topLeading) {
                Color.clear
                    .frame(width: 1, height: 1)
                    .allowsHitTesting(false)
                    .accessibilityElement()
                    .accessibilityLabel(Text(verbatim: "Strict replay"))
                    .accessibilityValue(Text(verbatim: verdict))
                    .accessibilityIdentifier("replay.strict")
            }
        } else {
            content
        }
        #else
        content
        #endif
    }
}
