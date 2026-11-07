import SafariServices
import SwiftUI
import WebKit

/// A dev server's page, loaded from the forward's loopback address on this device.
///
/// The page runs in a web view with its own throwaway storage and no bridge to the app, and it
/// stays on the forwarded server: links to anywhere else don't open. Safari's view controller
/// is kept as an alternative engine, off by default.
struct PreviewSheet: View {
    enum Engine: Sendable {
        case webView, safari
    }

    static let engine = Engine.webView

    let url: URL
    let title: String
    /// Called when the user does something in the preview, so the forward isn't stopped as idle.
    let activity: () -> Void

    var body: some View {
        switch Self.engine {
        case .webView:
            WebPreview(url: url, title: title, activity: activity)
        case .safari:
            SafariPreview(url: url)
                .ignoresSafeArea()
        }
    }
}

/// Back, forward and reload, with a page that fails to load said in words.
private struct WebPreview: View {
    @Environment(\.dismiss) private var dismiss
    /// Made once on appear: a view struct is rebuilt often, a web view should not be.
    @State private var page: WebPage?
    let url: URL
    let title: String
    let activity: () -> Void

    var body: some View {
        NavigationStack {
            ZStack {
                if let page {
                    content(page)
                }
            }
            // motion: state change — a failed load covers the page with its sentence, and leaves
            .motion(.fade, value: page?.failure)
            .background(Color.togoInk)
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(PortsCopy.done, systemImage: "checkmark", role: .confirm) { dismiss() }
                }
                ToolbarItemGroup(placement: .bottomBar) {
                    Button(PortsCopy.back, systemImage: "chevron.backward") {
                        activity()
                        page?.view.goBack()
                    }
                    .disabled(page?.canGoBack != true)
                    Button(PortsCopy.forward, systemImage: "chevron.forward") {
                        activity()
                        page?.view.goForward()
                    }
                    .disabled(page?.canGoForward != true)
                    Button(PortsCopy.reload, systemImage: "arrow.clockwise", action: reload)
                }
            }
        }
        .togoControls()
        .onAppear {
            if page == nil { page = WebPage(origin: url) }
        }
        .onChange(of: page?.navigations) { activity() }
    }

    @ViewBuilder private func content(_ page: WebPage) -> some View {
        WebViewHost(page: page)
            .ignoresSafeArea(edges: .bottom)
        if let failure = page.failure {
            StateView(
                kind: .error, title: PortsCopy.pageFailed, message: failure,
                action: .init(PortsCopy.reload) { reload() }
            )
            .background(Color.togoInk)
            // motion: state change — the failure covers the page in place
            .transition(.opacity)
        }
    }

    private func reload() {
        guard let page else { return }
        activity()
        page.failure = nil
        if page.view.url == nil {
            page.view.load(URLRequest(url: url))
        } else {
            page.view.reload()
        }
    }
}

/// The web view and what the toolbar shows about it.
@MainActor @Observable private final class WebPage: NSObject, WKNavigationDelegate, WKUIDelegate {
    let view: WKWebView
    private(set) var canGoBack = false
    private(set) var canGoForward = false
    private(set) var navigations = 0
    var failure: String?
    @ObservationIgnored private var observations: [NSKeyValueObservation] = []
    /// The forward's address. Navigation is allowed only to this scheme, host and port.
    @ObservationIgnored let origin: URL

    init(origin: URL) {
        self.origin = origin
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        configuration.dataDetectorTypes = []
        configuration.preferences.javaScriptCanOpenWindowsAutomatically = false
        configuration.allowsInlineMediaPlayback = true
        view = WKWebView(frame: .zero, configuration: configuration)
        view.allowsLinkPreview = false
        view.allowsBackForwardNavigationGestures = true
        view.isInspectable = false
        super.init()
        view.navigationDelegate = self
        view.uiDelegate = self
        observations = [
            view.observe(\.canGoBack, options: [.new]) { [weak self] web, _ in
                MainActor.assumeIsolated { self?.canGoBack = web.canGoBack }
            },
            view.observe(\.canGoForward, options: [.new]) { [weak self] web, _ in
                MainActor.assumeIsolated { self?.canGoForward = web.canGoForward }
            },
        ]
        view.load(URLRequest(url: origin))
    }

    func webView(
        _ webView: WKWebView, decidePolicyFor action: WKNavigationAction
    ) async -> WKNavigationActionPolicy {
        guard let target = action.request.url else { return .cancel }
        if target.scheme == "about" || sameOrigin(target) { return .allow }
        return .cancel
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        failure = nil
        navigations += 1
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: any Error) {
        fail(error)
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: any Error) {
        fail(error)
    }

    /// A link that asks for a new window opens here instead, if it stays on the server.
    func webView(
        _ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
        for action: WKNavigationAction, windowFeatures: WKWindowFeatures
    ) -> WKWebView? {
        if let target = action.request.url, sameOrigin(target) {
            webView.load(action.request)
        }
        return nil
    }

    private func sameOrigin(_ target: URL) -> Bool {
        return target.scheme == origin.scheme && target.host == origin.host && target.port == origin.port
    }

    private func fail(_ error: any Error) {
        // A newer navigation replaced this one; nothing failed.
        if (error as NSError).code == NSURLErrorCancelled { return }
        failure = ServiceErrors.message(error)
    }
}

private struct WebViewHost: UIViewRepresentable {
    let page: WebPage

    func makeUIView(context: Context) -> WKWebView {
        page.view
    }

    func updateUIView(_ view: WKWebView, context: Context) {}
}

private struct SafariPreview: UIViewControllerRepresentable {
    let url: URL

    func makeUIViewController(context: Context) -> SFSafariViewController {
        let configuration = SFSafariViewController.Configuration()
        configuration.entersReaderIfAvailable = false
        configuration.barCollapsingEnabled = true
        return SFSafariViewController(url: url, configuration: configuration)
    }

    func updateUIViewController(_ controller: SFSafariViewController, context: Context) {}
}
