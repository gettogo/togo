import Foundation

/// The steps of one web sign-in in the system's authentication browser
/// (`ASWebAuthenticationSession`), with the phone-side forward that carries a localhost
/// callback to the machine. Pure: events in, effects out, so the order is tested.
///
/// A page that returns to the machine first gets its forward, listening on the same port the
/// redirect names; only then does the browser open, so the redirect can never arrive early.
/// The browser can't recognise an `http://localhost` callback by itself (see
/// `WebAuthSession`), so the flow ends it once the forward has carried the callback, or once
/// the agent lists the provider as set up.
public struct WebAuthFlow: Equatable, Sendable {
    public enum Phase: Equatable, Sendable {
        /// Opening the forward for the callback.
        case preparing
        /// The browser is up.
        case browsing
        /// The callback went through the forward to the machine; the browser closes shortly,
        /// after the agent's own "signed in" page had a moment to show.
        case delivered
        case signedIn
        /// The callback can't come back on its own (the port is taken on the phone, or the
        /// browser closed first): the provider's code is pasted instead.
        case needsCode
        /// Closed before signing in, with no code to fall back on.
        case cancelled
        case failed(String)
    }

    public enum Event: Equatable, Sendable {
        case forwardReady
        /// The port is in use on this phone, or iOS refuses it.
        case forwardBusy
        /// The forward could not be kept, even after reconnecting.
        case forwardLost(String)
        case browserEnded
        /// A request came in on the forward.
        case callbackSeen
        /// The delay after the callback passed.
        case settled
        /// The agent lists the provider as set up.
        case providerSetUp
        /// A pasted code went into the session.
        case codeSent
        /// Open the page again.
        case reopen
    }

    public enum Effect: Equatable, Sendable {
        case openForward
        case openBrowser
        case closeBrowser
        case stopForward
        /// Send `.settled` after a short pause.
        case settleLater
    }

    public private(set) var phase: Phase = .preparing
    /// The redirect lands on the machine, so a forward goes first.
    public let needsForward: Bool
    public let offersCode: Bool
    /// The redirect names the machine on a port a phone can't listen on (below 1024): only a
    /// pasted code finishes it. The browser still opens, to show that code.
    public let codeOnly: Bool
    /// The browser is up now.
    public private(set) var browserOpen = false

    /// `forwardless`: nothing can carry a callback to the machine here.
    public init(_ page: SignInURL, forwardless: Bool = false) {
        needsForward = page.forwardablePort != nil && !forwardless
        codeOnly = page.returnsToMachine && !needsForward
        offersCode = page.offersCode || codeOnly
    }

    /// The first effects: the forward, else the browser at once.
    public mutating func start() -> [Effect] {
        phase = .preparing
        if needsForward { return [.openForward] }
        return open(as: codeOnly ? .needsCode : .browsing)
    }

    public var isOver: Bool {
        switch phase {
        case .signedIn, .cancelled, .failed: true
        default: false
        }
    }

    public mutating func handle(_ event: Event) -> [Effect] {
        switch (event, phase) {
        case (.forwardReady, .preparing):
            return open(as: .browsing)
        case (.forwardBusy, .preparing):
            // The browser still opens: the provider's page can show the code to paste.
            return open(as: .needsCode)
        case (.forwardLost(let message), .browsing), (.forwardLost(let message), .preparing):
            if offersCode { phase = .needsCode; return [] }
            phase = .failed(message)
            return close() + [.stopForward]
        case (.callbackSeen, .browsing), (.callbackSeen, .needsCode):
            phase = .delivered
            return [.settleLater]
        case (.settled, .delivered):
            return finish()
        case (.providerSetUp, _), (.codeSent, .needsCode), (.codeSent, .browsing):
            guard !isOver else { return [] }
            return finish()
        case (.browserEnded, .browsing), (.browserEnded, .preparing):
            browserOpen = false
            phase = offersCode ? .needsCode : .cancelled
            return offersCode ? [] : [.stopForward]
        case (.browserEnded, .delivered):
            browserOpen = false
            return finish()
        case (.browserEnded, _):
            browserOpen = false
            return []
        case (.reopen, .needsCode), (.reopen, .cancelled), (.reopen, .failed):
            phase = .preparing
            return needsForward ? [.stopForward, .openForward] : open(as: .browsing)
        default:
            return []
        }
    }

    private mutating func open(as next: Phase) -> [Effect] {
        phase = next
        guard !browserOpen else { return [] }
        browserOpen = true
        return [.openBrowser]
    }

    private mutating func close() -> [Effect] {
        guard browserOpen else { return [] }
        browserOpen = false
        return [.closeBrowser]
    }

    private mutating func finish() -> [Effect] {
        phase = .signedIn
        return close() + [.stopForward]
    }
}
