import Foundation

/// A sign-in step an agent waits on, read off its screen by the helper: the page to open,
/// a device code to enter or confirm there, and whether it waits for a pasted secret (an API
/// key, or the authorization code the page shows). A secret answer is a text decision the
/// helper pastes into the pane; the app never stores or logs it.
public struct SignInStep: Codable, Hashable, Sendable {
    /// The page, when the screen shows one. Only http and https pages are kept.
    public var url: URL?
    public var code: String?
    public var secret: Bool

    public init(url: URL? = nil, code: String? = nil, secret: Bool = false) {
        self.url = url
        self.code = code
        self.secret = secret
    }

    private enum CodingKeys: String, CodingKey { case url, code, secret }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let raw = try container.decodeIfPresent(String.self, forKey: .url)
        url = raw.flatMap(URL.init(string:)).flatMap { url in
            let scheme = url.scheme?.lowercased()
            return (scheme == "http" || scheme == "https") && url.host() != nil ? url : nil
        }
        let code = try container.decodeIfPresent(String.self, forKey: .code)
        self.code = code.flatMap { $0.isEmpty || $0.count > 16 ? nil : $0 }
        secret = try container.decodeIfPresent(Bool.self, forKey: .secret) ?? false
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeIfPresent(url?.absoluteString, forKey: .url)
        try container.encodeIfPresent(code, forKey: .code)
        if secret { try container.encode(true, forKey: .secret) }
    }
}
