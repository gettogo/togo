import Foundation
import Observation
import TogoKit
import TogoTerminal

/// A page an agent asked its machine to open, waiting for the user on this device.
struct PendingOpen: Identifiable, Equatable, Sendable {
    let id: UUID
    let hostID: HostID
    /// The session whose agent asked, when the helper knows it.
    let sessionID: String?
    let url: URL
    /// The agent's name as the app shows it, when known.
    let agent: String?
    /// When the helper published the request.
    let at: Date

    var tabID: TabID? { sessionID.map { TabID(hostID: hostID, sessionID: $0) } }
    var origin: LinkCenter.Origin { .init(hostID: hostID, sessionID: sessionID) }
}

/// Every page agents asked to open, held until the user uses one or it expires, so a request
/// whose sheet was missed or swept away stays reachable: in the chat as a banner and in the
/// roster as "Needs you".
///
/// A screen that is waiting for a page (the picker's "Sign in to Anthropic") claims requests
/// from its session, and shows them itself instead of a sheet over it.
@MainActor @Observable final class OpenRequests {
    /// How a new request reaches the user.
    enum Route: Equatable {
        /// A screen waiting for it took it.
        case claimed
        /// Ask with a sheet over whatever shows.
        case present
        /// Too old to put a sheet up for; it waits in the chat and the roster.
        case hold
    }

    /// Requests younger than this get a sheet; older ones only wait in the chat and roster.
    static let freshness: TimeInterval = 120
    /// Requests are dropped after this: the agent has stopped listening for the sign-in.
    static let lifetime: TimeInterval = 600

    private(set) var pending: [PendingOpen] = []

    /// Request ids already seen, so a snapshot read twice does not ask twice.
    @ObservationIgnored private var seen: [UUID] = []
    @ObservationIgnored private var waiters: [Waiter] = []
    @ObservationIgnored private var sweep: Task<Void, Never>?

    private struct Waiter {
        let hostID: HostID
        let sessionID: String?
        weak var owner: AnyObject?
        let deliver: @MainActor (PendingOpen) -> Void

        func takes(_ open: PendingOpen) -> Bool {
            guard owner != nil, open.hostID == hostID else { return false }
            guard let asked = open.sessionID, let sessionID else { return true }
            return asked == sessionID
        }
    }

    // MARK: Requests

    /// Takes in a request from a snapshot. Nil when it was seen before, is not a web page, or
    /// is past its lifetime.
    func receive(_ request: BrowserOpen, hostID: HostID, agent: String?, now: Date = Date()) -> (open: PendingOpen, route: Route)? {
        guard !seen.contains(request.id) else { return nil }
        seen.append(request.id)
        if seen.count > 128 { seen.removeFirst(seen.count - 128) }
        let age = now.timeIntervalSince(request.at)
        guard age < Self.lifetime, let url = URL(string: request.url), LinkPolicy.allows(url) else { return nil }
        expire(now: now)
        let open = PendingOpen(
            id: request.id, hostID: hostID, sessionID: request.sessionID, url: url, agent: agent, at: request.at
        )
        // A repeat of the same page from the same session replaces the older one.
        pending.removeAll { $0.hostID == hostID && $0.sessionID == open.sessionID && $0.url == url }
        pending.append(open)
        scheduleSweep()
        if let waiter = waiters.last(where: { $0.takes(open) }) {
            waiter.deliver(open)
            return (open, .claimed)
        }
        return (open, age < Self.freshness ? .present : .hold)
    }

    /// The user used the request (opened it here or on the Mac), or dismissed it.
    func resolve(_ id: UUID) {
        pending.removeAll { $0.id == id }
    }

    func isPending(_ id: UUID) -> Bool {
        pending.contains { $0.id == id }
    }

    /// Drops requests past their lifetime.
    func expire(now: Date = Date()) {
        let kept = pending.filter { now.timeIntervalSince($0.at) < Self.lifetime }
        if kept.count != pending.count { pending = kept }
    }

    /// The requests waiting in one session's chat, oldest first.
    func pending(for tab: TabID) -> [PendingOpen] {
        pending.filter { $0.tabID == tab }
    }

    /// Sessions with a request waiting, for the roster's "Needs you".
    var waitingTabs: Set<TabID> {
        Set(pending.compactMap(\.tabID))
    }

    /// Requests that name no session; the roster counts them on their own.
    var unlisted: Int {
        pending.filter { $0.sessionID == nil }.count
    }

    // MARK: Waiting screens

    /// A screen starts waiting for a page from `sessionID` on `hostID`. Requests that arrive
    /// go to `deliver` instead of a sheet while `owner` lives. Returns the newest such request
    /// already waiting, which the screen shows at once.
    @discardableResult
    func wait(
        hostID: HostID, sessionID: String?, owner: AnyObject, deliver: @escaping @MainActor (PendingOpen) -> Void
    ) -> PendingOpen? {
        waiters.removeAll { $0.owner == nil || $0.owner === owner }
        let waiter = Waiter(hostID: hostID, sessionID: sessionID, owner: owner, deliver: deliver)
        waiters.append(waiter)
        return pending.last { waiter.takes($0) && Date().timeIntervalSince($0.at) < Self.freshness }
    }

    func stopWaiting(_ owner: AnyObject) {
        waiters.removeAll { $0.owner == nil || $0.owner === owner }
    }

    private func scheduleSweep() {
        guard let oldest = pending.map(\.at).min() else { return }
        let due = max(oldest.addingTimeInterval(Self.lifetime).timeIntervalSinceNow, 1)
        sweep?.cancel()
        sweep = Task { [weak self] in
            try? await Task.sleep(for: .seconds(due))
            guard !Task.isCancelled, let self else { return }
            self.expire()
            self.scheduleSweep()
        }
    }
}
