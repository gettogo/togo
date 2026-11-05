import UIKit

/// Finds a plain `http://` or `https://` URL printed without OSC 8 markup.
///
/// SwiftTerm only opens explicit links on a tap, so printed URLs (a dev server's address, a
/// pull request link from an agent) are found here from the text under the finger.
enum BareLink {
    /// Punctuation that usually ends the sentence around a URL rather than the URL itself.
    private static let trailingPunctuation: Set<Character> = [".", ",", ";", ":", "!", "?", "'", "\""]
    private static let closers: [Character: Character] = [")": "(", "]": "[", "}": "{"]

    /// The URL covering the character at `index` of a logical line (wrapped rows joined).
    static func url(in line: String, at index: Int) -> URL? {
        guard index >= 0 else { return nil }
        for match in line.matches(of: /https?:\/\/[^\s<>"'`]+/.ignoresCase()) {
            let text = trimmed(String(line[match.range]))
            let start = line.distance(from: line.startIndex, to: match.range.lowerBound)
            guard (start..<(start + text.count)).contains(index) else { continue }
            guard let url = URL(string: text), LinkPolicy.allows(url) else { return nil }
            return url
        }
        return nil
    }

    private static func trimmed(_ text: String) -> String {
        var text = Substring(text)
        while let last = text.last {
            if trailingPunctuation.contains(last) {
                text.removeLast()
            } else if let opener = closers[last], text.count(where: { $0 == opener }) < text.count(where: { $0 == last }) {
                // An unbalanced closer belongs to the prose: "(see https://a.dev/x)".
                text.removeLast()
            } else {
                break
            }
        }
        return String(text)
    }
}

/// The long-press menu for a link: Open and Copy Link.
@MainActor final class LinkMenu: NSObject, @preconcurrency UIEditMenuInteractionDelegate {
    var open: (URL) -> Void = { _ in }
    var copy: (String) -> Void = { _ in }

    private lazy var interaction = UIEditMenuInteraction(delegate: self)
    private var url: URL?

    init(view: UIView) {
        super.init()
        view.addInteraction(interaction)
    }

    func present(_ url: URL, at point: CGPoint) {
        self.url = url
        interaction.presentEditMenu(with: UIEditMenuConfiguration(identifier: nil, sourcePoint: point))
    }

    func editMenuInteraction(
        _ interaction: UIEditMenuInteraction, menuFor configuration: UIEditMenuConfiguration,
        suggestedActions: [UIMenuElement]
    ) -> UIMenu? {
        guard let url else { return nil }
        return UIMenu(children: [
            UIAction(title: "Open", image: UIImage(systemName: "safari")) { [weak self] _ in self?.open(url) },
            UIAction(title: "Copy Link", image: UIImage(systemName: "link")) { [weak self] _ in
                self?.copy(url.absoluteString)
            },
        ])
    }
}
