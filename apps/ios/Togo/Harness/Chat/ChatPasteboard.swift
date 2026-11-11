import SwiftUI
import TogoKit
import UIKit
import UniformTypeIdentifiers

/// What the chat puts on the pasteboard. A reply goes as one item in three forms: its markdown
/// as the plain text (editors, terminals, a chat with an agent), and HTML and RTF with the
/// formatting (Notes, Mail, Slack, Docs), so each app takes the form it reads best. Code,
/// commands and diffs go as their exact text only: no line numbers, markers or added
/// whitespace, and no trailing newline the block did not have.
enum ChatPasteboard {
    /// The item a reply's markdown copies as.
    static func items(markdown: String) -> [String: Any] {
        var item: [String: Any] = [UTType.utf8PlainText.identifier: markdown]
        item[UTType.html.identifier] = Data(RichMarkdown.html(markdown).utf8)
        if let rtf = RichMarkdown.rtf(RichMarkdown.attributed(markdown, style: .pasteboard)) {
            item[UTType.rtf.identifier] = rtf
        }
        return item
    }

    /// A reply, a thought or a markdown card: markdown and rich text together.
    static func copy(markdown: String, to board: UIPasteboard = .general) {
        board.setItems([items(markdown: markdown)])
        confirm()
    }

    /// A code block, command, diff or prompt: exactly its text.
    static func copy(exact text: String, to board: UIPasteboard = .general) {
        board.setItems([[UTType.utf8PlainText.identifier: text]])
        confirm()
    }

    /// Part of a reply selected in the chat: its words as plain text, and as RTF with their
    /// formatting for apps that paste it. The app's own `togo-file` links and its colours stay
    /// in the app, so a paste takes the destination's ink and opens nothing it cannot.
    static func items(selection: NSAttributedString) -> [String: Any] {
        var item: [String: Any] = [UTType.utf8PlainText.identifier: selection.string]
        let rich = NSMutableAttributedString(attributedString: selection)
        let whole = NSRange(location: 0, length: rich.length)
        rich.removeAttribute(.foregroundColor, range: whole)
        rich.enumerateAttribute(.link, in: whole) { value, range, _ in
            guard value != nil else { return }
            let url = (value as? URL) ?? (value as? String).flatMap(URL.init(string:))
            if let exported = url.flatMap(RichMarkdown.exported) {
                rich.addAttribute(.link, value: exported, range: range)
            } else {
                rich.removeAttribute(.link, range: range)
            }
        }
        if let rtf = RichMarkdown.rtf(rich) { item[UTType.rtf.identifier] = rtf }
        return item
    }

    static func copy(selection: NSAttributedString, to board: UIPasteboard = .general) {
        board.setItems([items(selection: selection)])
        confirm()
    }

    /// A table: its Markdown for plain editors, tab-separated values for spreadsheets and HTML
    /// for Notes, Mail and Slack, so it pastes as a table wherever tables paste.
    static func items(table: MarkdownTable) -> [String: Any] {
        [
            UTType.utf8PlainText.identifier: table.markdown,
            UTType.utf8TabSeparatedText.identifier: table.tabSeparated(ChatTableText.words),
            UTType.html.identifier: Data(("<meta charset=\"utf-8\">" + ChatTableText.html(table)).utf8),
        ]
    }

    static func copy(table: MarkdownTable, to board: UIPasteboard = .general) {
        board.setItems([items(table: table)])
        confirm()
    }

    /// VoiceOver hears that the copy happened; the edit menu's own Copy says nothing either.
    private static func confirm() {
        AccessibilityNotification.Announcement(ChatCopy.copied).post()
    }
}

extension HarnessItem {
    /// A reply or a thought is markdown; every other row's words are copied as they are.
    var copiesAsMarkdown: Bool {
        switch body {
        case .assistant, .thinking: true
        default: false
        }
    }

    /// Copies the row's words (`chatText`) in the form that suits them.
    func copyWords() {
        guard let text = chatText else { return }
        if copiesAsMarkdown {
            ChatPasteboard.copy(markdown: text)
        } else {
            ChatPasteboard.copy(exact: text)
        }
    }
}
