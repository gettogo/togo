import Foundation
import TogoKit

/// A background shell, dev server, exec or job as its card shows it: what it is, how it is
/// going, the last lines it printed, and where it opens. Read from its mission control row
/// (which carries the live tail and the open target) and, when present, its `background` item.
struct BackgroundJob: Equatable, Identifiable {
    let id: String
    var title: String
    var command: String?
    var state: HarnessState
    var startedAt: Date?
    var tail: [String]
    var open: HarnessOpen?

    /// The protocol sends at most four tail lines; a longer tail keeps its end.
    static let tailLines = 4

    init(row: HarnessRow) {
        id = row.id
        title = row.title
        command = row.subtitle
        state = row.state
        startedAt = row.startedAt?.date
        tail = Array((row.tail ?? []).suffix(Self.tailLines))
        open = row.open
    }

    init?(item: HarnessItem) {
        guard case .background(let job) = item.body else { return nil }
        id = item.id
        title = job.title
        command = job.command
        state = job.state
        startedAt = job.startedAt.date
        tail = []
        open = nil
    }

    /// The job with `id`: a background row (by row id), else a background item (by item id or
    /// job id), with the item filling what the row leaves out.
    static func find(_ id: String, mission: MissionState, timeline: HarnessTimeline) -> BackgroundJob? {
        let item = timeline.item(id) ?? timeline.items.first { item in
            if case .background(let job) = item.body { return job.jobId == id }
            return false
        }
        let fromItem = item.flatMap(BackgroundJob.init(item:))
        let jobID: String? = {
            if case .background(let job)? = item?.body { return job.jobId }
            return nil
        }()
        let rows = flatten(mission.section(.background)?.rows ?? [])
        guard let row = rows.first(where: { $0.id == id || $0.id == jobID || $0.id == fromItem?.id }) else {
            return fromItem
        }
        var job = BackgroundJob(row: row)
        if job.command?.isEmpty ?? true { job.command = fromItem?.command }
        return job
    }

    private static func flatten(_ rows: [HarnessRow]) -> [HarnessRow] {
        rows.flatMap { [$0] + flatten($0.children ?? []) }
    }

    /// The live terminal the job runs in, when it has one.
    var terminal: TerminalTarget? {
        guard let open, open.kind == .terminal else { return nil }
        return TerminalTarget(open.ref)
    }

    /// A screen of its output, for jobs without a pane of their own (an exec, a background
    /// shell the agent keeps): a Live list.
    var outputScreen: String? {
        guard let open, open.kind == .screen, !open.ref.isEmpty else { return nil }
        return open.ref
    }
}

/// A live terminal to attach to: a session, and optionally a window and pane in it, written
/// `s_abcdefghij`, `s_abcdefghij:2` or `s_abcdefghij:2.1` (tmux's own target form). Session
/// ids follow the helper's two forms; anything else is refused.
struct TerminalTarget: Equatable, Hashable {
    let sessionID: String
    /// The tmux window and pane, as given after the colon.
    let pane: String?

    init?(_ ref: String) {
        let parts = ref.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        let session = String(parts[0])
        guard Self.isSessionID(session) else { return nil }
        if parts.count == 2 {
            let pane = String(parts[1])
            guard !pane.isEmpty, pane.count <= 16,
                  pane.allSatisfy({ $0.isASCII && ($0.isNumber || $0 == ".") }),
                  !pane.hasPrefix("."), !pane.hasSuffix(".") else { return nil }
            self.pane = pane
        } else {
            pane = nil
        }
        sessionID = session
    }

    /// The target written back in the form `init?(_:)` reads.
    var ref: String { pane.map { "\(sessionID):\($0)" } ?? sessionID }

    static func isSessionID(_ id: String) -> Bool {
        if id.hasPrefix("s_") {
            let rest = id.dropFirst(2)
            return rest.count == 10 && rest.allSatisfy { ("a"..."z").contains($0) || ("2"..."7").contains($0) }
        }
        if id.hasPrefix("u_") {
            let rest = id.dropFirst(2)
            return !rest.isEmpty && rest.count.isMultiple(of: 2) && rest.allSatisfy { $0.isHexDigit && !$0.isUppercase }
        }
        return false
    }
}
