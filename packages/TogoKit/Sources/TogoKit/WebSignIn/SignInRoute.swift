import Foundation

/// Which pages sign in, and where their redirect returns.
public enum SignInRoute {
    public static let loopbackHosts: Set<String> = ["localhost", "127.0.0.1", "::1", "[::1]"]

    public static func isLoopback(_ url: URL) -> Bool {
        guard let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https",
              let host = url.host(percentEncoded: false)?.lowercased() else { return false }
        return loopbackHosts.contains(host) || host.hasSuffix(".localhost")
    }

    /// A page on the machine's loopback address, or a sign-in page whose redirect goes there,
    /// such as `redirect_uri=http://localhost:54545/callback`.
    public static func returnsToLoopback(_ url: URL) -> Bool {
        if isLoopback(url) { return true }
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        return items.contains { item in
            guard let value = item.value, let inner = URL(string: value) else { return false }
            return isLoopback(inner)
        }
    }

    /// The port on the machine a sign-in returns to: the page's own when it is on the loopback
    /// address, else its redirect's (`redirect_uri=http://localhost:54545/callback` → 54545),
    /// with the scheme's default when the address names none. Nil when it returns elsewhere.
    public static func callbackPort(_ url: URL) -> Int? {
        if isLoopback(url) { return port(of: url) }
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        for item in items {
            guard let value = item.value, let inner = URL(string: value), isLoopback(inner) else { continue }
            return port(of: inner)
        }
        return nil
    }

    /// A page that signs in: it returns to the machine, or its address reads as an OAuth,
    /// device or login page. Those go to the authentication browser; other pages to Safari's view.
    public static func isSignIn(_ url: URL) -> Bool {
        if returnsToLoopback(url) { return true }
        let path = url.path.lowercased()
        let markers = ["oauth", "authorize", "/login", "/signin", "/sign-in", "/device", "/auth/", "/activate"]
        if markers.contains(where: path.contains) { return true }
        let names = Set((URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []).map { $0.name.lowercased() })
        return !names.isDisjoint(with: ["client_id", "redirect_uri", "callback_url", "user_code", "code_challenge"])
    }

    private static func port(of url: URL) -> Int {
        url.port ?? (url.scheme?.lowercased() == "https" ? 443 : 80)
    }
}
