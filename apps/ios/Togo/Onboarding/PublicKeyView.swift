import SwiftUI
import UIKit

/// Shown when the machine refused this phone's key: the public key to add, and a way to try
/// again once it is in place.
struct PublicKeyView: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let machine: String
    let publicKey: String
    let onCheckAgain: () -> Void

    @State private var copied = false

    var body: some View {
        OnboardingStep(
            title: String(localized: "onboard.publicKey.title", defaultValue: "Add this device's key"),
            message: String(
                localized: "onboard.publicKey.message",
                defaultValue: "Add this key to ~/.ssh/authorized_keys on \(machine)"
            )
        ) {
            CopyableText(text: publicKey)
            let layout = dynamicTypeSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 0)) : AnyLayout(HStackLayout(spacing: 24))
            layout {
                Button {
                    UIPasteboard.general.string = publicKey
                    copied = true
                } label: {
                    Label(
                        copied
                            ? String(localized: "onboard.copied", defaultValue: "Copied")
                            : String(localized: "onboard.copy", defaultValue: "Copy"),
                        systemImage: copied ? "checkmark" : "doc.on.doc"
                    )
                    .contentTransition(.identity)
                }
                .buttonStyle(.secondary(.fit))
                ShareLink(item: publicKey) {
                    Label(String(localized: "onboard.share", defaultValue: "Share"), systemImage: "square.and.arrow.up")
                }
                .buttonStyle(.secondary(.fit))
            }
            Text(String(
                localized: "onboard.publicKey.hint",
                defaultValue: "Only the public half leaves the device. Send it to yourself, then paste it on its own line."
            ))
            .font(.footnote)
            .foregroundStyle(Color.togoMute)
        } actions: {
            Button(String(localized: "onboard.checkAgain", defaultValue: "Check again"), action: onCheckAgain)
                .buttonStyle(.primary)
        }
        .task(id: copied) {
            guard copied else { return }
            try? await Task.sleep(for: .seconds(2))
            copied = false
        }
    }
}
