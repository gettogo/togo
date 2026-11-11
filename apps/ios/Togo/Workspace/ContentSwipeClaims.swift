import UIKit

/// Who holds a navigation controller's content swipe back. Several terminals can be on screen
/// under one navigation controller (two panes, each with its own container), and the swipe is
/// one switch for all of them: it stays off while any of them claims it, and goes back to what
/// it was when the last one lets go, in whatever order they appear, are covered or leave.
@MainActor final class ContentSwipeClaims {
    static let shared = ContentSwipeClaims()

    private struct Entry {
        let recognizer: UIGestureRecognizer
        let wasEnabled: Bool
        var count: Int
    }

    private var entries: [ObjectIdentifier: Entry] = [:]

    /// One more claim on `recognizer`: the first turns it off.
    func claim(_ recognizer: UIGestureRecognizer) {
        let key = ObjectIdentifier(recognizer)
        if var entry = entries[key] {
            entry.count += 1
            entries[key] = entry
        } else {
            entries[key] = Entry(recognizer: recognizer, wasEnabled: recognizer.isEnabled, count: 1)
        }
        recognizer.isEnabled = false
    }

    /// One claim fewer: the last puts it back as it was before the first.
    func release(_ recognizer: UIGestureRecognizer) {
        let key = ObjectIdentifier(recognizer)
        guard var entry = entries[key] else { return }
        entry.count -= 1
        if entry.count > 0 {
            entries[key] = entry
        } else {
            entries[key] = nil
            recognizer.isEnabled = entry.wasEnabled
        }
    }

    /// How many claims `recognizer` holds now.
    func count(for recognizer: UIGestureRecognizer) -> Int {
        entries[ObjectIdentifier(recognizer)]?.count ?? 0
    }
}
