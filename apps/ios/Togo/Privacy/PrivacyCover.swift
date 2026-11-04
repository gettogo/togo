import SwiftUI

/// Hides content from the app switcher snapshot. Opaque ink, never a blur: a blur leaks
/// the shape of what was on screen, and glass on content is banned.
private struct PrivacyCover: ViewModifier {
    @Environment(\.scenePhase) private var scenePhase

    func body(content: Content) -> some View {
        content.overlay {
            if scenePhase != .active {
                PrivacySurface()
            }
        }
    }
}

struct PrivacySurface: View {
    var body: some View {
        ZStack {
            Color.togoInk
            Text(verbatim: "togo")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.togoMute)
                .accessibilityHidden(true)
        }
        .ignoresSafeArea()
    }
}

struct ResumeLockScreen: View {
    let unlock: () -> Void

    var body: some View {
        ZStack {
            PrivacySurface()
            Button("Unlock with \(BiometryName.current)", action: unlock)
                .foregroundStyle(Color.togoSnow)
                .padding(.top, 80)
        }
    }
}

extension View {
    func privacyCover() -> some View {
        modifier(PrivacyCover())
    }
}
