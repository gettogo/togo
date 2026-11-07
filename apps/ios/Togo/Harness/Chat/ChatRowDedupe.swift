import Foundation
import TogoKit

/// Rules that keep the chat from saying one thing twice.
enum ChatRowDedupe {
    /// A stopped turn already says so on its work line ("Stopped after 8s  2 steps"), so the
    /// helper's separate "Interrupted" notice for the same turn is dropped. A notice with no
    /// stopped work line between the prompts around it stays: it is the only word on it.
    static func apply(_ rows: [ChatRow]) -> [ChatRow] {
        var out: [ChatRow] = []
        var segment: [ChatRow] = []
        func flush() {
            let stopped = segment.contains { row in
                if case .work(_, let summary, _) = row { return summary.failed && !summary.running }
                return false
            }
            out.append(contentsOf: stopped ? segment.filter { !isInterrupt($0) } : segment)
            segment = []
        }
        for row in rows {
            if case .prompt = row { flush() }
            segment.append(row)
        }
        flush()
        return out
    }

    static func isInterrupt(_ row: ChatRow) -> Bool {
        guard case .notice(let item) = row, case .notice(let notice) = item.body else { return false }
        return notice.code == "interrupted"
    }
}
