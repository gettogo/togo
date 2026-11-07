import Foundation

extension ChatRow {
    /// A turn the agent started on its own: its quiet marker ("Subagent finished").
    public var isContinued: Bool {
        if case .notice(let item) = self, case .notice(let notice) = item.body {
            return notice.code == HarnessItem.Notice.continued
        }
        return false
    }

    /// When the row's content began, and when it ended (the same moment for a single item;
    /// nil while its work still runs).
    var span: (start: Date, end: Date?) {
        switch self {
        case .prompt(let item), .reply(let item), .needs(let item), .notice(let item), .shell(let item):
            (item.at.date, item.at.date)
        case .work(_, let summary, let steps):
            (summary.startedAt ?? steps.first?.at.date ?? .distantPast, summary.running ? nil : summary.endedAt)
        }
    }
}

/// Where time passed in the chat: rows that come more than `threshold` after everything
/// before them ended get a quiet timestamp, or, for a turn the agent started on its own, a
/// "2m later" on its marker.
public enum ChatGaps {
    public static let threshold: TimeInterval = 60

    /// The silence before each row that follows one longer than `threshold`, by row id.
    public static func gaps(_ rows: [ChatRow], threshold: TimeInterval = threshold) -> [String: TimeInterval] {
        var out: [String: TimeInterval] = [:]
        var last: Date?
        for row in rows {
            let span = row.span
            guard span.start != .distantPast else { continue }
            if let last, span.start.timeIntervalSince(last) > threshold {
                out[row.id] = span.start.timeIntervalSince(last)
            }
            // Rows of a turn are drawn prompt, work, replies, so a reply may predate the work's
            // end: the latest moment seen so far is what the next row is measured from.
            let end = span.end ?? span.start
            last = max(last ?? end, end)
        }
        return out
    }

    /// The moment a row starts, for its timestamp.
    public static func start(of row: ChatRow) -> Date? {
        let start = row.span.start
        return start == .distantPast ? nil : start
    }
}
