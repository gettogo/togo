import SwiftUI
import Testing
import UIKit
@testable import Togo

@MainActor struct ComposerTextInputTests {
    private final class Phrase: UIDictationPhrase {
        let value: String
        init(_ value: String) { self.value = value; super.init() }
        override var text: String { value }
    }

    private func input(_ model: CommandLineModel) -> ComposerTextInput {
        ComposerTextInput(
            text: Binding(get: { model.draft }, set: { model.draft = $0 }),
            focused: .constant(false), placeholder: "Message", assists: false,
            typed: { model.typed($0) }, dictated: { model.dictated($0) },
            dictationStarted: { model.dictationStarted() },
            dictationFailed: { model.dictationFailed() }, key: { _, _, _ in .ignored }
        )
    }

    private func makeWindow() throws -> UIWindow {
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        return UIWindow(windowScene: scene)
    }

    private func native(_ host: UIHostingController<ComposerTextInput>, in window: UIWindow) throws -> ComposerTextView {
        window.frame = CGRect(x: 0, y: 0, width: 340, height: 120)
        window.rootViewController = host
        window.isHidden = false
        for _ in 0..<3 {
            window.layoutIfNeeded()
            RunLoop.main.run(until: .now + 0.05)
        }
        func find(_ view: UIView) -> ComposerTextView? {
            if let field = view as? ComposerTextView { return field }
            return view.subviews.lazy.compactMap(find).first
        }
        return try #require(find(host.view))
    }

    private func model() -> CommandLineModel {
        CommandLineModel(store: .preview(), routes: CommandLineModelTests.Session().routes())
    }

    @Test func nativeOrdinaryEditsAfterSendKeepRepeatedAndMultilineText() async throws {
        let model = model()
        model.draft = "f"
        await model.submit()
        let host = UIHostingController(rootView: input(model))
        let window = try makeWindow()
        defer { window.isHidden = true }
        let field = try native(host, in: window)
        field.insertText("f")
        #expect(field.text == "f")
        #expect(model.draft == "f")
        field.insertText("\nhttps://example.com/a?q=1&r=2\n\t--flag")
        #expect(model.draft == "f\nhttps://example.com/a?q=1&r=2\n\t--flag")
    }

    @Test func nativeDictationResultSettlesTheFieldInTheSameCallback() async throws {
        let model = model()
        model.dictationStarted()
        model.draft = "fix the test"
        await model.submit()
        let host = UIHostingController(rootView: input(model))
        let window = try makeWindow()
        defer { window.isHidden = true }
        let field = try native(host, in: window)
        field.insertDictationResult([Phrase("fix the test. Then run it")])
        #expect(model.draft == "Then run it")
        #expect(field.text == "Then run it")
        #expect(!field.insertingDictation)
    }

    @Test func nativeFailureDoesNotLeaveTheNextInputArmedAsAnEcho() async throws {
        let model = model()
        model.dictationStarted()
        model.draft = "f"
        await model.submit()
        let host = UIHostingController(rootView: input(model))
        let window = try makeWindow()
        defer { window.isHidden = true }
        let field = try native(host, in: window)
        field.dictationRecognitionFailed()
        field.insertDictationResult([Phrase("f")])
        #expect(model.draft == "f")
        #expect(field.text == "f")
    }

    @Test func nativePlaceholderCanFinishOrFailWithoutChangingOrdinaryText() throws {
        let model = model()
        let host = UIHostingController(rootView: input(model))
        let window = try makeWindow()
        defer { window.isHidden = true }
        let field = try native(host, in: window)
        let placeholder = field.insertDictationResultPlaceholder
        #expect(!field.frame(forDictationResultPlaceholder: placeholder).isNull)
        field.removeDictationResultPlaceholder(placeholder, willInsertResult: false)
        field.insertText("f")
        #expect(model.draft == "f")
        let next = field.insertDictationResultPlaceholder
        field.removeDictationResultPlaceholder(next, willInsertResult: true)
        field.insertDictationResult([Phrase("irst"), Phrase(" input")])
        #expect(model.draft == "first input")
    }

    @Test func dictationInTheMiddleKeepsTheInsertionPoint() throws {
        let model = model()
        model.draft = "hello world"
        let host = UIHostingController(rootView: input(model))
        let window = try makeWindow()
        defer { window.isHidden = true }
        let field = try native(host, in: window)
        field.selectedRange = NSRange(location: 5, length: 0)
        field.insertDictationResult([Phrase(", wide")])
        #expect(model.draft == "hello, wide world")
        #expect(field.selectedRange == NSRange(location: 11, length: 0))
    }
}
