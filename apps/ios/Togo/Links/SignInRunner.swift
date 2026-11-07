import Foundation
import Observation
import TogoKit

/// Runs one web sign-in: the forward for its callback, then the system's authentication
/// browser, then the outcome. `WebAuthFlow` decides; this carries out its effects.
@MainActor @Observable final class SignInRunner {
    let page: SignInURL
    let hostID: HostID?
    let sessionID: String?
    private(set) var flow: WebAuthFlow
    /// The forward could not come up, or was lost: what to say beside the paste field.
    private(set) var note: String?

    var phase: WebAuthFlow.Phase { flow.phase }
    /// The flow reached signed in, cancelled or failed.
    @ObservationIgnored var onEnd: (@MainActor (WebAuthFlow.Phase) -> Void)?
    /// The authentication browser closed, by code or by the user.
    @ObservationIgnored var onBrowserClosed: (@MainActor () -> Void)?
    /// The callback went through to the machine.
    @ObservationIgnored var onDelivered: (@MainActor () -> Void)?

    @ObservationIgnored private let tunnel: (any CallbackTunnel)?
    @ObservationIgnored private let browser: any WebAuthPresenting
    @ObservationIgnored private var forward: CallbackForward?
    @ObservationIgnored private let settleDelay: Duration
    @ObservationIgnored private var ended = false

    init(
        url: URL, hostID: HostID?, sessionID: String?, tunnel: (any CallbackTunnel)?,
        browser: any WebAuthPresenting = WebAuthSession(), settleDelay: Duration = .milliseconds(1500)
    ) {
        page = SignInURL(url)
        self.hostID = hostID
        self.sessionID = sessionID
        self.tunnel = tunnel
        self.browser = browser
        self.settleDelay = settleDelay
        var flow = WebAuthFlow(page)
        if tunnel == nil || hostID == nil, flow.needsForward {
            // Nothing can carry the callback: the provider's code is the only way back.
            flow = WebAuthFlow(SignInURL(url), forwardless: true)
        }
        self.flow = flow
    }

    func begin() {
        ended = false
        run(flow.start())
    }

    func reopen() {
        ended = false
        send(.reopen)
    }

    /// The agent lists the provider as set up: whatever the browser shows, it is done.
    func providerSetUp() { send(.providerSetUp) }

    /// A pasted code went into the session.
    func codeSent() { send(.codeSent) }

    /// Closes the browser and the forward, as when the screen goes.
    func finish() {
        browser.close()
        let forward = forward
        self.forward = nil
        Task { await forward?.stop() }
    }

    // MARK: Effects

    private func send(_ event: WebAuthFlow.Event) {
        let before = flow.phase
        run(flow.handle(event))
        if flow.phase == .delivered, before != .delivered { onDelivered?() }
        if flow.isOver, !ended {
            ended = true
            onEnd?(flow.phase)
        }
        if event == .browserEnded { onBrowserClosed?() }
    }

    private func run(_ effects: [WebAuthFlow.Effect]) {
        for effect in effects {
            switch effect {
            case .openForward: openForward()
            case .openBrowser: openBrowser()
            case .closeBrowser: browser.close()
            case .stopForward: finishForward()
            case .settleLater:
                Task { [weak self, settleDelay] in
                    try? await Task.sleep(for: settleDelay)
                    self?.send(.settled)
                }
            }
        }
    }

    private func openForward() {
        guard let tunnel, let hostID, let port = page.forwardablePort else {
            send(.forwardBusy)
            return
        }
        let forward = CallbackForward(tunnel: tunnel, hostID: hostID, sessionID: sessionID, port: port)
        forward.onCallback = { [weak self] in self?.send(.callbackSeen) }
        forward.onLost = { [weak self] loss in
            let reason = switch loss {
            case .portTaken: CallbackCopy.portTaken(port)
            case .unreachable(let detail): CallbackCopy.unreachable(detail)
            }
            self?.note = reason
            self?.send(.forwardLost(reason))
        }
        self.forward = forward
        Task { [weak self] in
            do {
                let ready = try await forward.start()
                guard let self, self.forward === forward else { return }
                if !ready { self.note = CallbackCopy.portTaken(port) }
                self.send(ready ? .forwardReady : .forwardBusy)
            } catch {
                guard let self, self.forward === forward else { return }
                self.note = ServiceErrors.message(error)
                self.send(.forwardLost(ServiceErrors.message(error)))
            }
        }
    }

    private func openBrowser() {
        let opened = browser.open(page.url) { [weak self] in self?.send(.browserEnded) }
        if !opened { send(.browserEnded) }
    }

    private func finishForward() {
        let forward = forward
        self.forward = nil
        Task { await forward?.stop() }
    }
}

enum CallbackCopy {
    static func portTaken(_ port: Int) -> String {
        String(
            localized: "signIn.portTaken",
            defaultValue: "Another app on this iPhone is using port \(String(port)), so the sign-in can't come back on its own. Paste the code instead."
        )
    }

    static func unreachable(_ detail: String) -> String {
        String(
            localized: "signIn.unreachable",
            defaultValue: "Lost the connection to your Mac during the sign-in. \(detail)"
        )
    }
}
