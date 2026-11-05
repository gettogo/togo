import SwiftUI

/// The host key check on a first connection, and the warning when a trusted key changed.
/// A changed key is never accepted here: the only way on is to forget the old key, after
/// which the new one goes through the same first-connection check.
struct HostKeyCheckView: View {
    enum Kind: Equatable {
        case new(HostKeyPrompt)
        case changed(ChangedHostKey)
    }

    let machine: String
    let kind: Kind
    let onTrust: () -> Void
    let onCancel: () -> Void
    let onForget: () -> Void

    @State private var confirmingForget = false

    var body: some View {
        switch kind {
        case .new(let prompt): newKey(prompt)
        case .changed(let change): changedKey(change)
        }
    }

    private func newKey(_ prompt: HostKeyPrompt) -> some View {
        OnboardingStep(
            title: String(localized: "onboard.hostKey.new.title", defaultValue: "Check this machine's key"),
            message: String(
                localized: "onboard.hostKey.new.message",
                defaultValue: "This is the first time Togo connects to \(machine). Compare this fingerprint with the one the machine shows, then trust it."
            )
        ) {
            fingerprint(Self.keyName(prompt.keyType), prompt.fingerprint)
            VStack(alignment: .leading, spacing: 8) {
                Text(String(localized: "onboard.hostKey.new.howTo", defaultValue: "To see it on the machine, run:"))
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                CopyableText(text: "ssh-keygen -lf " + Self.publicKeyPath(prompt.keyType))
            }
        } actions: {
            Button(String(localized: "onboard.hostKey.trust", defaultValue: "Trust this machine"), action: onTrust)
                .buttonStyle(.primary)
            Button(String(localized: "onboard.cancel", defaultValue: "Cancel"), action: onCancel)
                .buttonStyle(.secondary)
        }
    }

    private func changedKey(_ change: ChangedHostKey) -> some View {
        OnboardingStep(
            title: String(localized: "onboard.hostKey.changed.title", defaultValue: "This machine's key changed"),
            message: String(
                localized: "onboard.hostKey.changed.message",
                defaultValue: "The key \(machine) sent is not the one you trusted. If nobody reinstalled or changed this machine, someone may be intercepting the connection. Togo will not connect."
            )
        ) {
            fingerprint(String(localized: "onboard.hostKey.trusted", defaultValue: "Trusted before"), change.trusted.joined(separator: "\n"))
            fingerprint(String(localized: "onboard.hostKey.presented", defaultValue: "Sent now"), change.presented)
        } actions: {
            Button(String(localized: "onboard.cancel", defaultValue: "Cancel"), action: onCancel)
                .buttonStyle(.primary)
                .keyboardShortcut(.defaultAction)
            Button(String(localized: "onboard.hostKey.forget", defaultValue: "Forget the old key")) { confirmingForget = true }
                .buttonStyle(.secondary)
                .confirmationDialog(
                    String(localized: "onboard.hostKey.forget.confirm", defaultValue: "Forget the old key for \(machine)?"),
                    isPresented: $confirmingForget, titleVisibility: .visible
                ) {
                    Button(String(localized: "onboard.hostKey.forget", defaultValue: "Forget the old key"), role: .destructive, action: onForget)
                } message: {
                    Text(String(
                        localized: "onboard.hostKey.forget.detail",
                        defaultValue: "Do this only if you know why the key changed. Togo then shows the new key for you to check."
                    ))
                }
        }
    }

    private func fingerprint(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
            CopyableText(text: value)
        }
        .accessibilityElement(children: .combine)
    }

    /// "ssh-ed25519" reads as "ED25519 key".
    private static func keyName(_ keyType: String) -> String {
        let bare = keyType.replacingOccurrences(of: "ssh-", with: "").replacingOccurrences(of: "-sha2-nistp256", with: "")
        let name = bare.hasPrefix("ecdsa") ? "ECDSA" : bare.uppercased()
        return String(localized: "onboard.hostKey.keyName", defaultValue: "\(name) key")
    }

    private static func publicKeyPath(_ keyType: String) -> String {
        let file = keyType.contains("ed25519") ? "ed25519" : keyType.contains("ecdsa") ? "ecdsa" : "rsa"
        return "/etc/ssh/ssh_host_\(file)_key.pub"
    }
}
