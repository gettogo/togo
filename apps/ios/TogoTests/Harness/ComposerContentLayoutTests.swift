import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor @Suite(.serialized) struct ComposerContentLayoutTests {
    private func attachments(_ count: Int) async -> ComposerAttachments {
        let image = UIGraphicsImageRenderer(size: .init(width: 120, height: 80)).image { context in
            UIColor.gray.setFill()
            context.fill(.init(x: 0, y: 0, width: 120, height: 80))
        }
        let attachments = ComposerAttachments(upload: { name, _ in "/uploads/\(name)" }, prepare: { data in
            .init(preview: image, encoded: .init(data: data, fileExtension: "jpg"))
        })
        await attachments.add((0..<count).map { Data([UInt8($0)]) })
        return attachments
    }

    @Test func pictureStripUsesContentHeightInsteadOfFillingTheProposal() async {
        let pictures = await attachments(3)
        let host = UIHostingController(rootView: AttachmentStrip(attachments: pictures))
        for height: CGFloat in [200, 600] {
            let size = host.sizeThatFits(in: .init(width: 320, height: height))
            #expect(size.height >= 56 && size.height < 100, "A thumbnail strip must reserve its own height without taking the input's room")
        }
    }

    @Test(arguments: [1, 3], [DynamicTypeSize.large, .accessibility3])
    func picturesDraftAndStateChipNeverOverlap(count: Int, type: DynamicTypeSize) async throws {
        let pictures = await attachments(count)
        let services = PreviewHarnessServices()
        var routes = CommandLineModel.Routes.preview(services)
        routes.screens = { HarnessSamples.descriptor.screens }
        let model = CommandLineModel(store: .preview(services), routes: routes, attachments: pictures)
        model.draft = (0..<12).map { "Draft line \($0): inspect the workspace" }.joined(separator: "\n")
        let root = VStack(spacing: 0) {
            Spacer(minLength: 0)
            SmartCommandLine(model: model)
        }
        .environment(\.dynamicTypeSize, type)
        .environment(\.loopsPaused, true)
        let host = try LayoutTestHost(root, width: 340, height: 440)
        defer { host.close() }
        let field = try #require(host.native(ComposerTextView.self))
        let fieldFrame = field.convert(field.bounds, to: nil)
        let elements = host.elements()
        let picture = try #require(elements.first { $0.label == ComposerCopy.attachment(1, of: count, state: nil) })
        let chip = try #require(elements.first { $0.label == ComposerCopy.sessionState || $0.id == "composer.state" })
        #expect(picture.frame.maxY <= fieldFrame.minY + 1, "The picture must finish before the draft begins")
        #expect(fieldFrame.maxY <= chip.frame.minY + 1, "The field must finish before the model chip")
        #expect(field.isScrollEnabled, "A capped long draft must remain scrollable")
        #expect(field.clipsToBounds)
    }
}
