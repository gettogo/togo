import SwiftUI
import UIKit
import UniformTypeIdentifiers

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

/// Sign-in codes and links on the clipboard: this device only (never Universal Clipboard to
/// the Mac or iPad) and gone after two minutes, so they do not sit there for other apps.
enum SensitivePasteboard {
    static let lifetime: TimeInterval = 120

    static func options(now: Date = Date()) -> [UIPasteboard.OptionsKey: Any] {
        [.localOnly: true, .expirationDate: now.addingTimeInterval(lifetime)]
    }

    static func copy(_ text: String, to pasteboard: UIPasteboard = .general) {
        pasteboard.setItems([[UTType.plainText.identifier: text]], options: options())
    }

    static func copy(_ url: URL, to pasteboard: UIPasteboard = .general) {
        pasteboard.setItems([[UTType.url.identifier: url]], options: options())
    }
}
