import Foundation
import TogoKit

/// Local pages belong to a session's port preview. Their first page load is never an OAuth
/// callback, even when the page's path contains login or auth.
enum SessionWebLink {
    enum Destination: Equatable { case localPreview, signIn, web }

    static func destination(_ url: URL) -> Destination {
        if SignInRoute.isLoopback(url) { return .localPreview }
        return SignInRoute.isSignIn(url) ? .signIn : .web
    }

    static func previewURL(_ original: URL?, remote: Int, local: URL) -> URL {
        guard let original, SignInRoute.isLoopback(original),
              (original.port ?? (original.scheme?.lowercased() == "https" ? 443 : 80)) == remote,
              var components = URLComponents(url: original, resolvingAgainstBaseURL: false) else { return local }
        components.host = "127.0.0.1"
        components.port = local.port
        return components.url ?? local
    }
}
