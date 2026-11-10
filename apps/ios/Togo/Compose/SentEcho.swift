import UIKit

/// The words of the last Send, watched for a moment after it.
///
/// The keyboard's own dictation keeps listening when Send is tapped. Its final result arrives
/// after the field was cleared and puts the sent words back, so the message seems not to have
/// gone. A late copy of the sent words that lands in the empty field is dropped; anything said
/// after them stays. Only what the keyboard writes is filtered, never the app's own changes
/// such as a recalled message or a failed send's words coming back.
struct SentEcho: Equatable {
    /// Dictation's last word comes within a second or two of Send.
    static let window: Duration = .seconds(5)

    let text: String
    let at: ContinuousClock.Instant

    enum Outcome: Equatable {
        /// What the keyboard wrote is not a late copy: it stands and the watch ends.
        case other
        /// The field holds this instead: the sent words taken out of what the keyboard wrote.
        case echo(kept: String)
        /// Still empty: keep watching.
        case waiting
    }

    /// What to do with `draft`, written into a field that held `previous`.
    func outcome(of draft: String, previous: String, now: ContinuousClock.Instant) -> Outcome {
        guard now - at <= Self.window, previous.isEmpty else { return .other }
        if draft.isEmpty { return .waiting }
        let sent = Self.words(text)
        guard !sent.isEmpty else { return .other }
        // The final result may differ from the words shown while listening only in case and
        // punctuation ("hello world" against "Hello world.").
        if Self.words(draft) == sent { return .echo(kept: "") }
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmed.isEmpty, draft.hasPrefix(trimmed) {
            let rest = draft.dropFirst(trimmed.count).drop { $0.isWhitespace || $0.isPunctuation }
            return .echo(kept: String(rest))
        }
        return .other
    }

    private static func words(_ text: String) -> String {
        String(text.lowercased().filter { $0.isLetter || $0.isNumber })
    }
}

/// The keyboard's dictation, for the field being typed in.
@MainActor enum KeyboardDictation {
    /// Ends the keyboard's dictation if it is listening in the focused field, so it stops with
    /// the Send rather than writing into the field after it. The keyboard stays up: the field
    /// takes focus back in the same turn.
    static func end() {
        guard let field = FirstResponder.current else { return }
        end(in: field)
    }

    static func end(in field: UIResponder) {
        guard field.isFirstResponder, field.textInputMode?.primaryLanguage == "dictation" else { return }
        field.resignFirstResponder()
        field.becomeFirstResponder()
    }
}

/// The responder with focus, asked through the responder chain.
@MainActor private enum FirstResponder {
    static weak var found: UIResponder?

    static var current: UIResponder? {
        found = nil
        defer { found = nil }
        UIApplication.shared.sendAction(#selector(UIResponder.togoReportFirstResponder(_:)), to: nil, from: nil, for: nil)
        return found
    }
}

extension UIResponder {
    @objc fileprivate func togoReportFirstResponder(_ sender: Any?) {
        FirstResponder.found = self
    }
}
