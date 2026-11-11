import Foundation
import TogoKit
import TogoTerminal

/// The keys a waiting request puts above the keyboard, and what each one answers. Nothing is
/// read off the screen: the helper's request card says what the agent asks.
enum PromptKeys {
    /// The most options shown as words; past that the agent's menu is driven with the arrows.
    static let wordLimit = 3
    static let moreID = "more"

    /// The open request for `sessionID`, if any.
    static func card(for sessionID: String, in requests: [RequestCard]) -> RequestCard? {
        requests.first { $0.sessionID == sessionID && $0.phase == .open }
    }

    static func plan(for card: RequestCard?) -> [PromptKey] {
        guard let card, card.phase == .open else { return [] }
        let choices = RequestChoices(card.options)
        let hasRoles = choices.approve?.role == .approve || choices.deny != nil
        if card.options.isEmpty {
            // A question that takes only text: typing is the answer; More shows it whole.
            return card.allowText ? [more] : []
        }
        if card.options.count <= wordLimit {
            let words = card.options.map { option in
                PromptKey(id: "option-\(option.n)", label: option.label, filled: option == choices.approve, kind: .answer)
            }
            return words + (card.allowText ? [more] : [])
        }
        if !hasRoles {
            // A long menu the agent draws itself: move through it and pick.
            return [
                PromptKey(id: "up", label: KeysCopy.up, symbol: "arrow.up", kind: .stroke(KeyStroke(.up))),
                PromptKey(id: "down", label: KeysCopy.down, symbol: "arrow.down", kind: .stroke(KeyStroke(.down))),
                PromptKey(id: "enter", label: KeysCopy.enter, symbol: "return", kind: .stroke(KeyStroke(.enter))),
                more,
            ]
        }
        var keys: [PromptKey] = []
        if let approve = choices.approve {
            keys.append(PromptKey(id: "option-\(approve.n)", label: approve.label, filled: true, kind: .answer))
        }
        if let deny = choices.deny {
            keys.append(PromptKey(id: "option-\(deny.n)", label: deny.label, kind: .answer))
        }
        return keys + [more]
    }

    static let more = PromptKey(id: moreID, label: KeysCopy.more, symbol: "ellipsis", kind: .more)

    /// What tapping `key` sends to the helper, or nil for keys that are not answers.
    static func decision(for key: PromptKey, card: RequestCard) -> Decision? {
        guard key.kind == .answer, key.id.hasPrefix("option-"), let n = Int(key.id.dropFirst("option-".count)) else {
            return nil
        }
        return card.options.contains { $0.n == n } ? .option(n) : nil
    }
}
