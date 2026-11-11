import SwiftUI
import UIKit

/// Read-only text that selects the way iOS text does: a long press picks the word under the
/// finger with handles to drag across lines, a double tap picks a word, a triple tap a line or
/// paragraph, and the edit menu offers Copy, Look Up, Translate and Share. SwiftUI's
/// `textSelection(.enabled)` on iOS can only copy the whole text, so this is UIKit's text view
/// (TextKit 2), not editable. It does not scroll unless asked (`Layout.scrolls`): in the chat
/// the chat's own scroll view moves it, so drags, swipes and the back gesture stay the chat's.
///
/// The text is built once per `key` and Dynamic Type size; SwiftUI updates with the same key
/// keep the selection and skip the layout.
struct SelectableText: UIViewRepresentable {
    enum Layout: Equatable {
        /// Wraps to the width offered; as tall as its lines.
        case wraps
        /// Every line as long as it is written, for code: as wide and tall as the text, inside
        /// a horizontal scroll view.
        case lines
        /// Fills the space offered and scrolls itself, for a long history in a sheet.
        case scrolls
    }

    /// Names the content: the text is built again only when it changes.
    let key: AnyHashable
    let build: @MainActor (UITraitCollection) -> NSAttributedString
    var layout: Layout = .wraps
    var insets: UIEdgeInsets = .zero
    /// What Copy puts on the pasteboard for the selected part; nil keeps UIKit's own copy.
    var copy: (@MainActor (NSAttributedString) -> Void)?
    /// The text is a heading, for VoiceOver's headings rotor.
    var heading = false
    /// As wide as its longest line when that is narrower than the room, as `Text` is.
    var hugs = false
    /// For `Layout.scrolls`: the part to bring to the middle once the text is shown.
    var reveal: NSRange?
    /// With no width offered (a table cell in a sideways scroll), lines wrap at this width
    /// rather than running on, so a long cell wraps by words and never by letters.
    var maxWidth: CGFloat?

    init(
        key: AnyHashable, layout: Layout = .wraps, insets: UIEdgeInsets = .zero, heading: Bool = false,
        hugs: Bool = false, reveal: NSRange? = nil, maxWidth: CGFloat? = nil, copy: (@MainActor (NSAttributedString) -> Void)? = nil,
        build: @escaping @MainActor (UITraitCollection) -> NSAttributedString
    ) {
        self.key = key
        self.layout = layout
        self.insets = insets
        self.heading = heading
        self.hugs = hugs
        self.reveal = reveal
        self.maxWidth = maxWidth
        self.copy = copy
        self.build = build
    }

    func makeUIView(context: Context) -> SelectableTextUIView {
        let view = SelectableTextUIView(usingTextLayoutManager: true)
        view.isEditable = false
        view.isSelectable = true
        view.isScrollEnabled = layout == .scrolls
        view.backgroundColor = .clear
        view.textContainerInset = insets
        view.textContainer.lineFragmentPadding = 0
        view.dataDetectorTypes = []
        // Nothing the keyboard would change: the text is read and copied as it is.
        view.smartQuotesType = .no
        view.smartDashesType = .no
        // Lifting selected words to drag them would fight the chat's scroll for the same press.
        view.textDragInteraction?.isEnabled = false
        // Links and selection in the text's own colour, underlined: the beacon blue only ever
        // means needs-you.
        view.tintColor = .togoSnow
        view.linkTextAttributes = [
            .foregroundColor: UIColor.togoSnow,
            .underlineStyle: NSUnderlineStyle.single.rawValue,
        ]
        view.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        view.setContentHuggingPriority(.defaultLow, for: .horizontal)
        view.delegate = context.coordinator
        return view
    }

    func updateUIView(_ view: SelectableTextUIView, context: Context) {
        let coordinator = context.coordinator
        coordinator.openURL = context.environment.openURL
        coordinator.actions = context.environment.selectableTextActions
        view.onCopy = copy
        if view.textContainerInset != insets { view.textContainerInset = insets }
        if heading { view.accessibilityTraits.insert(.header) }
        let category = context.environment.dynamicTypeSize.contentSizeCategory
        guard coordinator.shownKey != key || coordinator.shownCategory != category else { return }
        coordinator.shownKey = key
        coordinator.shownCategory = category
        coordinator.reset()
        view.attributedText = build(UITraitCollection(preferredContentSizeCategory: category))
        if layout == .scrolls, let reveal, NSMaxRange(reveal) <= view.attributedText.length {
            // Once the view has its size, so the middle is the middle of what shows.
            DispatchQueue.main.async { [weak view] in view?.center(reveal) }
        }
    }

    func sizeThatFits(_ proposal: ProposedViewSize, uiView: SelectableTextUIView, context: Context) -> CGSize? {
        switch layout {
        case .scrolls:
            return nil
        case .lines:
            return context.coordinator.natural(uiView)
        case .wraps:
            guard let width = proposal.width, width.isFinite, width > 0 else {
                let natural = context.coordinator.natural(uiView)
                if let maxWidth, natural.width > maxWidth { return context.coordinator.size(uiView, width: maxWidth) }
                return natural
            }
            if hugs {
                let natural = context.coordinator.natural(uiView)
                if natural.width <= width { return natural }
            }
            return context.coordinator.size(uiView, width: width)
        }
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    /// A link opens as it would anywhere else in the app (through `openURL`, so the chat's
    /// router sends files to the review and pages to Safari's view); a scheme the router
    /// refuses does nothing, not even from the long-press menu.
    final class Coordinator: NSObject, UITextViewDelegate {
        var openURL: OpenURLAction?
        var actions: SelectableTextActions?
        var shownKey: AnyHashable?
        var shownCategory: UIContentSizeCategory?
        /// Heights by width for the text shown, so a lazy list asking again costs nothing.
        private var measured: [CGFloat: CGFloat] = [:]
        private var naturalSize: CGSize?

        func reset() {
            measured.removeAll()
            naturalSize = nil
        }

        func size(_ view: UITextView, width: CGFloat) -> CGSize {
            if let height = measured[width] { return CGSize(width: width, height: height) }
            let height = ceil(view.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude)).height)
            measured[width] = height
            return CGSize(width: width, height: height)
        }

        /// The size with no line wrapped: the widest line, and every line's height.
        func natural(_ view: UITextView) -> CGSize {
            if let naturalSize { return naturalSize }
            let text = view.attributedText ?? NSAttributedString()
            let bounds = text.boundingRect(
                with: CGSize(width: CGFloat.greatestFiniteMagnitude, height: .greatestFiniteMagnitude),
                options: [.usesLineFragmentOrigin, .usesFontLeading], context: nil
            )
            let inset = view.textContainerInset
            // A point of slack, so rounding never wraps the longest line.
            let width = ceil(bounds.width) + 1 + inset.left + inset.right
            let size = self.size(view, width: width)
            naturalSize = size
            return size
        }

        func textView(_ textView: UITextView, primaryActionFor textItem: UITextItem, defaultAction: UIAction) -> UIAction? {
            guard case .link(let url) = textItem.content else { return defaultAction }
            guard ChatLinks.route(url) != .refused else { return nil }
            return UIAction { [weak self] _ in self?.openURL?(url) }
        }

        func textView(
            _ textView: UITextView, menuConfigurationFor textItem: UITextItem, defaultMenu: UIMenu
        ) -> UITextItem.MenuConfiguration? {
            guard case .link(let url) = textItem.content, ChatLinks.route(url) == .refused else {
                return UITextItem.MenuConfiguration(menu: defaultMenu)
            }
            return nil
        }

        /// The system's Copy, Look Up, Translate and Share first, then the row's own actions.
        func textView(
            _ textView: UITextView, editMenuForTextIn range: NSRange, suggestedActions: [UIMenuElement]
        ) -> UIMenu? {
            let text = textView.text as NSString? ?? ""
            let selected = NSMaxRange(range) <= text.length ? text.substring(with: range) : ""
            let extra = actions?.items(selected) ?? []
            guard !extra.isEmpty else { return nil }
            return UIMenu(children: suggestedActions + [UIMenu(options: .displayInline, children: extra)])
        }
    }
}

/// The text view under `SelectableText`.
final class SelectableTextUIView: UITextView {
    var onCopy: (@MainActor (NSAttributedString) -> Void)?

    /// Scrolls so the range sits in the middle of the view, or as near as the ends allow.
    func center(_ range: NSRange) {
        layoutIfNeeded()
        guard let start = position(from: beginningOfDocument, offset: range.location),
              let end = position(from: start, offset: range.length),
              let textRange = textRange(from: start, to: end) else { return }
        let rect = firstRect(for: textRange)
        guard !rect.isNull, !rect.isInfinite else { return }
        let top = -adjustedContentInset.top
        let bottom = max(top, contentSize.height + adjustedContentInset.bottom - bounds.height)
        let y = min(max(rect.midY - bounds.height / 2, top), bottom)
        setContentOffset(CGPoint(x: contentOffset.x, y: y), animated: false)
    }

    override func copy(_ sender: Any?) {
        let range = selectedRange
        guard let onCopy, range.length > 0, NSMaxRange(range) <= attributedText.length else { return super.copy(sender) }
        onCopy(attributedText.attributedSubstring(from: range))
    }

    /// Leaving the text (a tap in the composer, another row's words) leaves nothing selected
    /// behind, as everywhere else in iOS.
    override func resignFirstResponder() -> Bool {
        let resigned = super.resignFirstResponder()
        if resigned, selectedRange.length > 0 { selectedRange = NSRange(location: selectedRange.location, length: 0) }
        return resigned
    }
}

/// Items a selectable text's edit menu adds after the system's own: the row's actions (Copy
/// message, Quote in reply, Show in terminal), so a long press on words selects them and the
/// row's menu is still one tap away.
struct SelectableTextActions {
    /// Built when the menu opens, from the words selected then.
    let items: @MainActor (_ selected: String) -> [UIMenuElement]
}

extension EnvironmentValues {
    @Entry var selectableTextActions: SelectableTextActions?
}

extension DynamicTypeSize {
    /// UIKit's name for the size, for fonts built outside SwiftUI.
    var contentSizeCategory: UIContentSizeCategory {
        switch self {
        case .xSmall: .extraSmall
        case .small: .small
        case .medium: .medium
        case .large: .large
        case .xLarge: .extraLarge
        case .xxLarge: .extraExtraLarge
        case .xxxLarge: .extraExtraExtraLarge
        case .accessibility1: .accessibilityMedium
        case .accessibility2: .accessibilityLarge
        case .accessibility3: .accessibilityExtraLarge
        case .accessibility4: .accessibilityExtraExtraLarge
        case .accessibility5: .accessibilityExtraExtraExtraLarge
        @unknown default: .large
        }
    }
}
