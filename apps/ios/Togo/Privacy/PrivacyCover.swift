import SwiftUI

/// Hides content from the app switcher snapshot. Opaque ink, never a blur: a blur leaks
/// the shape of what was on screen, and glass on content is banned.
private struct PrivacyCover: ViewModifier {
    @Environment(\.scenePhase) private var scenePhase
    @State private var wasActive = false

    func body(content: Content) -> some View {
        content
            .overlay {
                if PrivacyCoverRule.covers(scenePhase, wasActive: wasActive) {
                    PrivacySurface()
                }
            }
            .onChange(of: scenePhase, initial: true) { _, phase in
                if phase == .active { wasActive = true }
            }
    }
}

/// When the cover is up. A scene is inactive for a moment at every cold launch, before it
/// first turns active; covering it then would hide the launch, so the cover waits until the
/// scene has been active once. The switcher snapshot is taken on the way to the background,
/// which is always covered.
enum PrivacyCoverRule {
    static func covers(_ phase: ScenePhase, wasActive: Bool) -> Bool {
        switch phase {
        case .active: false
        case .inactive: wasActive
        case .background: true
        @unknown default: true
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
