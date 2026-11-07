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
        (controller as? LinkSheetController)?.closedOnPurpose = true
        controller.dismiss(animated: true) { next?() }
    }
}

/// A sheet that knows why it went away: closed by its own buttons, swiped away by the user, or
/// taken down by a presentation under it closing (a picker sheet that settled). Only the last
/// calls `onTornDown`, so the sheet can come back: nothing closes it without the user.
final class LinkSheetController: UIHostingController<AnyView>, UIAdaptivePresentationControllerDelegate {
    var onTornDown: (@MainActor () -> Void)?
    /// Its own buttons closed it, or the user swiped it down.
    var closedOnPurpose = false

    override func viewDidLoad() {
        super.viewDidLoad()
        presentationController?.delegate = self
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        presentationController?.delegate = self
    }

    override func viewDidDisappear(_ animated: Bool) {
        super.viewDidDisappear(animated)
        // Asked a moment later, once the user's swipe has reported itself and a sheet that
        // was only covered is known to be still presented.
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: .milliseconds(300))
            guard let self, !self.closedOnPurpose, self.presentingViewController == nil else { return }
            self.closedOnPurpose = true
            self.onTornDown?()
        }
    }

    func presentationControllerDidDismiss(_ presentationController: UIPresentationController) {
        closedOnPurpose = true
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
                Text(verbatim: url.absoluteString)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(Color.togoMute)
                    // trunc-ok: a link in a list, middle-cut so host and end show; choosing it opens a sheet with the whole address
                    .lineLimit(2)
                    .truncationMode(.middle)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Button {
                UIPasteboard.general.url = url
                copied = url
            } label: {
                Text(copied == url ? LinkCopy.copied : LinkCopy.copy)
                    .contentTransition(.interpolate)
            }
            .buttonStyle(.secondary(.fit))
            // motion: cause → effect — the tapped Copy says Copied in place
            .motion(.respond, value: copied == url)
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
    static let done = String(localized: "links.done", defaultValue: "Done")
    static let dismiss = String(localized: "links.dismiss", defaultValue: "Dismiss")

    static func wantsToOpen(agent: String?, host: String) -> String {
        if let agent, !agent.isEmpty {
            return String(localized: "links.wantsToOpen", defaultValue: "\(agent) wants to open \(host)")
        }
        return String(localized: "links.wantsToOpen.unknown", defaultValue: "Your agent wants to open \(host)")
    }
}
