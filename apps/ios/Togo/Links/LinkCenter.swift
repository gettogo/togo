import SafariServices
import SwiftUI
import TogoKit
import TogoTerminal
import UIKit

/// Where web links from sessions go: links tapped in a terminal, pages agents ask to open on
/// their machine, and the list of links on screen.
///
/// Pages open in Safari's view inside the app, except sign-in pages: those open in the system's
/// authentication browser (`SignInRunner`), which providers accept where they refuse an
/// embedded web view. A redirect to the machine's loopback address reaches the agent through a
/// forward this phone opens on the same port first (`CallbackForward`).
@MainActor final class LinkCenter {
    static let shared = LinkCenter()

    /// The machine and session a link came from, when known.
    struct Origin: Equatable {
        var hostID: HostID?
        var sessionID: String?
    }

    /// Opens a validated web page in the machine's own browser. Nil hides "Open on Mac".
    var openOnMac: ((HostID, URL) async throws -> Void)?
    /// The machine's SSH connection, for sign-in callbacks. Nil leaves only the paste-code way.
    var tunnel: (any CallbackTunnel)?
    /// Types a pasted sign-in code into the agent.
    var codeSender: (any SignInCodeSending)?
    /// Whether a session's agent shows a prompt for a code or key now.
    var awaitsCode: ((HostID, String) -> Bool)?

    /// Pages agents asked to open, until used or expired.
    let requests = OpenRequests()

    /// How many times a sheet torn down by a presentation under it comes back.
    private let reshowLimit = 3

    // MARK: Entry points

    /// An agent asked to open `request` on the machine.
    /// `agent` names the agent that asked, when known.
    func agentAsked(_ request: BrowserOpen, hostID: HostID, agent: String?, now: Date = Date()) {
        guard let (open, route) = requests.receive(request, hostID: hostID, agent: agent, now: now) else { return }
        if route == .present { ask(open) }
    }

    /// "Claude wants to open claude.ai" as a sheet over whatever shows. The request stays
    /// pending until the user opens it here or on the Mac; a sheet a presentation under it
    /// took down with itself comes back, and one the user swiped away leaves the request in
    /// the chat's banner and the roster.
    func ask(_ open: PendingOpen, shown: Int = 0) {
        guard requests.isPending(open.id) else { return }
        let handle = SheetHandle()
        let view = OpenAskSheet(
            agent: open.agent, url: open.url, canOpenOnMac: openOnMac != nil, handle: handle,
            open: { [weak self] in self?.use(open) },
            openOnMac: { [weak self] in try await self?.sendToMac(open) }
        )
        let host = LinkSheetController(rootView: AnyView(view.togoControls()))
        host.onTornDown = { [weak self] in
            guard let self, shown + 1 < self.reshowLimit, self.requests.isPending(open.id) else { return }
            self.ask(open, shown: shown + 1)
        }
        handle.controller = host
        present(prompt: host)
    }

    /// Sign-ins under way from links and request cards, kept until they end.
    private var running: [ObjectIdentifier: SignInRunner] = [:]

    /// Opens a pending request here: a sign-in in the authentication browser, else Safari's view.
    func use(_ open: PendingOpen) {
        requests.resolve(open.id)
        if SignInRoute.isSignIn(open.url) {
            signIn(open.url, from: open.origin)
        } else {
            self.open(open.url, from: open.origin)
        }
    }

    /// A sign-in page, from an agent's request or the bar's Open: the system's authentication
    /// browser, with the callback forward first when it returns to the machine. Its outcome
    /// comes up as a sheet when it closes.
    func signIn(_ url: URL, from origin: Origin) {
        guard LinkPolicy.allows(url) else { return }
        let runner = makeRunner(url, hostID: origin.hostID, sessionID: origin.sessionID)
        let id = ObjectIdentifier(runner)
        running[id] = runner
        var shown = false
        runner.onEnd = { [weak self, weak runner] phase in
            guard let self, let runner else { return }
            if phase == .signedIn, !shown { shown = true; self.presentOutcome(runner) }
            self.running[id] = nil
        }
        runner.onBrowserClosed = { [weak self, weak runner] in
            guard let self, let runner, !shown else { return }
            if runner.phase == .needsCode || runner.phase == .delivered {
                shown = true
                self.presentOutcome(runner)
            }
            self.running[id] = nil
        }
        runner.begin()
    }

    /// A runner for `url`, with this app's tunnel.
    func makeRunner(_ url: URL, hostID: HostID?, sessionID: String?) -> SignInRunner {
        SignInRunner(url: url, hostID: hostID, sessionID: sessionID, tunnel: hostID == nil ? nil : tunnel)
    }

    private func presentOutcome(_ runner: SignInRunner) {
        let handle = SheetHandle()
        var paste: PasteCodeModel?
        if let hostID = runner.hostID, let session = runner.sessionID, !session.isEmpty, let codeSender {
            let model = PasteCodeModel(hostID: hostID, sessionID: session, sender: codeSender)
            model.onSent = { [weak runner] in runner?.codeSent() }
            paste = model
        }
        // The sheet keeps the runner from here on.
        let controller = LinkSheetController(rootView: AnyView(SignInOutcomeSheet(runner: runner, paste: paste, handle: handle)))
        handle.controller = controller
        // The authentication browser is still going away; the presenter waits for it.
        LinkPresenter.present(controller, height: 360, name: "link.signInOutcome")
    }

    /// Opens a pending request in the machine's own browser.
    func sendToMac(_ open: PendingOpen) async throws {
        try await openOnMac?(open.hostID, open.url)
        requests.resolve(open.id)
    }

    /// A link the user chose: tapped in a terminal or picked from a list.
    func open(_ url: URL, from origin: Origin) {
        guard LinkPolicy.allows(url) else { return }
        if SignInRoute.returnsToLoopback(url) {
            signIn(url, from: origin)
            return
        }
        let configuration = SFSafariViewController.Configuration()
        configuration.entersReaderIfAvailable = false
        configuration.barCollapsingEnabled = true
        let safari = SFSafariViewController(url: url, configuration: configuration)
        safari.dismissButtonStyle = .done
        LinkPresenter.present(safari, height: nil, name: "link.safari")
    }

    /// Every link on a terminal's screen and recent scrollback, newest first.
    func showLinks(_ urls: [URL], from origin: Origin) {
        let handle = SheetHandle()
        let view = LinksSheet(urls: urls, handle: handle, open: { [weak self] in self?.open($0, from: origin) })
        let host = UIHostingController(rootView: AnyView(view.togoControls()))
        handle.controller = host
        present(prompt: host)
    }

    private func present(prompt host: UIHostingController<AnyView>) {
        host.view.backgroundColor = UIColor.togoInk
        let fitted = host.sizeThatFits(in: CGSize(width: 390, height: CGFloat.greatestFiniteMagnitude)).height
        LinkPresenter.present(host, height: fitted, name: "link.list")
    }
}
