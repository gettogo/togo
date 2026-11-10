import SnapshotTesting
import SwiftUI
import Testing
import UIKit
@testable import Togo

/// The terminal's input status over the foot of the terminal: a picture on its way or why it
/// could not go. The terminal itself takes the typing, so there is no field to show.
@MainActor
struct TerminalInputSnapshotTests {
    private func status(support: ImageSupport = .available, phase: ImagePasteFlow.Phase) -> some View {
        let model = TerminalInputModel(
            images: ImagePasteFlow(support: support, phase: phase, upload: { _, _ in "" }, paste: { _ in }),
            paste: { _ in }
        )
        return VStack(spacing: 0) {
            Spacer()
            TerminalInputStatus(model: model)
                .modifier(ComposerMargins())
                .padding(.bottom, ComposerMargins.bottom)
        }
    }

    @Test func uploading() {
        assertScreen(status(phase: .uploading), named: "terminal-input-uploading")
    }

    @Test func uploadFailed() {
        assertScreen(status(phase: .failed(detail: "The connection to Studio was lost.")), named: "terminal-input-upload-failed")
    }

    @Test func photoLoadFailed() {
        assertScreen(status(phase: .loadFailed), named: "terminal-input-load-failed")
    }

    @Test func tooLarge() {
        assertScreen(status(phase: .tooLarge), named: "terminal-input-too-large")
    }

    @Test func noUploadCapability() {
        assertScreen(status(support: .unsupported, phase: .unsupported), named: "terminal-input-unsupported")
    }

    @Test func noHelper() {
        assertScreen(status(support: .none, phase: .noHelper), named: "terminal-input-no-helper")
    }

    @Test func annotate() {
        let view = AnnotateView(image: Self.screenshot, onCancel: {}, onSend: { _ in })
        assertScreen(view, named: "annotate")
    }

    /// A stand-in screenshot: a dark window with lines of text-like bars.
    static let screenshot: UIImage = {
        let size = CGSize(width: 1_179, height: 1_500)
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        return UIGraphicsImageRenderer(size: size, format: format).image { context in
            UIColor(white: 0.1, alpha: 1).setFill()
            context.fill(CGRect(origin: .zero, size: size))
            UIColor(white: 0.75, alpha: 1).setFill()
            for row in 0..<18 {
                let width = CGFloat([900, 640, 1_020, 480, 760][row % 5])
                context.fill(CGRect(x: 60, y: 80 + CGFloat(row) * 76, width: width, height: 28))
            }
        }
    }()
}
