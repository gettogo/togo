import SwiftUI
import TogoKit
import UIKit

/// Closes a sheet `LinkPresenter` put up, then runs what comes next once it is gone.
@MainActor final class SheetHandle {
    weak var controller: UIViewController?

    func close(then next: (@MainActor @Sendable () -> Void)? = nil) {
        guard let controller else {
            next?()
            return
        }
        controller.dismiss(animated: true) { next?() }
    }
}

/// "Claude wants to open claude.ai": an agent asked its machine to open a web page.
struct OpenAskSheet: View {
    let agent: String?
    let url: URL
    let canOpenOnMac: Bool
    let handle: SheetHandle
    let open: @MainActor @Sendable () -> Void
    let openOnMac: () async throws -> Void

    @State private var note: String?
    @State private var failed = false
    @State private var copied = 0
    @State private var working = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                VStack(alignment: .leading, spacing: 6) {
                    Text(LinkCopy.wantsToOpen(agent: agent, host: url.host(percentEncoded: false) ?? url.absoluteString))
                        .font(.headline)
                        .foregroundStyle(Color.togoSnow)
                        .fixedSize(horizontal: false, vertical: true)
                    Text(verbatim: url.absoluteString)
                        .font(.system(.footnote, design: .monospaced))
                        .foregroundStyle(Color.togoMute)
                        .lineLimit(3)
                        .truncationMode(.middle)
                        .textSelection(.enabled)
                }
                if let note {
                    Text(note)
                        .font(.footnote)
                        .foregroundStyle(Color.togoMute)
                }
                VStack(spacing: 4) {
                    Button(LinkCopy.open) { handle.close(then: open) }
                        .buttonStyle(.primary)
                    HStack(spacing: 8) {
                        Button(LinkCopy.copy) {
                            UIPasteboard.general.url = url
                            failed = false
                            note = LinkCopy.copied
                            copied += 1
                        }
                        .buttonStyle(.secondary)
                        if canOpenOnMac {
                            Button(LinkCopy.openOnMac) { Task { await sendToMac() } }
                                .buttonStyle(.secondary)
                                .disabled(working)
                        }
                    }
                }
            }
            .padding(20)
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(Color.togoInk)
        .haptic(.done, trigger: copied)
        .haptic(.error, trigger: failed, when: { $0 })
    }

    private func sendToMac() async {
        working = true
        defer { working = false }
        do {
            try await openOnMac()
            handle.close()
        } catch {
            failed = true
            note = ServiceErrors.message(error)
        }
    }
}

/// Every web link on a terminal's screen and recent scrollback, newest first.
struct LinksSheet: View {
    let urls: [URL]
    let handle: SheetHandle
    let open: @MainActor @Sendable (URL) -> Void

    @State private var copied: URL?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                Text(LinkCopy.linksOnScreen)
                    .font(.headline)
                    .foregroundStyle(Color.togoSnow)
                if urls.isEmpty {
                    Text(LinkCopy.noLinks)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoMute)
                }
                ForEach(urls, id: \.self) { url in
                    row(url)
                    if url != urls.last {
                        Rectangle().fill(Color.togoLine).frame(height: 1)
                    }
                }
            }
            .padding(20)
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(Color.togoInk)
        .haptic(.done, trigger: copied)
    }

    private func row(_ url: URL) -> some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: url.host(percentEncoded: false) ?? "")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                    .lineLimit(1)
                Text(verbatim: url.absoluteString)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(Color.togoMute)
                    .lineLimit(2)
                    .truncationMode(.middle)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Button(copied == url ? LinkCopy.copied : LinkCopy.copy) {
                UIPasteboard.general.url = url
                copied = url
            }
            .buttonStyle(.secondary(.fit))
            Button(LinkCopy.open) { handle.close { open(url) } }
                .buttonStyle(.primary(.fit))
        }
        .accessibilityElement(children: .contain)
    }
}

enum LinkCopy {
    static let open = String(localized: "links.open", defaultValue: "Open")
    static let copy = String(localized: "links.copy", defaultValue: "Copy")
    static let copied = String(localized: "links.copied", defaultValue: "Copied")
    static let openOnMac = String(localized: "links.openOnMac", defaultValue: "Open on Mac")
    static let linksOnScreen = String(localized: "links.onScreen", defaultValue: "Links on screen")
    static let noLinks = String(localized: "links.none", defaultValue: "No links on screen or in recent output.")
    static let signIn = String(localized: "links.signIn", defaultValue: "Sign in")
    static let signedIn = String(localized: "links.signedIn", defaultValue: "Signed in")
    static let signedInDetail = String(
        localized: "links.signedInDetail", defaultValue: "The agent on your machine has the code."
    )
    static let done = String(localized: "links.done", defaultValue: "Done")
    static let back = String(localized: "links.back", defaultValue: "Back")
    static let reload = String(localized: "links.reload", defaultValue: "Reload")
    static let reaching = String(localized: "links.reaching", defaultValue: "Sending the sign-in to your machine")

    static func wantsToOpen(agent: String?, host: String) -> String {
        if let agent, !agent.isEmpty {
            return String(localized: "links.wantsToOpen", defaultValue: "\(agent) wants to open \(host)")
        }
        return String(localized: "links.wantsToOpen.unknown", defaultValue: "Your agent wants to open \(host)")
    }

    static func callbackFailed(_ port: Int, _ reason: String) -> String {
        String(localized: "links.callbackFailed", defaultValue: "Couldn't reach port \(String(port)) on your machine. \(reason)")
    }
}
