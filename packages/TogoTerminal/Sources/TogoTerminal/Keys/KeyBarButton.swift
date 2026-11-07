import UIKit

@MainActor enum KeyBarButton {
    /// How far a slim key's face sits inside its target, top and bottom.
    static let faceInset: CGFloat = 3

    /// `slim` keys are laid out by the row: the face is drawn inside a target the row's height,
    /// with tighter padding. Others, as on the pad, keep a 44 point minimum.
    static func make(title: String, symbol: String?, slim: Bool = false) -> UIButton {
        var configuration = UIButton.Configuration.gray()
        configuration.cornerStyle = .medium
        configuration.contentInsets = slim
            ? NSDirectionalEdgeInsets(top: 4, leading: 9, bottom: 4, trailing: 9)
            : NSDirectionalEdgeInsets(top: 6, leading: 12, bottom: 6, trailing: 12)
        if slim {
            configuration.background.backgroundInsets = NSDirectionalEdgeInsets(
                top: faceInset, leading: 0, bottom: faceInset, trailing: 0
            )
        }
        let textStyle: UIFont.TextStyle = slim ? .subheadline : .callout
        if let symbol {
            configuration.image = UIImage(systemName: symbol)
            configuration.preferredSymbolConfigurationForImage = UIImage.SymbolConfiguration(textStyle: textStyle)
        } else {
            configuration.title = title
            configuration.titleTextAttributesTransformer = UIConfigurationTextAttributesTransformer { attributes in
                var attributes = attributes
                attributes.font = UIFont.preferredFont(forTextStyle: textStyle)
                return attributes
            }
        }
        let button = UIButton(configuration: configuration)
        button.accessibilityLabel = title
        if !slim {
            button.translatesAutoresizingMaskIntoConstraints = false
            NSLayoutConstraint.activate([
                button.widthAnchor.constraint(greaterThanOrEqualToConstant: 44),
                button.heightAnchor.constraint(greaterThanOrEqualToConstant: 44),
            ])
        }
        return button
    }

    /// What VoiceOver says for a key whose face is an abbreviation.
    static func spokenTitle(_ title: String) -> String {
        switch title {
        case "Esc": "Escape"
        case "Ctrl": "Control"
        case "Alt": "Alt, Option"
        case "^C": "Control C, interrupt"
        case "Back Tab": "Shift Tab"
        case "Ctrl O": "Control O"
        default: title
        }
    }

    /// Armed is tinted; locked is filled, so the two read apart without colour alone.
    static func show(_ state: ModifierLatch.State, on button: UIButton) {
        let title = button.configuration?.title
        var configuration: UIButton.Configuration = switch state {
        case .off: .gray()
        case .once: .tinted()
        case .locked: .filled()
        }
        configuration.cornerStyle = .medium
        configuration.contentInsets = button.configuration?.contentInsets ?? .zero
        configuration.background.backgroundInsets = button.configuration?.background.backgroundInsets ?? .zero
        configuration.title = title
        configuration.image = button.configuration?.image
        configuration.preferredSymbolConfigurationForImage = button.configuration?.preferredSymbolConfigurationForImage
        configuration.titleTextAttributesTransformer = button.configuration?.titleTextAttributesTransformer
        button.configuration = configuration
        button.isSelected = state != .off
        button.accessibilityValue = switch state {
        case .off: "Off"
        case .once: "On for next key"
        case .locked: "Locked"
        }
    }
}

/// Splits a row of keys into pages of whole keys that each fill the width.
enum KeyRowLayout {
    struct Slot: Equatable {
        var index: Int
        var width: CGFloat
    }

    /// How much wider than its natural width a key on a sparse last page may grow, so one or
    /// two keys left over do not stretch across the whole row.
    static let maximumStretch: CGFloat = 1.6

    static func pages(widths: [CGFloat], available: CGFloat, spacing: CGFloat) -> [[Slot]] {
        guard !widths.isEmpty, available > 0 else { return [] }
        var pages: [[Int]] = [[]]
        var used: CGFloat = 0
        for (index, width) in widths.enumerated() {
            let needed = pages[pages.count - 1].isEmpty ? width : used + spacing + width
            if needed <= available || pages[pages.count - 1].isEmpty {
                pages[pages.count - 1].append(index)
                used = needed
            } else {
                pages.append([index])
                used = width
            }
        }
        return pages.enumerated().map { number, page in
            let natural = page.reduce(0) { $0 + widths[$1] } + spacing * CGFloat(max(0, page.count - 1))
            var extra = max(0, available - natural) / CGFloat(page.count)
            let isLast = number == pages.count - 1
            if isLast, pages.count > 1 {
                let narrowest = page.map { widths[$0] }.min() ?? 0
                extra = min(extra, narrowest * (maximumStretch - 1))
            }
            return page.map { Slot(index: $0, width: widths[$0] + extra) }
        }
    }
}
