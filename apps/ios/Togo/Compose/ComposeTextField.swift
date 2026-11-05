import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// A growing text view that takes image pastes. SwiftUI's `TextField` offers no hook for a
/// pasted image, which is the quickest way to share a screenshot with an agent.
struct ComposeTextField: UIViewRepresentable {
    @Binding var text: String
    let focusOnAppear: Bool
    let onPasteImage: (Data) -> Void
    let onSubmit: () -> Void

    static let maxLines: CGFloat = 6

    func makeUIView(context: Context) -> PastingTextView {
        let view = PastingTextView(placeholder: ComposeCopy.placeholder)
        view.delegate = context.coordinator
        view.focusOnAppear = focusOnAppear
        view.text = text
        view.refreshPlaceholder()
        return view
    }

    func updateUIView(_ view: PastingTextView, context: Context) {
        context.coordinator.parent = self
        view.onPasteImage = onPasteImage
        view.onSubmit = onSubmit
        if view.text != text {
            view.text = text
            view.refreshPlaceholder()
        }
    }

    func sizeThatFits(_ proposal: ProposedViewSize, uiView view: PastingTextView, context: Context) -> CGSize? {
        let width = proposal.width ?? 240
        let fitting = view.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude)).height
        let font = view.font ?? .preferredFont(forTextStyle: .body)
        let inset = view.textContainerInset.top + view.textContainerInset.bottom
        return CGSize(width: width, height: min(fitting, font.lineHeight * Self.maxLines + inset))
    }

    func makeCoordinator() -> Coordinator {
        Coordinator(parent: self)
    }

    @MainActor final class Coordinator: NSObject, UITextViewDelegate {
        var parent: ComposeTextField

        init(parent: ComposeTextField) {
            self.parent = parent
        }

        func textViewDidChange(_ textView: UITextView) {
            parent.text = textView.text
            (textView as? PastingTextView)?.refreshPlaceholder()
        }
    }
}

final class PastingTextView: UITextView {
    var onPasteImage: ((Data) -> Void)?
    var onSubmit: (() -> Void)?
    var focusOnAppear = false
    private let placeholderLabel = UILabel()

    init(placeholder: String) {
        super.init(frame: .zero, textContainer: nil)
        font = .preferredFont(forTextStyle: .body)
        adjustsFontForContentSizeCategory = true
        textColor = .togoSnow
        tintColor = .togoSnow
        backgroundColor = .clear
        textContainerInset = UIEdgeInsets(top: 9, left: 10, bottom: 9, right: 10)
        // Agents read prose; autocorrect and smart punctuation help here, unlike in the terminal.
        autocapitalizationType = .sentences
        placeholderLabel.text = placeholder
        placeholderLabel.font = font
        placeholderLabel.adjustsFontForContentSizeCategory = true
        placeholderLabel.textColor = .togoMute
        placeholderLabel.isAccessibilityElement = false
        accessibilityLabel = placeholder
        placeholderLabel.translatesAutoresizingMaskIntoConstraints = false
        addSubview(placeholderLabel)
        NSLayoutConstraint.activate([
            placeholderLabel.leadingAnchor.constraint(
                equalTo: frameLayoutGuide.leadingAnchor, constant: textContainerInset.left + textContainer.lineFragmentPadding
            ),
            placeholderLabel.topAnchor.constraint(equalTo: frameLayoutGuide.topAnchor, constant: textContainerInset.top),
        ])
    }

    required init?(coder: NSCoder) {
        nil
    }

    func refreshPlaceholder() {
        placeholderLabel.isHidden = !text.isEmpty
    }

    override func didMoveToWindow() {
        super.didMoveToWindow()
        if window != nil, focusOnAppear {
            focusOnAppear = false
            becomeFirstResponder()
        }
    }

    override var keyCommands: [UIKeyCommand]? {
        let send = UIKeyCommand(title: ComposeCopy.send, action: #selector(submit), input: "\r", modifierFlags: .command)
        return (super.keyCommands ?? []) + [send]
    }

    @objc private func submit() {
        onSubmit?()
    }

    override func canPerformAction(_ action: Selector, withSender sender: Any?) -> Bool {
        if action == #selector(paste(_:)), UIPasteboard.general.hasImages { return true }
        return super.canPerformAction(action, withSender: sender)
    }

    /// Text wins when the pasteboard holds both, as when copying from a web page.
    override func paste(_ sender: Any?) {
        let board = UIPasteboard.general
        if !board.hasStrings, board.hasImages, let data = Self.imageData(on: board) {
            onPasteImage?(data)
            return
        }
        super.paste(sender)
    }

    /// The encoded bytes as copied, so a large image is downsampled from its file rather than
    /// decoded whole the way `UIPasteboard.image` would.
    private static func imageData(on board: UIPasteboard) -> Data? {
        for type in board.types where UTType(type)?.conforms(to: .image) == true {
            if let data = board.data(forPasteboardType: type) { return data }
        }
        return board.image?.pngData()
    }
}
