import CryptoKit
import SwiftUI
import TogoKit
import UIKit
import WebKit

/// One serial, offline rendering process. Rows hold only cached images, never web views.
@MainActor final class MermaidRenderer: NSObject, WKNavigationDelegate {
    static let shared = MermaidRenderer()
    struct Key: Hashable {
        let digest: String
        let dark: Bool
        let width: Int
        init(_ source: String, dark: Bool, width: CGFloat) {
            digest = SHA256.hash(data: Data(source.utf8)).map { String(format: "%02x", $0) }.joined()
            self.dark = dark
            self.width = max(160, min(1600, Int(width / 32) * 32))
        }
    }
    private struct Entry { let image: UIImage?; let cost: Int }
    private var cache: [Key: Entry] = [:]
    private var order: [Key] = []
    private var jobs: [Key: Task<UIImage?, Never>] = [:]
    private var tail: Task<Void, Never>?
    private var web: WKWebView?
    private var loaded = false
    private(set) var lastFailure: String?
    private var ready: CheckedContinuation<Bool, Never>?
    private var completion: CheckedContinuation<UIImage?, Never>?
    private var deadline: Task<Void, Never>?
    private var generation = UUID()
    private let maxBytes = 24 * 1024 * 1024

    func cached(_ key: Key) -> UIImage? { cache[key]?.image }

    func image(_ fence: MarkdownFence, dark: Bool, width: CGFloat) async -> UIImage? {
        guard width.isFinite, width > 0,
              MarkdownFenceRegistry.renderer(language: fence.language, source: fence.source, closed: true, streaming: false) != nil else { return nil }
        let key = Key(fence.source, dark: dark, width: width)
        if let entry = cache[key] { touch(key); return entry.image }
        if let job = jobs[key] { return await job.value }
        guard jobs.count < 12 else { return nil }
        let previous = tail
        let job = Task { @MainActor [weak self] in
            await previous?.value
            guard let self else { return nil as UIImage? }
            let image = await self.render(fence.source, key: key)
            // A render that timed out or was interrupted is tried again next time. Only a
            // diagram that cannot be drawn is remembered as failed.
            if image != nil || self.lastFailure == "javascript-or-size" { self.store(image, key: key) }
            return image
        }
        jobs[key] = job
        tail = Task { _ = await job.value }
        let image = await job.value
        jobs[key] = nil
        return image
    }

    private func touch(_ key: Key) { order.removeAll { $0 == key }; order.append(key) }
    private func store(_ image: UIImage?, key: Key) {
        let cost = image?.cgImage.map { $0.bytesPerRow * $0.height } ?? 0
        guard cost <= maxBytes else { return }
        cache[key] = Entry(image: image, cost: cost); touch(key)
        while order.count > 32 || cache.values.reduce(0, { $0 + $1.cost }) > maxBytes {
            if let first = order.first { cache[first] = nil; order.removeFirst() }
        }
    }

    func clearCache() { cache.removeAll(); order.removeAll() }

    #if DEBUG
    /// Inspect actual SVG line layout without retaining or exporting diagram contents.
    func hasUnbrokenLabel(_ label: String) async -> Bool {
        guard let web else { return false }
        let result = try? await web.callAsyncJavaScript(
            "return [...document.querySelectorAll('#diagram svg text tspan')].some(node => !node.querySelector('tspan') && node.textContent === label);",
            arguments: ["label": label], in: nil, contentWorld: .page)
        return result as? Bool == true
    }
    #endif

    private func prepare(token: UUID) async -> Bool {
        guard generation == token else { return false }
        if loaded, web != nil { return true }
        guard let assets = MermaidAssets(),
              let rule = try? await WKContentRuleListStore.default().compileContentRuleList(forIdentifier: "togo-diagram-offline-v1", encodedContentRuleList: MermaidAssets.rules) else { lastFailure = "assets-or-policy"; return false }
        guard generation == token else { return false }
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .nonPersistent()
        configuration.setURLSchemeHandler(assets, forURLScheme: "togo-diagram")
        configuration.userContentController.add(rule)
        let view = WKWebView(frame: CGRect(x: 0, y: 0, width: 800, height: 600), configuration: configuration)
        view.navigationDelegate = self
        view.isUserInteractionEnabled = false
        view.isOpaque = false
        view.scrollView.isScrollEnabled = false
        web = view
        return await withCheckedContinuation { continuation in
            ready = continuation
            view.load(URLRequest(url: MermaidAssets.page))
        }
    }

    private func render(_ source: String, key: Key) async -> UIImage? {
        let token = UUID(); generation = token; lastFailure = nil
        armDeadline(token, duration: .seconds(8), phase: "bootstrap")
        guard await prepare(token: token), generation == token, let web else { deadline?.cancel(); return nil }
        armDeadline(token, duration: .seconds(4), phase: "render")
        let traits = UITraitCollection(userInterfaceStyle: key.dark ? .dark : .light)
        func hex(_ color: UIColor) -> String {
            var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
            color.resolvedColor(with: traits).getRed(&r, green: &g, blue: &b, alpha: &a)
            return String(format: "#%02x%02x%02x", Int(r * 255), Int(g * 255), Int(b * 255))
        }
        let colors = ["background": hex(.togoDeepest), "text": hex(.togoSnow), "fill": hex(.togoRaised), "line": hex(.togoMute)]
        return await withCheckedContinuation { (continuation: CheckedContinuation<UIImage?, Never>) in
            completion = continuation
            web.frame.size = CGSize(width: CGFloat(key.width), height: 1200)
            web.callAsyncJavaScript(Self.draw, arguments: ["source": source, "dark": key.dark, "colors": colors], in: nil, in: .page) { [weak self, weak web] result in
                guard let self, self.generation == token, let web else { return }
                guard case .success(let value) = result, let size = value as? [String: Double],
                      let width = size["width"], let height = size["height"], width > 0, height > 0,
                      width <= 1600, height <= 2400 else { self.lastFailure = "javascript-or-size"; self.finish(nil, token: token); return }
                web.frame.size = CGSize(width: width, height: height)
                let snapshot = WKSnapshotConfiguration()
                snapshot.rect = CGRect(x: 0, y: 0, width: width, height: height)
                let pixels = min(1600, max(width, Double(key.width) * 2), sqrt(4_000_000 * width / height), 4096 * width / height)
                snapshot.snapshotWidth = NSNumber(value: pixels / Double(max(1, web.traitCollection.displayScale)))
                web.takeSnapshot(with: snapshot) { [weak self] image, _ in
                    if image == nil { self?.lastFailure = "snapshot" }
                    self?.finish(image, token: token)
                }
            }
        }
    }

    private func armDeadline(_ token: UUID, duration: Duration, phase: String) {
        deadline?.cancel()
        deadline = Task { @MainActor [weak self] in
            try? await Task.sleep(for: duration)
            guard !Task.isCancelled, self?.generation == token else { return }
            self?.lastFailure = "timeout-" + phase
            self?.reset()
        }
    }

    private func finish(_ image: UIImage?, token: UUID) {
        guard generation == token else { return }
        deadline?.cancel(); deadline = nil
        let reply = completion; completion = nil; reply?.resume(returning: image)
    }
    private func reset() {
        generation = UUID(); loaded = false
        web?.stopLoading(); web?.navigationDelegate = nil; web = nil
        let pending = ready; ready = nil; pending?.resume(returning: false)
        let reply = completion; completion = nil; reply?.resume(returning: nil)
    }
    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        guard webView === web else { return }
        loaded = true; let pending = ready; ready = nil; pending?.resume(returning: true)
    }
    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: any Error) { if webView === web { reset() } }
    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: any Error) { if webView === web { reset() } }
    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) { if webView === web { reset() } }
    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction) async -> WKNavigationActionPolicy {
        action.request.url == MermaidAssets.page && !loaded && action.targetFrame?.isMainFrame == true ? .allow : .cancel
    }

    private static let draw = #"""
    if (source.length > 16384 || source.includes('%%{') || source.trimStart().startsWith('---')) return null;
    mermaid.initialize({startOnLoad:false,securityLevel:'strict',htmlLabels:false,suppressErrorRendering:true,
      maxTextSize:16384,maxEdges:300,theme:'base',darkMode:dark,fontFamily:'-apple-system, sans-serif',
      flowchart:{htmlLabels:false,useMaxWidth:false,wrappingWidth:320},
      themeVariables:{background:colors.background,primaryColor:colors.fill,primaryTextColor:colors.text,
        primaryBorderColor:colors.line,lineColor:colors.line,secondaryColor:colors.fill,tertiaryColor:colors.fill},
      secure:['secure','securityLevel','startOnLoad','htmlLabels','flowchart','theme','themeVariables','fontFamily','maxTextSize','maxEdges','suppressErrorRendering']});
    const host = document.getElementById('diagram'); host.replaceChildren();
    const result = await mermaid.render('togo_diagram',source);
    const dom = new DOMParser().parseFromString(result.svg,'image/svg+xml');
    const svg = dom.documentElement;
    if (svg.nodeName.toLowerCase() !== 'svg' || svg.querySelector('script,foreignObject,image,iframe,object,embed')) return null;
    for (const node of [svg,...svg.querySelectorAll('*')]) {
      for (const attr of [...node.attributes]) {
        if (/^on/i.test(attr.name) || /href/i.test(attr.name)) node.removeAttribute(attr.name);
      }
    }
    const box = svg.viewBox.baseVal;
    const naturalWidth = Math.ceil(box.width+16), naturalHeight = Math.ceil(box.height+16);
    if (!Number.isFinite(naturalWidth) || !Number.isFinite(naturalHeight) || naturalWidth<1 || naturalHeight<1 || naturalWidth>16000 || naturalHeight>24000) return null;
    const scale = Math.min(1,1600/naturalWidth,2400/naturalHeight);
    const width = Math.ceil(naturalWidth*scale), height = Math.ceil(naturalHeight*scale);
    svg.style.maxWidth='none'; svg.setAttribute('width',width); svg.setAttribute('height',height);
    document.body.style.background=colors.background;
    host.append(document.importNode(svg,true));
    await document.fonts.ready;
    host.firstElementChild.getBoundingClientRect();
    return {width,height};
    """#
}
