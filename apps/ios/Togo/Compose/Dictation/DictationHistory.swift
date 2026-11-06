import Foundation

/// The last dictations, newest first. Kept in this device's defaults and never synced, since
/// what someone said aloud is theirs alone.
struct DictationHistory {
    static let limit = 20
    private static let key = "dictation.history"
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func load() -> [String] {
        defaults.stringArray(forKey: Self.key) ?? []
    }

    func add(_ text: String) {
        let entries = [text] + load().filter { $0 != text }
        defaults.set(Array(entries.prefix(Self.limit)), forKey: Self.key)
    }

    func clear() {
        defaults.removeObject(forKey: Self.key)
    }
}
