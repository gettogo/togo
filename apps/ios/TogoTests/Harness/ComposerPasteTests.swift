import Foundation
import Testing
import UIKit
import UniformTypeIdentifiers
@testable import Togo

/// Pasting into the composer (owner: "particular around links and multiline. A lot screw that
/// up and add extra characters", "I want to paste in images"): text lands exactly as copied,
/// only typed punctuation is straightened, pictures become chips and files their path.
@MainActor
struct ComposerPasteTests {
    // MARK: Text keeps every character

    @Test(arguments: [
        "https://example.com/a_b?q=1&r=%20x#frag",
        "see https://github.com/org/repo/pull/123/files#diff-abc",
        "line one\nline two\n\nline four",
        "for f in *; do\n\techo \"$f\"\ndone",
        "It’s “quoted” — with an em dash – and ‘single’",
        "npm test — —watch",
        "a\u{00A0}b\tc",
    ])
    func pastedTextIsKeptExactly(_ pasted: String) {
        #expect(CommandInput.settled(from: "", to: pasted) == pasted)
        #expect(CommandInput.settled(from: "ask: ", to: "ask: " + pasted) == "ask: " + pasted)
        #expect(CommandInput.settled(from: "ab", to: "a" + pasted + "b") == "a" + pasted + "b")
    }

    @Test func pastedLineEndingsBecomeLineFeeds() {
        #expect(CommandInput.settled(from: "", to: "one\r\ntwo\r\nthree") == "one\ntwo\nthree")
        #expect(CommandInput.settled(from: "x ", to: "x one\rtwo") == "x one\ntwo")
    }

    @Test func whatWasThereBeforeIsNeverTouched() {
        // An em dash someone pasted earlier stays when a word is typed after it.
        let before = "pasted — dash"
        #expect(CommandInput.settled(from: before, to: before + "s") == before + "s")
    }

    @Test func typedSmartPunctuationIsStillStraightened() {
        #expect(CommandInput.settled(from: "say ", to: "say \u{201C}") == "say \"")
        #expect(CommandInput.settled(from: "npm test -", to: "npm test \u{2014}") == "npm test --")
        #expect(CommandInput.settled(from: "don", to: "don\u{2019}") == "don'")
        #expect(CommandInput.settled(from: "don t", to: "don\u{2019}t") == "don\u{2019}t")
    }

    @Test func deletingChangesNothing() {
        #expect(CommandInput.settled(from: "a — b", to: "a — ") == "a — ")
    }

    // MARK: Routing

    @Test func screenshotsAndPhotosAreImages() {
        #expect(ComposerPasteRoute.kind([UTType.png.identifier]) == .image)
        #expect(ComposerPasteRoute.kind([UTType.heic.identifier, UTType.jpeg.identifier]) == .image)
        // A web image carries its address too; it is still a picture.
        #expect(ComposerPasteRoute.kind([UTType.jpeg.identifier, UTType.url.identifier]) == .image)
    }

    @Test func wordsWinOverAPictureInTheSameItem() {
        let notes = [UTType.flatRTFD.identifier, UTType.utf8PlainText.identifier, UTType.png.identifier]
        #expect(ComposerPasteRoute.kind(notes) == .text)
        #expect(ComposerPasteRoute.kind([UTType.url.identifier]) == .text)
        #expect(ComposerPasteRoute.kind([UTType.html.identifier]) == .text)
        #expect(ComposerPasteRoute.kind([UTType.json.identifier]) == .text)
    }

    @Test func otherFilesAreFiles() {
        #expect(ComposerPasteRoute.kind([UTType.pdf.identifier]) == .file)
        #expect(ComposerPasteRoute.kind([UTType.zip.identifier]) == .file)
        #expect(ComposerPasteRoute.fileType([UTType.fileURL.identifier, UTType.pdf.identifier]) == UTType.pdf.identifier)
    }

    @Test func theImagesOwnEncodingIsRead() {
        #expect(ComposerPasteRoute.imageType([UTType.png.identifier, UTType.jpeg.identifier]) == UTType.jpeg.identifier)
        #expect(ComposerPasteRoute.imageType([UTType.gif.identifier]) == UTType.gif.identifier)
        #expect(ComposerPasteRoute.imageType([UTType.pdf.identifier]) == nil)
    }

    @Test func aFilePathIsTypedWithOneSpaceEachSide() {
        #expect(ComposerPasteRoute.pathInsert("/u/notes.pdf", after: "") == "/u/notes.pdf ")
        #expect(ComposerPasteRoute.pathInsert("/u/notes.pdf", after: "read") == " /u/notes.pdf ")
        #expect(ComposerPasteRoute.pathInsert("/u/notes.pdf", after: "read ") == "/u/notes.pdf ")
        #expect(ComposerPasteRoute.pathInsert("/u/my notes.pdf", after: "") == "'/u/my notes.pdf' ")
    }

    @Test func fileNamesKeepTheirExtension() {
        #expect(ComposerPasteRoute.fileName("report", type: UTType.pdf.identifier) == "report.pdf")
        #expect(ComposerPasteRoute.fileName("report.PDF", type: UTType.pdf.identifier) == "report.PDF")
        #expect(ComposerPasteRoute.fileName(nil, type: UTType.pdf.identifier) == "file.pdf")
    }

    // MARK: The delegate

    @Test func aPastedScreenshotGoesToTheAttachmentsNotTheText() async throws {
        let png = try #require(UIGraphicsImageRenderer(size: CGSize(width: 4, height: 4)).image { _ in }.pngData())
        let item = FakePasteItem(NSItemProvider(item: png as NSData, typeIdentifier: UTType.png.identifier))
        let delegate = ComposerPasteDelegate()
        let seen = Seen()
        delegate.onImages = { seen.images += $0 }
        delegate.textPasteConfigurationSupporting(UITextView(), transform: item)
        #expect(item.result == .none)
        #expect(await poll { !seen.images.isEmpty })
        #expect(seen.images == [png])
    }

    @Test func pastedTextTakesUIKitsOwnPath() {
        let item = FakePasteItem(NSItemProvider(object: "a\nb" as NSString))
        let delegate = ComposerPasteDelegate()
        delegate.onImages = { _ in Issue.record("text is not a picture") }
        delegate.textPasteConfigurationSupporting(UITextView(), transform: item)
        #expect(item.result == .default)
    }

    @Test func aPictureWithNowhereToGoIsRefusedOutLoud() throws {
        let png = try #require(UIGraphicsImageRenderer(size: CGSize(width: 2, height: 2)).image { _ in }.pngData())
        let item = FakePasteItem(NSItemProvider(item: png as NSData, typeIdentifier: UTType.png.identifier))
        let delegate = ComposerPasteDelegate()
        var refused = false
        delegate.onRefused = { refused = true }
        delegate.textPasteConfigurationSupporting(UITextView(), transform: item)
        #expect(refused)
        #expect(item.result == .none)
    }

    @Test func aPastedFileIsUploadedAndItsPathTyped() async {
        let item = FakePasteItem(NSItemProvider(item: Data("%PDF-1.4".utf8) as NSData, typeIdentifier: UTType.pdf.identifier))
        item.itemProvider.suggestedName = "spec"
        let delegate = ComposerPasteDelegate()
        let seen = Seen()
        delegate.onFile = { seen.file = ($0, $1) }
        delegate.textPasteConfigurationSupporting(UITextView(), transform: item)
        #expect(item.result == .none)
        #expect(await poll { seen.file != nil })
        #expect(seen.file?.0 == "spec.pdf")
        #expect(seen.file?.1 == Data("%PDF-1.4".utf8))
    }

    @Test func aFileOverTheHelpersLimitSaysSo() async {
        var uploads = 0
        let attachments = ComposerAttachments(upload: { _, _ in uploads += 1; return "/u/x" })
        let path = await attachments.upload(file: "big.bin", data: Data(count: ComposerAttachments.fileLimit + 1))
        #expect(path == nil)
        #expect(uploads == 0)
        #expect(attachments.notice == ComposerCopy.fileTooLarge)
        #expect(await attachments.upload(file: "small.txt", data: Data("hi".utf8)) == "/u/x")
    }

    private func poll(_ condition: () -> Bool) async -> Bool {
        for _ in 0..<200 {
            if condition() { return true }
            try? await Task.sleep(for: .milliseconds(10))
        }
        return condition()
    }
}

/// What the delegate handed on, from its main-actor callbacks.
@MainActor private final class Seen {
    var images: [Data] = []
    var file: (String, Data)?
}

/// A paste item that records which result the delegate chose.
@MainActor private final class FakePasteItem: NSObject, UITextPasteItem {
    enum Result: Equatable { case unset, none, `default`, string(String), attributed(String), attachment }

    let itemProvider: NSItemProvider
    let localObject: Any? = nil
    let defaultAttributes: [NSAttributedString.Key: Any] = [:]
    private(set) var result = Result.unset

    init(_ provider: NSItemProvider) { itemProvider = provider }

    func setResult(string: String) { result = .string(string) }
    func setResult(attributedString: NSAttributedString) { result = .attributed(attributedString.string) }
    func setResult(attachment: NSTextAttachment) { result = .attachment }
    func setNoResult() { result = .none }
    func setDefaultResult() { result = .default }
}
