import SnapshotTesting
import SwiftUI
import Testing
import UIKit
@testable import Togo

@MainActor
struct ComposeSnapshotTests {
    private func bar(
        text: String = "", history: [String] = [], support: ImageSupport = .available,
        phase: ImagePasteFlow.Phase = .idle
    ) -> some View {
        let defaults = UserDefaults(suiteName: "compose-snapshots-\(UUID().uuidString)")!
        let store = ComposeHistory(hostID: Samples.studio, sessionID: "s-04", defaults: defaults)
        store.save(history)
        let model = ComposeModel(
            text: text, history: store,
            images: ImagePasteFlow(support: support, phase: phase, upload: { _, _ in "" }, paste: { _ in }),
            paste: { _ in }, pressEnter: {}
        )
        return VStack(spacing: 0) {
            Spacer()
            ComposeBar(model: model, focusOnAppear: false)
        }
    }

    @Test func composeEmpty() {
        assertScreen(bar(), named: "compose-empty")
    }

    @Test func composeMultiLine() {
        let text = "The login test fails on CI but passes locally.\nCheck the timezone in the fixture,\nthen run the suite again."
        assertScreen(bar(text: text, history: ["Run the tests"]), named: "compose-multiline")
    }

    @Test func composeWithoutHelperHidesPhoto() {
        assertScreen(bar(text: "git status", support: .none), named: "compose-no-helper")
    }

    @Test func uploading() {
        assertScreen(bar(phase: .uploading), named: "compose-uploading")
    }

    @Test func uploadFailed() {
        assertScreen(bar(phase: .failed(detail: "The connection to Studio was lost.")), named: "compose-upload-failed")
    }

    @Test func photoLoadFailed() {
        assertScreen(bar(phase: .loadFailed), named: "compose-load-failed")
    }

    @Test func tooLarge() {
        assertScreen(bar(phase: .tooLarge), named: "compose-too-large")
    }

    @Test func noUploadCapability() {
        assertScreen(bar(support: .unsupported, phase: .unsupported), named: "compose-unsupported")
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
