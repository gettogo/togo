import Foundation
import TogoKit

/// Where a sign-in started from the phone stands, from the command typed into the session to
/// the agent listing the provider as set up. Pure, so its order is tested.
struct SignInProgress: Equatable {
    enum Phase: Equatable {
        /// The sign-in command is on its way to the session.
        case sending
        /// Typed; the agent has not asked to open its page yet.
        case waiting
        /// The agent's page is here, to open, copy or send to the Mac.
        case link
        /// The page is open in the sign-in browser.
        case browsing
        /// The callback reached the machine, or the page was opened elsewhere; waiting for the
        /// agent to list the provider as set up.
        case confirming
        case signedIn
        case failed(String)
    }

    private(set) var phase: Phase = .sending
    /// The page the agent asked to open, once it has.
    private(set) var open: PendingOpen?

    /// Whether the page should still look for the provider in the agent's list.
    var watching: Bool {
        switch phase {
        case .signedIn, .failed: false
        default: true
        }
    }

    /// The command's result. A failure ends it; anything else waits for the page, unless the
    /// page already came.
    mutating func sent(_ result: HarnessActionResult?) {
        guard phase == .sending else { return }
        guard let result, result.result != .failed, result.result != .fallback else {
            phase = .failed(result?.message ?? "")
            return
        }
        phase = open == nil ? .waiting : .link
    }

    /// The agent asked to open its page. A newer page replaces the one shown, unless the
    /// sign-in is already over.
    mutating func arrived(_ page: PendingOpen) {
        guard phase != .signedIn else { return }
        open = page
        if case .failed = phase { phase = .link; return }
        if phase != .browsing, phase != .confirming { phase = .link }
    }

    /// The page opened in the sign-in browser on this screen.
    mutating func browsing() {
        guard open != nil, phase == .link || phase == .confirming else { return }
        phase = .browsing
    }

    /// The page opened somewhere this screen does not see: Safari's view or the Mac.
    mutating func openedElsewhere() {
        guard open != nil, phase == .link || phase == .browsing else { return }
        phase = .confirming
    }

    /// The machine took the browser's callback.
    mutating func callbackDelivered() {
        guard phase != .signedIn else { return }
        phase = .confirming
    }

    /// The browser closed before the callback: the page is offered again.
    mutating func browserClosed() {
        if phase == .browsing { phase = .link }
    }

    /// The agent's list was read again: `setUp` is whether the provider has left "Not set up".
    mutating func listed(setUp: Bool) {
        guard setUp, watching else { return }
        phase = .signedIn
    }

    /// Trying the command again after a failure.
    mutating func retry() {
        phase = .sending
        open = nil
    }
}

extension SignInProgress {
    /// Whether `screen` lists `itemID` among the picker's "Not set up" entries. Nil when the
    /// screen is no picker.
    static func stillNotSetUp(_ itemID: String, in screen: HarnessScreen) -> Bool? {
        guard case .picker(let picker) = PrimitiveScreenLogic.payload(screen) else { return nil }
        return (picker.setup ?? []).contains { $0.id == itemID }
    }
}
