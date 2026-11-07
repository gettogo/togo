import Foundation
import TogoKit

/// The composer's text rules, apart from the model: which menu the draft calls for, how
/// completions filter between calls, and how a chosen one lands in the draft.
enum CommandLineLogic {
    /// What the draft is asking for.
    enum Trigger: Equatable {
        /// A leading "/" with no space after it yet: the command so far, without the slash.
        case command(String)
        /// An "@word" at the end of the draft: the word without the "@", and where the "@" is.
        case file(String, at: Int)

        var kind: HarnessCompleteParams.Kind {
            switch self {
            case .command: .command
            case .file: .file
            }
        }

        var query: String {
            switch self {
            case .command(let query), .file(let query, _): query
            }
        }
    }

    static func trigger(in draft: String) -> Trigger? {
        if draft.hasPrefix("/") {
            let body = draft.dropFirst()
            if !body.contains(where: \.isWhitespace) { return .command(String(body)) }
        }
        // The last word, when it starts with "@" and follows the start or a space.
        guard let at = draft.lastIndex(of: "@") else { return nil }
        let word = draft[draft.index(after: at)...]
        guard !word.contains(where: \.isWhitespace) else { return nil }
        if at > draft.startIndex, !draft[draft.index(before: at)].isWhitespace { return nil }
        return .file(String(word), at: draft.distance(from: draft.startIndex, to: at))
    }

    /// Completions that still match a longer query, best first: a title or insert starting with
    /// it, then one containing it, then a detail containing it. Order within a rank is kept.
    static func filter(_ items: [HarnessCompletion], query: String) -> [HarnessCompletion] {
        let q = query.lowercased()
        guard !q.isEmpty else { return items }
        func bare(_ s: String) -> String {
            var s = s.lowercased()
            while s.hasPrefix("/") || s.hasPrefix("@") { s.removeFirst() }
            return s
        }
        let ranked: [(Int, HarnessCompletion)] = items.compactMap { item in
            let title = bare(item.title), insert = bare(item.insert)
            if title.hasPrefix(q) || insert.hasPrefix(q) { return (0, item) }
            if title.contains(q) || insert.contains(q) { return (1, item) }
            if item.detail?.lowercased().contains(q) == true { return (2, item) }
            return nil
        }
        return ranked.enumerated().sorted { a, b in
            a.element.0 != b.element.0 ? a.element.0 < b.element.0 : a.offset < b.offset
        }.map(\.element.1)
    }

    /// Starred commands first, then recent ones, each in its own order; the rest as they came.
    static func ordered(_ items: [HarnessCompletion], favourites: [String], recents: [String]) -> [HarnessCompletion] {
        func word(_ item: HarnessCompletion) -> String { CommandMemory.commandWord(item.insert.isEmpty ? item.title : item.insert) }
        let starred = favourites.compactMap { fav in items.first { word($0) == fav } }
        let recent = recents.compactMap { r in items.first { word($0) == r && !favourites.contains(r) } }
        let lead = Set((starred + recent).map(word))
        return starred + recent + items.filter { !lead.contains(word($0)) }
    }

    /// The draft with the trigger replaced by the completion's insert text.
    static func applying(_ completion: HarnessCompletion, to draft: String, trigger: Trigger) -> String {
        switch trigger {
        case .command:
            return completion.insert
        case let .file(_, at):
            let start = draft.index(draft.startIndex, offsetBy: min(at, draft.count))
            return String(draft[..<start]) + completion.insert
        }
    }

    /// What Send sends: the draft without its trailing line breaks. nil when there is nothing.
    static func outgoing(_ draft: String) -> String? {
        let text = String(draft.reversed().drop(while: \.isNewline).reversed())
        return text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : text
    }

    /// A prompt typed while the agent works waits its turn; otherwise it goes now.
    static func sendMode(working: Bool) -> HarnessSendParams.Mode {
        working ? .queue : .now
    }

    /// Return with the menu open sends a command already typed in full; otherwise it takes the
    /// highlighted completion.
    static func returnSends(_ draft: String, highlighted: HarnessCompletion?) -> Bool {
        guard let highlighted else { return true }
        if highlighted.screen != nil { return false }
        let typed = draft.trimmingCharacters(in: .whitespaces)
        return typed == highlighted.insert.trimmingCharacters(in: .whitespaces)
    }
}
