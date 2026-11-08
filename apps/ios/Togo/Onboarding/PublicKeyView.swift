import SwiftUI
import TogoKit
import UIKit

/// One line to paste in a terminal on the machine: it makes `~/.ssh` if needed, appends the key
/// on its own line and sets the permissions sshd insists on (it ignores a key file others can
/// write, without saying why).
enum PublicKeyCommand {
    static func append(_ publicKey: String) -> String {
        let key = publicKey.trimmingCharacters(in: .whitespacesAndNewlines)
        return "mkdir -p ~/.ssh && chmod 700 ~/.ssh && printf '%s\\n' \(shellQuote([key])) >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"
    }
}

/// Shown when the machine refused this phone's key: the public key to add, and a way to try
/// again once it is in place.
struct PublicKeyView: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let machine: String
    let publicKey: String
    let onCheckAgain: () -> Void

    @State private var copied = false
    @State private var copiedCommand = false

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
            // The whole step as one command, for a Mac with no ~/.ssh yet.
            VStack(alignment: .leading, spacing: 8) {
                Text(String(
                    localized: "onboard.publicKey.command",
                    defaultValue: "Or run this in Terminal on \(machine). It adds the key with the right permissions:"
                ))
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                CopyableText(text: PublicKeyCommand.append(publicKey))
                Button {
                    UIPasteboard.general.string = PublicKeyCommand.append(publicKey)
                    copiedCommand = true
                } label: {
                    Label(
                        copiedCommand
                            ? String(localized: "onboard.copied", defaultValue: "Copied")
                            : String(localized: "onboard.copyCommand", defaultValue: "Copy command"),
                        systemImage: copiedCommand ? "checkmark" : "doc.on.doc"
                    )
                    .contentTransition(.identity)
                }
                .buttonStyle(.secondary(.fit))
            }
        } actions: {
            Button(String(localized: "onboard.checkAgain", defaultValue: "Check again"), action: onCheckAgain)
                .buttonStyle(.primary)
        }
        .task(id: copied) {
            guard copied else { return }
            try? await Task.sleep(for: .seconds(2))
            copied = false
        }
        .task(id: copiedCommand) {
            guard copiedCommand else { return }
            try? await Task.sleep(for: .seconds(2))
            copiedCommand = false
        }
    }
}
