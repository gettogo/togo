import Foundation

/// A `togo://` link: `togo://session/<bib>?host=<name>` opens a session and
/// `togo://new?host=<name>` starts one. Links come from anywhere, so anything else is refused.
enum DeepLink: Equatable, Sendable {
    case session(bib: String, host: String?)
    case newSession(host: String?)

    static let scheme = "togo"
    /// Bibs are short numbers; the bound keeps a hostile link from putting a novel in the notice.
    private static let maxBibLength = 8

    init?(url: URL) {
        guard url.scheme?.lowercased() == Self.scheme,
              let parts = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return nil }
        let host = parts.queryItems?.first { $0.name == "host" }?.value?
            .trimmingCharacters(in: .whitespacesAndNewlines)
        let machine = host?.isEmpty == false ? host : nil
        let path = url.pathComponents.filter { $0 != "/" }
        switch parts.host?.lowercased() {
        case "session":
            guard path.count == 1, let bib = path.first, !bib.isEmpty, bib.count <= Self.maxBibLength,
                  bib.allSatisfy({ $0.isASCII && ($0.isLetter || $0.isNumber) }) else { return nil }
            self = .session(bib: bib, host: machine)
        case "new":
            guard path.isEmpty else { return nil }
            self = .newSession(host: machine)
        default:
            return nil
        }
    }
}

extension AppStore {
    /// Opens what the link names. A session link that arrives before its machine has reported
    /// waits for that report, so a cold launch from a notification does not call a live
    /// session ended.
    func open(_ link: DeepLink) {
        switch link {
        case .session:
            pendingLink = link
            resolvePendingLink()
            // A machine stuck connecting must not pull the user into the tab much later.
            Task { [weak self] in
                try? await Task.sleep(for: Self.linkPatience)
                if self?.pendingLink == link { self?.pendingLink = nil }
            }
        case .newSession(let host):
            guard !services.hosts.isEmpty else { return }
            let hostID = host.flatMap { name in services.hosts.first { Self.sameName($0.name, name) }?.id }
            showSheet(.newSession(hostID))
        }
    }

    /// Called after every snapshot. A pending session link resolves once every machine it
    /// could be on has connected or given up.
    func resolvePendingLink() {
        guard case .session(let bib, let host)? = pendingLink else { return }
        let machines = controlCenter.machines.filter { machine in host.map { Self.sameName(machine.name, $0) } ?? true }
        if let row = machines.flatMap(\.sessions).first(where: { $0.bib == bib }) {
            pendingLink = nil
            sheet = nil
            open(hostID: row.hostID, sessionID: row.id)
            return
        }
        let known = host.map { name in services.hosts.contains { Self.sameName($0.name, name) } } ?? true
        let expected = host == nil ? services.hosts.count : 1
        let settled = machines.filter { $0.reachability != .connecting && $0.reachability != .waking }
        if known && settled.count < expected { return }
        pendingLink = nil
        showControlCenter()
        notice = Notice(text: Self.endedNotice(bib), isError: false)
    }

    static let linkPatience: Duration = .seconds(20)

    static func endedNotice(_ bib: String) -> String {
        String(localized: "cc.link.ended", defaultValue: "Session \(bib) has ended.")
    }

    private static func sameName(_ a: String, _ b: String) -> Bool {
        a.compare(b, options: [.caseInsensitive, .diacriticInsensitive]) == .orderedSame
    }
}
