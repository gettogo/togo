import SafariServices
import SwiftUI
import TogoKit
import TogoTerminal
import UIKit

/// Where web links from sessions go: links tapped in a terminal, pages agents ask to open on
/// their machine, and the list of links on screen.
///
/// Pages open in Safari's view inside the app, except sign-in pages whose redirect returns to
/// the machine's loopback address: those open in `SignInBrowser`, which sends the redirect to
/// the machine through a port forward so the agent on it receives the code.
@MainActor final class LinkCenter {
    static let shared = LinkCenter()

    /// The machine and session a link came from, when known.
    struct Origin: Equatable {
        var hostID: HostID?
        var sessionID: String?
    }

    /// Opens a validated web page in the machine's own browser. Nil hides "Open on Mac".
    var openOnMac: ((HostID, URL) async throws -> Void)?
    /// Port forwards to the machine, for sign-in redirects. Nil opens every page in Safari's view.
    var forwarding: (any LinkForwarding)?

    /// Requests already shown, so a snapshot read twice does not ask twice.
    private var asked: [UUID] = []
    /// Requests older than this are stale: the agent has given up or the user saw it on the Mac.
    private let freshness: TimeInterval = 120

    // MARK: Entry points

    /// An agent asked to open `request` on the machine.
    /// `agent` names the agent that asked, when known.
    func agentAsked(_ request: BrowserOpen, hostID: HostID, agent: String?, now: Date = Date()) {
        guard !asked.contains(request.id) else { return }
        asked.append(request.id)
        if asked.count > 64 { asked.removeFirst(asked.count - 64) }
        guard now.timeIntervalSince(request.at) < freshness,
              let url = URL(string: request.url), LinkPolicy.allows(url) else { return }
        let origin = Origin(hostID: hostID, sessionID: request.sessionID)
        let canOpenOnMac = openOnMac != nil
        let handle = SheetHandle()
        present(prompt: OpenAskSheet(
            agent: agent, url: url, canOpenOnMac: canOpenOnMac, handle: handle,
            open: { [weak self] in self?.open(url, from: origin) },
            openOnMac: { [weak self] in try await self?.openOnMac?(hostID, url) }
        ), handle: handle)
    }

    /// A link the user chose: tapped in a terminal or picked from a list.
    func open(_ url: URL, from origin: Origin) {
        guard LinkPolicy.allows(url) else { return }
        if let hostID = origin.hostID, let forwarding, SignInRoute.returnsToLoopback(url) {
            let browser = SignInBrowserModel(start: url, hostID: hostID, sessionID: origin.sessionID, forwarding: forwarding)
            let handle = SheetHandle()
            let controller = UIHostingController(rootView: SignInBrowser(model: browser, handle: handle))
            handle.controller = controller
            LinkPresenter.present(controller, height: nil)
            return
        }
        let configuration = SFSafariViewController.Configuration()
        configuration.entersReaderIfAvailable = false
        configuration.barCollapsingEnabled = true
        let safari = SFSafariViewController(url: url, configuration: configuration)
        safari.dismissButtonStyle = .done
        LinkPresenter.present(safari, height: nil)
    }

    /// Every link on a terminal's screen and recent scrollback, newest first.
    func showLinks(_ urls: [URL], from origin: Origin) {
        let handle = SheetHandle()
        present(prompt: LinksSheet(urls: urls, handle: handle, open: { [weak self] in self?.open($0, from: origin) }), handle: handle)
    }

    private func present(prompt view: some View, handle: SheetHandle) {
        let host = UIHostingController(rootView: AnyView(view.togoControls()))
        handle.controller = host
        host.view.backgroundColor = UIColor.togoInk
        let fitted = host.sizeThatFits(in: CGSize(width: 390, height: CGFloat.greatestFiniteMagnitude)).height
        LinkPresenter.present(host, height: fitted)
    }
}

/// Which pages need the sign-in browser.
enum SignInRoute {
    static let loopbackHosts: Set<String> = ["localhost", "127.0.0.1", "::1", "[::1]"]

    static func isLoopback(_ url: URL) -> Bool {
        guard let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https",
              let host = url.host(percentEncoded: false)?.lowercased() else { return false }
        return loopbackHosts.contains(host) || host.hasSuffix(".localhost")
    }

    /// A page on the machine's loopback address, or a sign-in page whose redirect goes there,
    /// such as `redirect_uri=http://localhost:54545/callback`.
    static func returnsToLoopback(_ url: URL) -> Bool {
        if isLoopback(url) { return true }
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        return items.contains { item in
            guard let value = item.value, let inner = URL(string: value) else { return false }
            return isLoopback(inner)
        }
    }
}

/// Presents over whatever the active window shows, sheets included.
@MainActor enum LinkPresenter {
    /// `height` sizes a short sheet to its content; nil takes the full height.
    static func present(_ controller: UIViewController, height: CGFloat?) {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        let scene = scenes.first { $0.activationState == .foregroundActive } ?? scenes.first
        guard var top = scene?.keyWindow?.rootViewController ?? scene?.windows.first?.rootViewController else { return }
        while let next = top.presentedViewController, !next.isBeingDismissed {
            top = next
        }
        if let height {
            controller.modalPresentationStyle = .formSheet
            if let sheet = controller.sheetPresentationController {
                sheet.detents = [.custom { context in min(max(height, 200), context.maximumDetentValue) }, .large()]
                sheet.prefersGrabberVisible = true
            }
        } else {
            controller.modalPresentationStyle = .pageSheet
            controller.sheetPresentationController?.detents = [.large()]
        }
        top.present(controller, animated: true)
    }
}

/// Opens port forwards to a machine for the sign-in browser.
@MainActor protocol LinkForwarding: AnyObject {
    /// The local port on this device that reaches `port` on the machine's loopback address.
    func forward(hostID: HostID, sessionID: String?, port: Int, host: String) async throws -> Int
    func stop(localPort: Int) async
}

/// Forwards through the review services' port forwards, the same ones port previews use.
@MainActor final class ReviewLinkForwarding: LinkForwarding {
    private let review: any ReviewServices

    init(review: any ReviewServices) {
        self.review = review
    }

    func forward(hostID: HostID, sessionID: String?, port: Int, host: String) async throws -> Int {
        let subject = ReviewSubject(
            hostID: hostID, sessionID: sessionID ?? "", bib: nil, title: "", cwd: "", managed: true, capabilities: []
        )
        var address = host.hasPrefix("[") || host == "::1" ? "::1" : "127.0.0.1"
        // The session's own list says which loopback address the server took, IPv4 or IPv6.
        if sessionID != nil, let listed = try? await review.ports(subject).first(where: { $0.port == port }) {
            address = listed.address
        }
        let info = try await review.forward(subject, port: ListeningPort(port: port, address: address, pid: 0, command: ""))
        return info.localPort
    }

    func stop(localPort: Int) async {
        await review.stopForward(localPort: localPort)
    }
}
