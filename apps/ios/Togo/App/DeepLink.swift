import Foundation
import TogoKit

/// A `togo://` link: `togo://session/<bib>?host=<name>` opens a session and
/// `togo://new?host=<name>` starts one, optionally with `folder=<path>` and `agent=<preset id>`
/// filled in on the sheet. New links carry `hostID`; request links may also carry `sessionID`.
/// Names and bibs remain supported only when their target is unambiguous. Links come from anywhere, so anything else is refused, and a folder
/// or agent that does not check out is dropped; nothing starts until the user taps Start.
enum DeepLink: Equatable, Sendable {
    case session(bib: String, host: String?, hostID: UUID? = nil, sessionID: String? = nil)
    case newSession(host: String?, folder: String? = nil, preset: UUID? = nil, hostID: UUID? = nil)

    static let scheme = "togo"
    /// Bibs are short numbers; the bound keeps a hostile link from putting a novel in the notice.
    private static let maxBibLength = 8

    init?(url: URL) {
        guard url.scheme?.lowercased() == Self.scheme,
              let parts = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return nil }
        func item(_ name: String) -> String? { parts.queryItems?.first { $0.name == name }?.value }
        let host = item("host")?.trimmingCharacters(in: .whitespacesAndNewlines)
        let machine = host?.isEmpty == false ? host : nil
        let hostID = item("hostID").flatMap(UUID.init(uuidString:))
        guard item("hostID") == nil || hostID != nil else { return nil }
        let sessionID = item("sessionID")
        if let sessionID {
            guard !sessionID.isEmpty, sessionID.utf8.count <= 256,
                  !sessionID.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains) else { return nil }
        }
        let path = url.pathComponents.filter { $0 != "/" }
        switch parts.host?.lowercased() {
        case "session":
            guard path.count == 1, let bib = path.first, !bib.isEmpty, bib.count <= Self.maxBibLength,
                  bib.allSatisfy({ $0.isASCII && ($0.isLetter || $0.isNumber) }) else { return nil }
            self = .session(bib: bib, host: machine, hostID: hostID, sessionID: sessionID)
        case "new":
            guard path.isEmpty else { return nil }
            self = .newSession(
                host: machine, folder: item("folder").flatMap(NewSessionPrefill.folder),
                preset: item("agent").flatMap(UUID.init(uuidString:)), hostID: hostID
            )
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
        clearPendingLink()
        newSessionPrefill = nil
        linkGeneration = UUID()
        pendingLink = link
        resolvePendingLink()
        guard pendingLink != nil else { return }
        let generation = linkGeneration
        pendingLinkTask = Task { [weak self] in
            do { try await Task.sleep(for: Self.linkPatience) } catch { return }
            self?.expirePendingLink(generation: generation)
        }
    }

    func clearPendingLink() {
        pendingLinkTask?.cancel()
        pendingLinkTask = nil
        pendingLink = nil
    }

    func expirePendingLink(generation: UUID) {
        guard generation == linkGeneration, pendingLink != nil else { return }
        clearPendingLink()
        notice = Notice(text: String(localized: "cc.link.expired",
            defaultValue: "The machine is still loading or connecting. Open Sessions and try again."), isError: false)
    }

    private func openNewSessionLink(host: String?, folder: String?, preset: UUID?, hostID requestedID: UUID?) {
        // With no machine set up there is nowhere to start one; first-run setup stays on screen.
        guard !services.hosts.isEmpty else {
            clearPendingLink()
            return
        }
        let candidates = matchingLinkHosts(name: host, id: requestedID)
        if requestedID != nil, candidates.isEmpty {
            finishUnavailableLink()
            return
        }
        let hostID = candidates.count == 1 ? candidates[0].id : nil
        if host != nil, candidates.count > 1 { notice = Notice(text: Self.ambiguousLinkNotice, isError: false) }
        let prefill = hostID.flatMap { id in
            folder == nil && preset == nil ? nil : NewSessionPrefill(hostID: id, folder: folder, presetID: preset)
        }
        newSessionPrefill = prefill
        clearPendingLink()
        let target = AppSheet.newSession(hostID)
        guard sheet == target, prefill != nil else {
            showSheet(target)
            return
        }
        // The same sheet is up already; it opens again so it starts from the prefill.
        sheet = nil
        let generation = linkGeneration
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: Self.coverDismissal)
            guard let self, self.linkGeneration == generation, self.sheet == nil else { return }
            self.showSheet(target)
        }
    }

    /// The prefill for a new-session sheet on `hostID`, once: the sheet that reads it takes it,
    /// and a sheet for another machine, or one on its way out, leaves it.
    func takeNewSessionPrefill(for hostID: HostID) -> NewSessionPrefill? {
        guard let prefill = newSessionPrefill, prefill.hostID == hostID, sheet == .newSession(hostID) else { return nil }
        newSessionPrefill = nil
        return prefill
    }

    /// Called after every snapshot. A pending session link resolves once every machine it
    /// could be on has connected or given up.
    func resolvePendingLink() {
        guard let pendingLink, services.hostsLoaded else { return }
        guard case .session(let bib, let host, let hostID, let sessionID) = pendingLink else {
            if case .newSession(let host, let folder, let preset, let hostID) = pendingLink {
                openNewSessionLink(host: host, folder: folder, preset: preset, hostID: hostID)
            }
            return
        }
        let candidates = matchingLinkHosts(name: host, id: hostID)
        if hostID != nil, candidates.isEmpty { finishUnavailableLink(); return }
        let ids = Set(candidates.map(\.id))
        let machines = controlCenter.machines.filter { ids.contains($0.id) }
        let matches = machines.flatMap(\.sessions).filter { row in sessionID.map { row.id == $0 } ?? (row.bib == bib) }
        let settled = machines.filter { $0.reachability != .connecting && $0.reachability != .waking }
        if settled.count < candidates.count { return }
        // A missing report from an offline candidate cannot establish uniqueness for a
        // legacy link. Stable identities remove that ambiguity without trusting names.
        if matches.count > 1 || (candidates.count > 1 && machines.contains(where: { $0.reachability != .online })) {
            clearPendingLink()
            if sheet?.isForm != true { showControlCenter() }
            notice = Notice(text: Self.ambiguousLinkNotice, isError: false)
            return
        }
        if let row = matches.first {
            clearPendingLink()
            if sheet?.isForm != true { sheet = nil }
            // Links must reveal the detail, including a remembered terminal's request.
            path = []
            if let card = controlCenter.requests.first(where: { $0.tabID == TabID(hostID: row.hostID, sessionID: row.id) && $0.isPending }) {
                openRequest(card)
            } else {
                showSession(TabID(hostID: row.hostID, sessionID: row.id))
                if row.status == .needsYou || (row.mission?.needsYou ?? 0) > 0 {
                    harness(for: TabID(hostID: row.hostID, sessionID: row.id))?.requestNeedsReveal()
                }
            }
            return
        }
        clearPendingLink()
        if sheet?.isForm != true { showControlCenter() }
        notice = Notice(text: Self.endedNotice(bib), isError: false)
    }

    static let linkPatience: Duration = .seconds(20)

    private func matchingLinkHosts(name: String?, id: UUID?) -> [HostRecordView] {
        services.hosts.filter { host in id.map { host.id == $0 } ?? (name.map { Self.sameName(host.name, $0) } ?? true) }
    }

    private func finishUnavailableLink() {
        clearPendingLink()
        notice = Notice(text: String(localized: "cc.link.machineGone",
            defaultValue: "That machine is no longer in Togo. Choose a machine in Sessions."), isError: false)
    }

    static let ambiguousLinkNotice = String(localized: "cc.link.ambiguous",
        defaultValue: "More than one machine could match this session. Choose the machine in Sessions.")

    static func endedNotice(_ bib: String) -> String {
        String(localized: "cc.link.ended", defaultValue: "Session \(bib) has ended.")
    }

    private static func sameName(_ a: String, _ b: String) -> Bool {
        a.compare(b, options: [.caseInsensitive, .diacriticInsensitive]) == .orderedSame
    }
}
