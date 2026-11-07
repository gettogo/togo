import SwiftUI
import TogoKit
import TogoTerminal
import WebKit

/// A browser for signing an agent in from the phone.
///
/// Sign-in pages for command-line agents end by redirecting to a server the agent runs on its
/// machine, such as `http://localhost:54545/callback?code=...`. That address means nothing on
/// the phone, so the redirect is caught here and sent to the machine instead: the port is
/// forwarded over the session's SSH connection and the same path and query load through the
/// forward, which hands the code to the agent. Then the browser says so and closes.
@MainActor @Observable final class SignInBrowserModel: NSObject, WKNavigationDelegate, WKUIDelegate {
    enum Phase: Equatable {
        case browsing
        case reaching
        case signedIn
        case failed(String)
    }

    let view: WKWebView
    private(set) var phase: Phase = .browsing
    private(set) var host = ""
    private(set) var canGoBack = false
    /// Set by the view to close the browser.
    @ObservationIgnored var close: () -> Void = {}

    @ObservationIgnored private let start: URL
    @ObservationIgnored private let hostID: HostID
    @ObservationIgnored private let sessionID: String?
    @ObservationIgnored private let forwarding: any LinkForwarding
    /// Local port on this device for each machine port, opened on demand.
    @ObservationIgnored private var forwards: [Int: Int] = [:]
    /// A page off the machine was shown, so a redirect back to it is the end of a sign-in.
    @ObservationIgnored private var leftMachine = false
    /// The redirect went to the machine and its answer is awaited.
    @ObservationIgnored private var awaitingCallback = false
    @ObservationIgnored private var observations: [NSKeyValueObservation] = []
    @ObservationIgnored private var closing: Task<Void, Never>?

    init(start: URL, hostID: HostID, sessionID: String?, forwarding: any LinkForwarding) {
        self.start = start
        self.hostID = hostID
        self.sessionID = sessionID
        self.forwarding = forwarding
        let configuration = WKWebViewConfiguration()
        // Kept, so a provider remembers the user the next time an agent asks to sign in.
        configuration.websiteDataStore = .default()
        configuration.dataDetectorTypes = []
        // Some providers turn embedded browsers away; this one reads as Safari.
        configuration.applicationNameForUserAgent = "Version/26.0 Mobile/15E148 Safari/604.1"
        view = WKWebView(frame: .zero, configuration: configuration)
        view.allowsBackForwardNavigationGestures = true
        view.allowsLinkPreview = false
        view.isInspectable = false
        super.init()
        view.navigationDelegate = self
        view.uiDelegate = self
        observations = [
            view.observe(\.canGoBack, options: [.new]) { [weak self] web, _ in
                MainActor.assumeIsolated { self?.canGoBack = web.canGoBack }
            },
            view.observe(\.url, options: [.new]) { [weak self] web, _ in
                MainActor.assumeIsolated { self?.host = Self.shownHost(web.url) }
            },
        ]
        host = Self.shownHost(start)
    }

    func begin() {
        guard view.url == nil, phase == .browsing else { return }
        open(start)
    }

    func reload() {
        if case .failed = phase { phase = .browsing }
        if view.url == nil { open(start) } else { view.reload() }
    }

    /// Stops every forward this browser opened.
    func finish() {
        closing?.cancel()
        let open = Array(forwards.values)
        forwards = [:]
        let forwarding = forwarding
        Task {
            for port in open {
                await forwarding.stop(localPort: port)
            }
        }
    }

    private func open(_ url: URL) {
        if onMachine(url) {
            Task { await deliver(url) }
        } else {
            leftMachine = true
            view.load(URLRequest(url: url))
        }
    }

    // MARK: Routing

    /// A loopback address that means the agent's machine, not one of this browser's forwards.
    private func onMachine(_ url: URL) -> Bool {
        guard SignInRoute.isLoopback(url) else { return false }
        return !(url.host() == "127.0.0.1" && forwards.values.contains(url.port ?? 80))
    }

    private func isForward(_ url: URL) -> Bool {
        SignInRoute.isLoopback(url) && !onMachine(url)
    }

    /// Loads `url` on the machine through a forward of its port.
    private func deliver(_ url: URL) async {
        let scheme = url.scheme?.lowercased() ?? "http"
        let port = url.port ?? (scheme == "https" ? 443 : 80)
        phase = .reaching
        do {
            let local: Int
            if let open = forwards[port] {
                local = open
            } else {
                local = try await forwarding.forward(
                    hostID: hostID, sessionID: sessionID, port: port, host: url.host() ?? "localhost"
                )
                forwards[port] = local
            }
            guard var parts = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return }
            parts.scheme = "http"
            parts.host = "127.0.0.1"
            parts.port = local
            guard let through = parts.url else { return }
            awaitingCallback = leftMachine
            phase = .browsing
            view.load(URLRequest(url: through))
        } catch {
            phase = .failed(LinkCopy.callbackFailed(port, ServiceErrors.message(error)))
        }
    }

    private func signedIn() {
        guard awaitingCallback else { return }
        awaitingCallback = false
        phase = .signedIn
        closing = Task { [weak self] in
            try? await Task.sleep(for: .seconds(1.5))
            guard !Task.isCancelled else { return }
            self?.close()
        }
    }

    // MARK: WKNavigationDelegate

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction) async -> WKNavigationActionPolicy {
        guard let url = action.request.url, let scheme = url.scheme?.lowercased() else { return .cancel }
        if scheme == "about" || scheme == "blob" || scheme == "data" { return .allow }
        guard scheme == "http" || scheme == "https" else { return .cancel }
        if onMachine(url) {
            if action.targetFrame?.isMainFrame != false { Task { await deliver(url) } }
            return .cancel
        }
        if !isForward(url) {
            // The machine answered the callback and sent the page on, as some agents do.
            if awaitingCallback, action.targetFrame?.isMainFrame != false { signedIn() }
            leftMachine = true
        }
        return .allow
    }

    func webView(_ webView: WKWebView, decidePolicyFor response: WKNavigationResponse) async -> WKNavigationResponsePolicy {
        if awaitingCallback, response.isForMainFrame, let url = response.response.url, isForward(url),
           let status = (response.response as? HTTPURLResponse)?.statusCode, status < 400 {
            signedIn()
        }
        return .allow
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: any Error) {
        fail(error)
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: any Error) {
        fail(error)
    }

    /// A page that opens a window opens here instead.
    func webView(
        _ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
        for action: WKNavigationAction, windowFeatures: WKWindowFeatures
    ) -> WKWebView? {
        if let url = action.request.url, LinkPolicy.allows(url) { open(url) }
        return nil
    }

    private func fail(_ error: any Error) {
        let code = (error as NSError).code
        // Cancelled navigations were replaced, often by a redirect this browser took over.
        if code == NSURLErrorCancelled || code == 102 || phase == .signedIn { return }
        phase = .failed(ServiceErrors.message(error))
    }

    private static func shownHost(_ url: URL?) -> String {
        guard let url else { return "" }
        if SignInRoute.isLoopback(url) { return LinkCopy.signIn }
        return url.host(percentEncoded: false) ?? ""
    }
}

struct SignInBrowser: View {
    @State var model: SignInBrowserModel
    let handle: SheetHandle

    var body: some View {
        NavigationStack {
            ZStack {
                WebViewBox(view: model.view)
                    .ignoresSafeArea(edges: .bottom)
                overlay
            }
            .background(Color.togoInk)
            .navigationTitle(model.host)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(LinkCopy.done, systemImage: "checkmark", role: .confirm) { handle.close() }
                }
                ToolbarItemGroup(placement: .bottomBar) {
                    Button(LinkCopy.back, systemImage: "chevron.backward") { model.view.goBack() }
                        .disabled(!model.canGoBack)
                    Button(LinkCopy.reload, systemImage: "arrow.clockwise") { model.reload() }
                }
            }
        }
        .togoControls()
        .haptic(.done, trigger: model.phase, when: { $0 == .signedIn })
        .onAppear {
            model.close = { [handle] in handle.close() }
            model.begin()
        }
        .onDisappear { model.finish() }
    }

    @ViewBuilder private var overlay: some View {
        switch model.phase {
        case .browsing:
            EmptyView()
        case .reaching:
            StateView(kind: .loading, title: LinkCopy.reaching, message: "")
                .background(Color.togoInk)
        case .signedIn:
            VStack(spacing: 8) {
                Image(systemName: "checkmark")
                    .font(.title2.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                Text(LinkCopy.signedIn)
                    .font(.headline)
                    .foregroundStyle(Color.togoSnow)
                Text(LinkCopy.signedInDetail)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .multilineTextAlignment(.center)
            }
            .padding(24)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
            .accessibilityElement(children: .combine)
        case .failed(let message):
            StateView(
                kind: .error, title: LinkCopy.signIn, message: message,
                action: .init(LinkCopy.reload) { model.reload() }
            )
            .background(Color.togoInk)
        }
    }
}

private struct WebViewBox: UIViewRepresentable {
    let view: WKWebView

    func makeUIView(context: Context) -> WKWebView { view }

    func updateUIView(_ view: WKWebView, context: Context) {}
}
