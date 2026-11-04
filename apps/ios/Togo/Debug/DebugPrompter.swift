#if DEBUG
import TogoKit
import UIKit

/// Asks for passwords, codes and host key trust with system alerts.
struct DebugPrompter: ConnectionPrompter {
    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? {
        switch prompt {
        case .password(let user, let host):
            let values = await Self.ask(
                title: "Password for \(user)@\(host)", message: nil,
                fields: [KIPrompt(text: "Password", echo: false)]
            )
            return values.flatMap(\.first).map(AuthAnswer.secret)
        case .passphrase(let label):
            let values = await Self.ask(
                title: "Passphrase for \(label)", message: nil,
                fields: [KIPrompt(text: "Passphrase", echo: false)]
            )
            return values.flatMap(\.first).map(AuthAnswer.secret)
        case .keyboardInteractive(let name, let instruction, let prompts):
            let values = await Self.ask(
                title: name.isEmpty ? "Verification code" : name,
                message: instruction.isEmpty ? nil : instruction, fields: prompts
            )
            return values.map(AuthAnswer.responses)
        }
    }

    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool {
        await Self.confirm(
            title: "Trust \(host)?",
            message: "This is the first connection to this host. Its \(keyType) key is\n\(fingerprint)",
            action: "Trust"
        )
    }

    // MARK: Alerts

    @MainActor private static func ask(title: String, message: String?, fields: [KIPrompt]) async -> [String]? {
        await withCheckedContinuation { continuation in
            let alert = UIAlertController(title: title, message: message, preferredStyle: .alert)
            for field in fields {
                alert.addTextField { text in
                    text.placeholder = field.text
                    text.isSecureTextEntry = !field.echo
                    text.autocapitalizationType = .none
                    text.autocorrectionType = .no
                }
            }
            alert.addAction(UIAlertAction(title: "Cancel", style: .cancel) { _ in continuation.resume(returning: nil) })
            alert.addAction(UIAlertAction(title: "Continue", style: .default) { [weak alert] _ in
                continuation.resume(returning: alert?.textFields?.map { $0.text ?? "" } ?? [])
            })
            present(alert, orResume: { continuation.resume(returning: nil) })
        }
    }

    @MainActor private static func confirm(title: String, message: String, action: String) async -> Bool {
        await withCheckedContinuation { continuation in
            let alert = UIAlertController(title: title, message: message, preferredStyle: .alert)
            alert.addAction(UIAlertAction(title: "Cancel", style: .cancel) { _ in continuation.resume(returning: false) })
            alert.addAction(UIAlertAction(title: action, style: .default) { _ in continuation.resume(returning: true) })
            present(alert, orResume: { continuation.resume(returning: false) })
        }
    }

    @MainActor private static func present(_ alert: UIAlertController, orResume fail: () -> Void) {
        let window = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows)
            .first(where: \.isKeyWindow)
        guard var top = window?.rootViewController else { return fail() }
        while let presented = top.presentedViewController { top = presented }
        top.present(alert, animated: true)
    }
}
#endif
