import AuthenticationServices
import UIKit

/// Shows a sign-in page in the system's authentication browser.
@MainActor protocol WebAuthPresenting: AnyObject {
    /// Opens `url`. `ended` runs once, when the browser closes for any reason.
    /// False when it could not open.
    func open(_ url: URL, ended: @escaping @MainActor () -> Void) -> Bool
    /// Closes it, as after the callback reached the machine.
    func close()
}

/// `ASWebAuthenticationSession`, as Apple asks apps to sign in with: the page runs in Safari's
/// own engine, which providers such as Google accept where they refuse an embedded web view
/// (`disallowed_useragent`), and it shares Safari's cookies (not ephemeral), so a provider the
/// user is already signed in to in Safari knows them.
///
/// The callback: these sign-ins return to `http://localhost:PORT/…`. The session's own
/// matching can't catch that. A custom scheme never appears in an http redirect, and
/// `.https(host:path:)` (iOS 17.4+) needs an associated domain, which `localhost` can't have,
/// and covers https only. So the session is given a custom scheme nothing ever redirects to
/// (`togo-signin`), lets the redirect load like any page (it reaches the agent through the
/// phone-side forward), and is cancelled from code once the forward has carried the callback
/// or the agent lists the provider as set up.
@MainActor final class WebAuthSession: NSObject, WebAuthPresenting, ASWebAuthenticationPresentationContextProviding {
    static let placeholderScheme = "togo-signin"

    private var session: ASWebAuthenticationSession?
    private var ended: (@MainActor () -> Void)?

    func open(_ url: URL, ended: @escaping @MainActor () -> Void) -> Bool {
        close()
        guard UIApplication.shared.connectedScenes.contains(where: { $0 is UIWindowScene }) else { return false }
        self.ended = ended
        let session = ASWebAuthenticationSession(
            url: url, callback: .customScheme(Self.placeholderScheme)
        ) { [weak self] _, _ in
            // A cancel from code, the user's Cancel and a (never expected) callback all end here.
            Task { @MainActor in self?.finished() }
        }
        session.prefersEphemeralWebBrowserSession = false
        session.presentationContextProvider = self
        self.session = session
        guard session.start() else {
            self.session = nil
            self.ended = nil
            return false
        }
        return true
    }

    func close() {
        session?.cancel()
    }

    private func finished() {
        session = nil
        let ended = ended
        self.ended = nil
        ended?()
    }

    nonisolated func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        MainActor.assumeIsolated { Self.anchor() }
    }

    /// The key window of the scene in front, else any window of an active scene.
    static func anchor() -> ASPresentationAnchor {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        let front = scenes.filter { $0.activationState == .foregroundActive } + scenes
        for scene in front {
            if let key = scene.keyWindow { return key }
            if let window = scene.windows.first { return window }
        }
        // `open` refuses to start without a scene, so there is one by now.
        guard let scene = front.first else { preconditionFailure("A sign-in opens from a window") }
        return UIWindow(windowScene: scene)
    }
}
