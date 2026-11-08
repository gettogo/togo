import SwiftUI
import TogoKit
import UIKit

/// The agent's sign-in page inside the sign-in screen: what it is, where it returns to, and
/// Open, Copy and Open on Mac. It stays until the sign-in is over.
struct SignInLinkCard: View {
    let open: PendingOpen
    let agent: String
    let model: SignInPageModel

    @State private var note: String?
    @State private var failed = false
    @State private var copied = 0
    @State private var sending = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 6) {
                Text(LinkCopy.wantsToOpen(agent: agent.isEmpty ? open.agent : agent, host: host))
                    .font(.headline)
                    .foregroundStyle(Color.togoSnow)
                    .fixedSize(horizontal: false, vertical: true)
                Text(verbatim: open.url.absoluteString)
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(Color.togoMute)
                    // trunc-ok: a sign-in address with long query parameters, middle-cut to three lines; Copy takes it whole
                    .lineLimit(3)
                    .truncationMode(.middle)
                    .textSelection(.enabled)
                if let port = SignInRoute.callbackPort(open.url) {
                    Text(SignInPageCopy.returnsTo(port))
                        .font(.footnote)
                        .foregroundStyle(Color.togoMute)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if let note {
                    Text(note)
                        .font(.footnote)
                        .foregroundStyle(Color.togoMute)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            VStack(spacing: 4) {
                Button(model.progress.phase == .link ? LinkCopy.open : SignInPageCopy.openAgain) { model.openHere() }
                    .buttonStyle(.primary)
                HStack(spacing: 8) {
                    Button(copied > 0 ? LinkCopy.copied : LinkCopy.copy) {
                        SensitivePasteboard.copy(open.url)
                        failed = false
                        copied += 1
                    }
                    .buttonStyle(.secondary)
                    .contentTransition(.interpolate)
                    // motion: cause → effect — the tapped Copy says Copied in place
                    .motion(.respond, value: copied)
                    if model.canOpenOnMac {
                        Button(LinkCopy.openOnMac) { Task { await sendToMac() } }
                            .buttonStyle(.secondary)
                            .disabled(sending)
                    }
                }
            }
        }
        .padding(.vertical, 8)
        .haptic(.done, trigger: copied)
        .haptic(.error, trigger: failed, when: { $0 })
    }

    private var host: String { open.url.host(percentEncoded: false) ?? open.url.absoluteString }

    private func sendToMac() async {
        sending = true
        defer { sending = false }
        do {
            try await model.openOnMac()
            note = SignInPageCopy.sentToMac
        } catch {
            failed = true
            note = ServiceErrors.message(error)
        }
    }
}

enum SignInPageCopy {
    static func title(_ provider: String) -> String {
        String(localized: "signInPage.title", defaultValue: "Sign in to \(provider)")
    }

    static func sending(_ agent: String) -> String {
        String(localized: "signInPage.sending", defaultValue: "Asking \(name(agent)) to start the sign-in")
    }

    static func waiting(_ agent: String) -> String {
        String(localized: "signInPage.waiting", defaultValue: "Waiting for \(name(agent)) to open the sign-in page")
    }

    static let waitingDetail = String(
        localized: "signInPage.waitingDetail",
        defaultValue: "The page shows here as soon as it opens. If the agent asks something first, answer it in the chat."
    )
    static let ready = String(localized: "signInPage.ready", defaultValue: "The sign-in page is ready")
    static let browsing = String(localized: "signInPage.browsing", defaultValue: "Signing in")
    static let openAgain = String(localized: "signInPage.openAgain", defaultValue: "Open again")
    static let sentToMac = String(localized: "signInPage.sentToMac", defaultValue: "Opened on the machine. Finish signing in there.")

    static func confirming(_ agent: String) -> String {
        String(localized: "signInPage.confirming", defaultValue: "Waiting for \(name(agent)) to confirm")
    }

    static let confirmingDetail = String(
        localized: "signInPage.confirmingDetail", defaultValue: "This page updates when the provider is set up."
    )

    static func signedIn(_ provider: String) -> String {
        String(localized: "signInPage.signedIn", defaultValue: "Signed in to \(provider)")
    }

    static func failed(_ agent: String) -> String {
        String(localized: "signInPage.failed", defaultValue: "\(name(agent)) didn't start the sign-in.")
    }

    static func returnsTo(_ port: Int) -> String {
        String(localized: "signInPage.returnsTo", defaultValue: "Signing in returns to port \(String(port)) on your machine; Togo sends it there.")
    }

    /// A stable key per phase, for the crossfade between them.
    static func key(_ phase: SignInProgress.Phase) -> Int {
        switch phase {
        case .sending: 0
        case .waiting: 1
        case .link: 2
        case .browsing: 3
        case .confirming: 4
        case .signedIn: 5
        case .failed: 6
        }
    }

    private static func name(_ agent: String) -> String {
        agent.isEmpty ? String(localized: "signInPage.theAgent", defaultValue: "the agent") : agent
    }
}
