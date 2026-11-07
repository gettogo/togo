import Foundation
import Observation

/// What the composer remembers per harness: the lines the user sent, newest first, and the
/// commands they starred. Kept on the phone in user defaults; never anything the agent wrote.
@MainActor @Observable final class CommandMemory {
    static let historyLimit = 50
    static let favouriteLimit = 24

    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private let prefix: String
    /// Bumped on every change, so views that read through methods redraw.
    private(set) var revision = 0

    init(defaults: UserDefaults = .standard, prefix: String = "composer") {
        self.defaults = defaults
        self.prefix = prefix
    }

    static let shared = CommandMemory()

    // MARK: History

    /// Lines sent in this harness, newest first, without repeats.
    func history(_ scope: String) -> [String] {
        _ = revision
        return defaults.stringArray(forKey: key("history", scope)) ?? []
    }

    func record(_ line: String, scope: String) {
        let text = line.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        var list = history(scope).filter { $0 != text }
        list.insert(text, at: 0)
        defaults.set(Array(list.prefix(Self.historyLimit)), forKey: key("history", scope))
        revision += 1
    }

    /// Slash commands from the history, newest first, by their command word: "/model".
    func recentCommands(_ scope: String, limit: Int = 5) -> [String] {
        var seen: [String] = []
        for line in history(scope) where line.hasPrefix("/") {
            let word = Self.commandWord(line)
            if !seen.contains(word) { seen.append(word) }
            if seen.count == limit { break }
        }
        return seen
    }

    nonisolated static func commandWord(_ line: String) -> String {
        String(line.prefix { !$0.isWhitespace })
    }

    // MARK: Favourites

    /// Starred commands by their command word, in the order they were starred.
    func favourites(_ scope: String) -> [String] {
        _ = revision
        return defaults.stringArray(forKey: key("favourites", scope)) ?? []
    }

    func isFavourite(_ word: String, scope: String) -> Bool {
        favourites(scope).contains(Self.commandWord(word))
    }

    func toggleFavourite(_ word: String, scope: String) {
        let word = Self.commandWord(word)
        guard !word.isEmpty else { return }
        var list = favourites(scope)
        if let index = list.firstIndex(of: word) {
            list.remove(at: index)
        } else {
            list.append(word)
        }
        defaults.set(Array(list.suffix(Self.favouriteLimit)), forKey: key("favourites", scope))
        revision += 1
    }

    private func key(_ kind: String, _ scope: String) -> String {
        "\(prefix).\(kind).\(scope.isEmpty ? "default" : scope)"
    }
}
