import SwiftUI
import UIKit

/// Owns the native text input so dictated results have a source. Ordinary
/// typing and paste never pass through the dictated-result filter.
struct ComposerTextInput: UIViewRepresentable {
    @Binding var text: String
    @Binding var focused: Bool
    var placeholder: String
    var assists: Bool
    var typed: (String) -> Void
    var dictated: (String) -> Void
    var dictationStarted: () -> Void
    var dictationFailed: () -> Void
    var key: (KeyEquivalent, EventModifiers, String) -> KeyPress.Result

    func makeUIView(context: Context) -> ComposerTextView {
        let view = ComposerTextView()
        view.delegate = context.coordinator
        view.backgroundColor = .clear
        view.textContainerInset = .zero
        view.textContainer.lineFragmentPadding = 0
        view.adjustsFontForContentSizeCategory = true
        view.smartQuotesType = .no
        view.smartDashesType = .no
        view.accessibilityIdentifier = "composer.field"
        view.onDictationStarted = { context.coordinator.parent.dictationStarted() }
        view.onDictationFailed = { context.coordinator.parent.dictationFailed() }
        view.onDictationResult = { [weak view] in
            guard let view else { return }
            let parent = context.coordinator.parent
            parent.dictated(view.text)
            // Update the native field in the same input transaction. A later
            // delegate callback then reads the settled text, never an old echo.
            view.text = parent.text
            view.selectedRange = NSRange(location: (view.text as NSString).length, length: 0)
        }
        view.onKey = { context.coordinator.parent.key($0, $1, $2) }
        return view
    }

    func updateUIView(_ view: ComposerTextView, context: Context) {
        context.coordinator.parent = self
        view.font = UIFont.monospacedSystemFont(ofSize: UIFont.preferredFont(forTextStyle: .body).pointSize, weight: .regular)
        view.textColor = UIColor(Color.togoSnow)
        view.accessibilityLabel = placeholder
        view.autocapitalizationType = assists ? .sentences : .none
        view.autocorrectionType = assists ? .yes : .no
        if view.text != text {
            view.text = text
            view.selectedRange = NSRange(location: (text as NSString).length, length: 0)
        }
        if focused, !view.isFirstResponder {
            view.becomeFirstResponder()
        } else if !focused, view.isFirstResponder {
            view.resignFirstResponder()
        }
    }

    func sizeThatFits(_ proposal: ProposedViewSize, uiView: ComposerTextView, context: Context) -> CGSize? {
        guard let width = proposal.width, width > 0 else { return nil }
        let line = uiView.font?.lineHeight ?? 21
        let fitting = uiView.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude))
        return CGSize(width: width, height: max(line, min(fitting.height, line * 6)))
    }

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    @MainActor final class Coordinator: NSObject, UITextViewDelegate {
        var parent: ComposerTextInput
        init(_ parent: ComposerTextInput) { self.parent = parent }

        func textViewDidChange(_ textView: UITextView) {
            guard (textView as? ComposerTextView)?.insertingDictation != true else { return }
            parent.typed(textView.text)
        }

        func textViewDidBeginEditing(_ textView: UITextView) {
            if !parent.focused { parent.focused = true }
        }

        func textViewDidEndEditing(_ textView: UITextView) {
            if parent.focused { parent.focused = false }
        }
    }
}

@MainActor final class ComposerTextView: UITextView {
    var onDictationStarted: () -> Void = {}
    var onDictationFailed: () -> Void = {}
    var onDictationResult: () -> Void = {}
    var onKey: (KeyEquivalent, EventModifiers, String) -> KeyPress.Result = { _, _, _ in .ignored }
    private(set) var insertingDictation = false

    override var insertDictationResultPlaceholder: Any {
        onDictationStarted()
        return super.insertDictationResultPlaceholder
    }

    override func insertDictationResult(_ dictationResult: [UIDictationPhrase]) {
        insertingDictation = true
        defer { insertingDictation = false }
        super.insertDictationResult(dictationResult)
        onDictationResult()
    }

    override func dictationRecognitionFailed() {
        super.dictationRecognitionFailed()
        onDictationFailed()
    }

    override func pressesBegan(_ presses: Set<UIPress>, with event: UIPressesEvent?) {
        let unhandled = presses.filter { press in
            guard markedTextRange == nil, let input = press.key, let equivalent = Self.equivalent(input) else { return true }
            var modifiers: EventModifiers = []
            if input.modifierFlags.contains(.shift) { modifiers.insert(.shift) }
            if input.modifierFlags.contains(.control) { modifiers.insert(.control) }
            if input.modifierFlags.contains(.alternate) { modifiers.insert(.option) }
            if input.modifierFlags.contains(.command) { modifiers.insert(.command) }
            return onKey(equivalent, modifiers, input.characters) != .handled
        }
        if !unhandled.isEmpty { super.pressesBegan(unhandled, with: event) }
    }

    private static func equivalent(_ key: UIKey) -> KeyEquivalent? {
        switch key.keyCode {
        case .keyboardReturnOrEnter, .keypadEnter: .return
        case .keyboardUpArrow: .upArrow
        case .keyboardDownArrow: .downArrow
        case .keyboardTab: .tab
        case .keyboardEscape: .escape
        default: key.charactersIgnoringModifiers.count == 1 ? key.charactersIgnoringModifiers.first.map { KeyEquivalent($0) } : nil
        }
    }
}
