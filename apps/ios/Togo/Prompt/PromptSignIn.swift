import SwiftUI
import TogoKit
import TogoTerminal
import UIKit

/// What a sign-in request offers, worked out once so the bar and its tests agree.
struct SignInOffer: Equatable {
    /// The device code, shown large with Copy.
    var code: String?
    /// The page to open, in the system's authentication browser.
    var page: URL?
    /// The agent waits for a pasted key or code, typed into a masked field.
    var secret: Bool

    init?(_ step: SignInStep?) {
        guard let step else { return nil }
        code = step.code
        page = step.url.flatMap { LinkPolicy.allows($0) ? $0 : nil }
        secret = step.secret
        if code == nil, page == nil, !secret { return nil }
    }

    /// Opening the page copies the code first, so it is on the clipboard to paste there.
    var copiesOnOpen: Bool { code != nil && page != nil }

    /// What the reply button says: the field that opens is for a secret.
    var replyLabel: String { SignInCopy.pasteKey }
}

/// The sign-in part of a request in the bar: the code to copy and the page to open. The
/// secret itself goes through the reply line, which turns into a masked field.
struct PromptSignInPanel: View {
    let offer: SignInOffer
    let hostID: HostID?
    let sessionID: String
    @State private var copied = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let code = offer.code {
                HStack(spacing: 10) {
                    Text(verbatim: code)
                        .font(.system(.title3, design: .monospaced).weight(.semibold))
                        .foregroundStyle(Color.togoSnow)
                        .textSelection(.enabled)
                        .fixedSize()
                        .accessibilityLabel(SignInCopy.codeLabel(code))
                    Button(copied > 0 ? SignInCopy.copied : SignInCopy.copyCode) { copy(code) }
                        .buttonStyle(.promptCapsule(.plain))
                        .contentTransition(.interpolate)
                        // motion: cause → effect — the tapped Copy says Copied in place
                        .motion(.respond, value: copied)
                }
            }
            if let page = offer.page {
                Button(SignInCopy.open(host: page.host(percentEncoded: false) ?? "")) { open(page) }
                    .buttonStyle(.promptCapsule(offer.secret ? .plain : .beacon))
                    .accessibilityHint(offer.copiesOnOpen ? SignInCopy.copiesHint : "")
            }
        }
        .haptic(.done, trigger: copied)
    }

    private func copy(_ code: String) {
        UIPasteboard.general.string = code
        copied += 1
    }

    private func open(_ page: URL) {
        if offer.copiesOnOpen, let code = offer.code { copy(code) }
        LinkCenter.shared.signIn(page, from: .init(hostID: hostID, sessionID: sessionID.isEmpty ? nil : sessionID))
    }
}

/// The reply line's field for a secret: masked, no autocorrection, nothing suggested or kept.
struct PromptSecretField: View {
    @Binding var text: String
    let placeholder: String

    var body: some View {
        SecureField(text: $text, prompt: Text(placeholder).foregroundStyle(Color.togoMute)) {
            Text(placeholder)
        }
        .textContentType(.password)
        .autocorrectionDisabled()
        .textInputAutocapitalization(.never)
        .privacySensitive()
    }
}

enum SignInCopy {
    static let copyCode = String(localized: "signIn.copyCode", defaultValue: "Copy code")
    static let copied = String(localized: "signIn.copied", defaultValue: "Copied")
    static let pasteKey = String(localized: "signIn.pasteKey", defaultValue: "Paste key or code")
    static let secretPlaceholder = String(localized: "signIn.secretPlaceholder", defaultValue: "Key or code")
    static let copiesHint = String(localized: "signIn.copiesHint", defaultValue: "Copies the code first.")

    static func open(host: String) -> String {
        host.isEmpty
            ? String(localized: "signIn.openPage", defaultValue: "Open sign-in page")
            : String(localized: "signIn.openHost", defaultValue: "Open \(host)")
    }

    static func codeLabel(_ code: String) -> String {
        let spelled = code.map(String.init).joined(separator: " ")
        return String(localized: "signIn.codeLabel", defaultValue: "Code \(spelled)")
    }
}
