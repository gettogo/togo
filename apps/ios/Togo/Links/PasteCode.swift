import SwiftUI
import TogoKit
import UIKit

/// How a pasted sign-in code went.
enum CodeSendResult: Equatable {
    case sent
    /// Typed, but the agent's input still holds it: Enter did not take.
    case notSubmitted
    case failed(String)
}

/// Types a sign-in code into the agent waiting for it on the machine.
@MainActor protocol SignInCodeSending: AnyObject {
    func send(code: String, hostID: HostID, sessionID: String) async -> CodeSendResult
}

/// The "Paste code" field's state. Pure apart from the sender.
@MainActor @Observable final class PasteCodeModel {
    enum Phase: Equatable {
        case editing
        case sending
        case sent
        case failed(String)
    }

    var text = ""
    private(set) var phase: Phase = .editing
    let hostID: HostID
    let sessionID: String
    @ObservationIgnored var onSent: (@MainActor () -> Void)?
    @ObservationIgnored private let sender: any SignInCodeSending

    init(hostID: HostID, sessionID: String, sender: any SignInCodeSending) {
        self.hostID = hostID
        self.sessionID = sessionID
        self.sender = sender
    }

    var canSend: Bool { Self.clean(text) != nil && phase != .sending && phase != .sent }

    func send() async {
        guard let code = Self.clean(text), phase != .sending else { return }
        phase = .sending
        switch await sender.send(code: code, hostID: hostID, sessionID: sessionID) {
        case .sent:
            phase = .sent
            text = ""
            onSent?()
        case .notSubmitted:
            phase = .failed(PasteCodeCopy.notSubmitted)
        case .failed(let message):
            phase = .failed(message.isEmpty ? PasteCodeCopy.failed : message)
        }
    }

    /// The code as it goes to the agent: one line, trimmed, without control characters, at
    /// most 4096 characters. Nil when nothing is left. A copied code often brings a newline or
    /// spaces along; those go.
    nonisolated static func clean(_ raw: String) -> String? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed.count <= 4096,
              !trimmed.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains) else { return nil }
        return trimmed
    }
}

/// The provider's code, pasted back into the agent: a focused field, Paste when the clipboard
/// holds text, and Send.
struct PasteCodeField: View {
    @Bindable var model: PasteCodeModel
    @FocusState private var focused: Bool
    @State private var clipboardHasText = UIPasteboard.general.hasStrings

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(PasteCodeCopy.title)
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
            Text(PasteCodeCopy.detail)
                .font(.footnote)
                .foregroundStyle(Color.togoMute)
                .fixedSize(horizontal: false, vertical: true)
            TextField(text: $model.text, prompt: Text(PasteCodeCopy.placeholder).foregroundStyle(Color.togoMute)) {
                Text(PasteCodeCopy.placeholder)
            }
            .font(.system(.body, design: .monospaced))
            .foregroundStyle(Color.togoSnow)
            .textContentType(.oneTimeCode)
            .autocorrectionDisabled()
            .textInputAutocapitalization(.never)
            .privacySensitive()
            .submitLabel(.send)
            // The page is still being pushed when it appears: focus lands a turn later, so the
            // keyboard comes up without a tap.
            .focusOnReveal($focused, revealed: model.phase != .sent)
            .onSubmit { Task { await model.send() } }
            .frame(minHeight: 44)
            if case .failed(let message) = model.phase {
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if model.phase == .sent {
                Text(PasteCodeCopy.sent)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .fixedSize(horizontal: false, vertical: true)
            }
            HStack(spacing: 8) {
                if clipboardHasText, model.text.isEmpty {
                    // The system's Paste button reads the clipboard without iOS asking first.
                    PasteButton(payloadType: String.self) { strings in
                        guard let first = strings.first else { return }
                        Task { @MainActor in
                            model.text = PasteCodeModel.clean(first) ?? first
                            await model.send()
                        }
                    }
                    .labelStyle(.titleAndIcon)
                    .buttonBorderShape(.capsule)
                }
                Button(model.phase == .sending ? PasteCodeCopy.sending : PasteCodeCopy.send) {
                    Task { await model.send() }
                }
                .buttonStyle(.primary)
                .disabled(!model.canSend)
            }
        }
        .padding(.vertical, 8)
        .haptic(.done, trigger: model.phase, when: { $0 == .sent })
        .onReceive(NotificationCenter.default.publisher(for: UIPasteboard.changedNotification)) { _ in
            clipboardHasText = UIPasteboard.general.hasStrings
        }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didBecomeActiveNotification)) { _ in
            // Back from copying the code in the browser or another app.
            clipboardHasText = UIPasteboard.general.hasStrings
        }
    }
}

enum PasteCodeCopy {
    static let title = String(localized: "pasteCode.title", defaultValue: "Paste code")
    static let detail = String(
        localized: "pasteCode.detail",
        defaultValue: "If the sign-in page shows a code, copy it and paste it here. It goes straight to the agent and is not kept."
    )
    static let placeholder = String(localized: "pasteCode.placeholder", defaultValue: "Code")
    static let send = String(localized: "pasteCode.send", defaultValue: "Send code")
    static let sending = String(localized: "pasteCode.sending", defaultValue: "Sending…")
    static let sent = String(localized: "pasteCode.sent", defaultValue: "Sent. Waiting for the agent to confirm.")
    static let failed = String(localized: "pasteCode.failed", defaultValue: "The code didn't reach the agent. Try again.")
    static let notSubmitted = String(
        localized: "pasteCode.notSubmitted",
        defaultValue: "The code was typed but the agent didn't take it. Check the session, then try again."
    )
    static let noSession = String(
        localized: "pasteCode.noSession", defaultValue: "This sign-in isn't tied to a session, so there's nowhere to send a code."
    )
}
