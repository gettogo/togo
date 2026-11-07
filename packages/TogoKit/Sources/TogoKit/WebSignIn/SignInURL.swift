import Foundation

/// What a sign-in page's address says about how its sign-in ends: the machine port its
/// redirect returns to, whether the provider can show a code to paste back instead, and who
/// the provider is. Pure, so every adapter's address shape is tested.
public struct SignInURL: Equatable, Sendable {
    public let url: URL
    /// The port on the machine the redirect returns to (`redirect_uri=http://localhost:54545/…`
    /// → 54545). Nil when it returns elsewhere, as device-code and hosted pages do.
    public let callbackPort: Int?
    /// The provider can show a code to paste into the agent (`code=true`, as Anthropic's
    /// sign-in carries), so a failed callback still has a way through.
    public let offersCode: Bool
    /// "Anthropic", "OpenAI", "GitHub"; the page's host when it names no provider known here.
    public let provider: String

    public init(_ url: URL) {
        self.url = url
        callbackPort = SignInRoute.callbackPort(url)
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        offersCode = items.contains { $0.name == "code" && ["true", "1"].contains($0.value?.lowercased() ?? "") }
        provider = Self.provider(host: url.host(percentEncoded: false) ?? "")
    }

    /// The redirect lands on the machine's loopback address, so it needs the phone-side forward.
    public var returnsToMachine: Bool { callbackPort != nil }

    /// Ports a phone app may listen on. Below 1024 iOS refuses, and the callback can only take
    /// the paste-code way.
    public var forwardablePort: Int? {
        guard let callbackPort, (1024...65_535).contains(callbackPort) else { return nil }
        return callbackPort
    }

    public static func provider(host: String) -> String {
        let host = host.lowercased()
        let known: [(suffix: String, name: String)] = [
            ("claude.ai", "Anthropic"), ("anthropic.com", "Anthropic"), ("claude.com", "Anthropic"),
            ("openai.com", "OpenAI"), ("chatgpt.com", "OpenAI"),
            ("accounts.google.com", "Google"), ("google.com", "Google"),
            ("github.com", "GitHub"), ("cursor.com", "Cursor"), ("ampcode.com", "Amp"),
            ("augmentcode.com", "Augment"), ("moonshot.cn", "Kimi"), ("kimi.com", "Kimi"),
            ("factory.ai", "Factory"), ("openrouter.ai", "OpenRouter"), ("charm.land", "Charm"),
            ("x.ai", "xAI"), ("mistral.ai", "Mistral"),
        ]
        for entry in known where host == entry.suffix || host.hasSuffix("." + entry.suffix) {
            return entry.name
        }
        return host.hasPrefix("www.") ? String(host.dropFirst(4)) : host
    }
}
