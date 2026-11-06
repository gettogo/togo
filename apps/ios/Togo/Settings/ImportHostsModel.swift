import Foundation
import Observation
import TogoKit

/// One host from the pasted config, with what importing it would do.
struct ImportRow: Identifiable, Equatable {
    enum Status: Equatable {
        /// Can be added.
        case ready
        /// A machine with the same host, user and port is already here.
        case alreadyAdded
        /// Cannot be added as written; the text says why.
        case blocked(String)
    }

    var host: ImportedHost
    var status: Status
    /// The machine as the add-machine form would have it, jump host included.
    var draft: MachineDraft

    var id: String { host.alias }

    /// `user@host:port`, leaving out a port of 22.
    var address: String {
        var text = host.user.map { "\($0)@" } ?? ""
        text += host.hostName
        if let port = host.port, port != 22 { text += ":\(port)" }
        return text
    }

    /// The jump as the config wrote it, for the row's second line.
    var jumpText: String? {
        guard let jump = host.jump else { return nil }
        return jump.alias ?? [jump.user.map { "\($0)@" }, jump.host, jump.port.map { ":\($0)" }]
            .compactMap { $0 }.joined()
    }
}

/// Reads SSH config text, lists what it would add and saves the rows the user leaves ticked.
/// Every row passes the same checks as the add-machine form. No key is imported: new machines
/// sign in with this phone's key until the user picks another.
@MainActor @Observable final class ImportHostsModel {
    var text = ""
    private(set) var rows: [ImportRow] = []
    private(set) var skipped: [ImportResult.Skipped] = []
    /// A plain sentence about why there is nothing to show, or nil.
    private(set) var message: String?
    private(set) var reviewed = false
    private(set) var saving = false
    private(set) var imported: Int?
    private(set) var failure: String?
    private var ticked: Set<String> = []

    /// Read again at save time, so a machine added since the preview is never added twice.
    private let existing: () -> [HostRecord]
    private let deviceKeyID: () async throws -> String
    private let save: (HostRecord) async throws -> Void

    init(
        existing: @escaping () -> [HostRecord], deviceKeyID: @escaping () async throws -> String,
        save: @escaping (HostRecord) async throws -> Void
    ) {
        self.existing = existing
        self.deviceKeyID = deviceKeyID
        self.save = save
    }

    var tickedCount: Int { rows.filter { ticked.contains($0.id) }.count }

    func isTicked(_ id: String) -> Bool { ticked.contains(id) }

    func toggle(_ id: String) {
        guard let row = rows.first(where: { $0.id == id }) else { return }
        if case .blocked = row.status { return }
        if !ticked.insert(id).inserted { ticked.remove(id) }
    }

    /// Reads the file at `url`; false, with a message, when it is too big, empty or not text.
    @discardableResult func load(_ url: URL) -> Bool {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        // One byte past the limit is enough to know the file is too big, without reading all of it.
        guard let handle = try? FileHandle(forReadingFrom: url),
              let data = try? handle.read(upToCount: SSHConfigImport.maxBytes + 1), !data.isEmpty else {
            message = String(localized: "import.hosts.unreadable", defaultValue: "Couldn't read that file.")
            return false
        }
        return loadFile(data)
    }

    /// Reads chosen file contents; false, with a message, when too big or not text.
    @discardableResult func loadFile(_ data: Data) -> Bool {
        guard data.count <= SSHConfigImport.maxBytes else {
            message = String(localized: "import.hosts.fileTooLarge", defaultValue: "That file is more than 256 KiB. Paste only the hosts you need.")
            return false
        }
        guard let contents = String(data: data, encoding: .utf8) else {
            message = String(localized: "import.hosts.notText", defaultValue: "That file isn't plain text.")
            return false
        }
        text = contents
        review()
        return true
    }

    func reportUnreadable() {
        message = String(localized: "import.hosts.unreadable", defaultValue: "Couldn't read that file.")
    }

    func review() {
        rows = []
        skipped = []
        ticked = []
        reviewed = false
        message = nil
        let result: ImportResult
        do {
            result = try SSHConfigImport.parse(text)
        } catch {
            message = String(localized: "import.hosts.tooLarge", defaultValue: "That is more than 256 KiB. Paste only the hosts you need.")
            return
        }
        skipped = result.skipped
        guard !result.hosts.isEmpty else {
            message = String(localized: "import.hosts.none", defaultValue: "No hosts found in that text.")
            return
        }
        let current = existing()
        var seen: [HostEndpoint] = []
        rows = result.hosts.map { host in
            var row = classify(host, existing: current)
            if row.status == .ready, let endpoint = Self.endpoint(row.draft) {
                if seen.contains(where: { Self.same($0, endpoint) }) { row.status = .alreadyAdded } else { seen.append(endpoint) }
            }
            return row
        }
        ticked = Set(rows.filter { $0.status == .ready }.map(\.id))
        reviewed = true
    }

    func startOver() {
        rows = []
        skipped = []
        ticked = []
        reviewed = false
        failure = nil
    }

    /// Saves each ticked row once. A row that is saved leaves the selection and shows as added,
    /// so trying again after a failure only adds what is left.
    func importTicked() async {
        guard !saving else { return }
        guard rows.contains(where: { ticked.contains($0.id) }) else { return }
        saving = true
        failure = nil
        defer { saving = false }
        do {
            let key = try await deviceKeyID()
            var count = imported ?? 0
            for row in rows where ticked.contains(row.id) {
                guard let index = rows.firstIndex(where: { $0.id == row.id }) else { continue }
                // The store is read again: another window or device may have added it meanwhile.
                if let endpoint = Self.endpoint(row.draft), existing().contains(where: { Self.same($0.profile.endpoint, endpoint) }) {
                    rows[index].status = .alreadyAdded
                    ticked.remove(row.id)
                    continue
                }
                let id = UUID()
                guard let profile = row.draft.profile(id: id, deviceKeyID: key) else { continue }
                try await save(HostRecord(id: id, profile: profile, symbol: row.draft.symbol.rawValue))
                count += 1
                imported = count
                rows[index].status = .alreadyAdded
                ticked.remove(row.id)
            }
            finished = true
        } catch {
            let saved = imported ?? 0
            failure = saved == 0
                ? String(localized: "import.hosts.failed", defaultValue: "Couldn't save the machines. Try again.")
                : String(localized: "import.hosts.failedSome", defaultValue: "Added \(saved), then couldn't save the rest. Try again to add the others.")
        }
    }

    /// True once every ticked row has been handled without a failure.
    private(set) var finished = false

    private func classify(_ host: ImportedHost, existing: [HostRecord]) -> ImportRow {
        var draft = MachineDraft(imported: host)
        var row = ImportRow(host: host, status: .ready, draft: draft)
        if let jump = host.jump {
            switch resolve(jump, existing: existing) {
            case .success(let endpoint):
                draft.jump = MachineDraft.text(for: endpoint)
            case .failure(let reason):
                row.draft = draft
                row.status = .blocked(reason.text)
                return row
            }
        }
        row.draft = draft
        if let reason = Self.firstError(draft) {
            row.status = .blocked(reason)
        } else if let endpoint = Self.endpoint(draft), existing.contains(where: { Self.same($0.profile.endpoint, endpoint) }) {
            row.status = .alreadyAdded
        }
        return row
    }

    private static func firstError(_ draft: MachineDraft) -> String? {
        let errors = draft.errors
        return MachineDraft.Field.allCases.lazy.compactMap { errors[$0] }.first
    }

    private static func endpoint(_ draft: MachineDraft) -> HostEndpoint? {
        draft.profile(id: UUID(), deviceKeyID: "x")?.endpoint
    }

    private struct JumpReason: Error { var text: String }

    /// A jump written as `user@host` is complete. A bare name must be an imported alias, which the
    /// parser has already filled in, or a machine the user already has.
    private func resolve(_ jump: ImportedJump, existing: [HostRecord]) -> Result<HostEndpoint, JumpReason> {
        if let user = jump.user {
            return .success(HostEndpoint(host: jump.host, port: jump.port ?? 22, user: user))
        }
        if jump.alias != nil {
            return .failure(JumpReason(text: String(
                localized: "import.hosts.jumpNoUser", defaultValue: "The jump host \(jump.host) has no user name in the config."
            )))
        }
        let name = jump.host.lowercased()
        let match = existing.first { $0.profile.name.lowercased() == name }
            ?? existing.first { $0.profile.endpoint.host.lowercased() == name }
        if let match {
            return .success(HostEndpoint(
                host: match.profile.endpoint.host, port: jump.port ?? match.profile.endpoint.port, user: match.profile.endpoint.user
            ))
        }
        return .failure(JumpReason(text: String(
            localized: "import.hosts.jumpMissing", defaultValue: "\(jump.host) isn't in this file or your machines."
        )))
    }

    private static func same(_ a: HostEndpoint, _ b: HostEndpoint) -> Bool {
        a.host.lowercased() == b.host.lowercased() && a.user == b.user && a.port == b.port
    }
}

extension ImportHostsModel: Identifiable {
    nonisolated var id: ObjectIdentifier { ObjectIdentifier(self) }
}

extension MachineDraft {
    /// The add-machine form filled in from one imported host. A jump without a user is written
    /// as the host alone, so the form says what is missing.
    init(imported host: ImportedHost) {
        self.init()
        name = host.alias
        address = host.hostName
        port = String(host.port ?? 22)
        user = host.user ?? ""
        if let jump = host.jump {
            jump.user.map { self.jump = Self.text(for: HostEndpoint(host: jump.host, port: jump.port ?? 22, user: $0)) }
            if jump.user == nil { self.jump = Self.bracketed(jump.host) }
        }
    }

    /// `user@host` or `user@host:port`, with an IPv6 host in brackets.
    static func text(for endpoint: HostEndpoint) -> String {
        let host = bracketed(endpoint.host)
        return endpoint.port == 22 ? "\(endpoint.user)@\(host)" : "\(endpoint.user)@\(host):\(endpoint.port)"
    }

    private static func bracketed(_ host: String) -> String {
        host.contains(":") && !host.hasPrefix("[") ? "[\(host)]" : host
    }
}
