import CryptoKit
import Foundation
import WebKit

/// This scheme has precisely two immutable resources. All other resource requests fail.
final class MermaidAssets: NSObject, WKURLSchemeHandler {
    static let page = URL(string: "togo-diagram://bundle/index.html")!
    nonisolated let script: Data
    nonisolated let pageData: Data

    init?(bundle: Bundle = .main) {
        func resource(_ name: String, _ ext: String) -> Data? {
            guard let url = bundle.url(forResource: name, withExtension: ext)
                ?? bundle.url(forResource: name, withExtension: ext, subdirectory: "Mermaid") else { return nil }
            return try? Data(contentsOf: url)
        }
        guard let script = resource("mermaid.min", "js"), let page = resource("mermaid-render", "html"),
              SHA256.hash(data: script).map({ String(format: "%02x", $0) }).joined() == "336df803d87a38b70a01e736e666c8f7601d2ce42f5e47c8fafe5d721074b9a3" else { return nil }
        self.script = script
        pageData = page
    }

    nonisolated func webView(_ webView: WKWebView, start task: any WKURLSchemeTask) {
        guard let url = task.request.url, url.scheme == "togo-diagram", url.host == "bundle", url.query == nil else {
            task.didFailWithError(URLError(.resourceUnavailable)); return
        }
        let data: Data
        let mime: String
        switch url.path {
        case "/index.html": data = pageData; mime = "text/html"
        case "/mermaid.js": data = script; mime = "application/javascript"
        default: task.didFailWithError(URLError(.resourceUnavailable)); return
        }
        task.didReceive(URLResponse(url: url, mimeType: mime, expectedContentLength: data.count, textEncodingName: "utf-8"))
        task.didReceive(data)
        task.didFinish()
    }

    nonisolated func webView(_ webView: WKWebView, stop task: any WKURLSchemeTask) {}

    static let rules = #"""
    [{"trigger":{"url-filter":".*"},"action":{"type":"block"}},
     {"trigger":{"url-filter":"^togo-diagram://bundle/index\\.html$"},"action":{"type":"ignore-previous-rules"}},
     {"trigger":{"url-filter":"^togo-diagram://bundle/mermaid\\.js$"},"action":{"type":"ignore-previous-rules"}}]
    """#
}
