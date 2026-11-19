import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

/// Render the chat and its composer in the same viewport. This is synthetic
/// renderer evidence and does not credit any real harness matrix cell.
@MainActor @Suite(.serialized)
struct WholeChatGeometryTests {
    @Test(arguments: ScreenSize.allCases, geometryTextSizes)
    func questionRowsStaySeparateFromTheComposer(layout: ScreenSize, type: DynamicTypeSize) throws {
        let store = HarnessSessionStore.preview()
        let choices = (0..<3).map { index in
            HarnessChoice(id: "choice-\(index)", label: "Choice \(index): review the isolated workspace and confirm the checks before continuing.")
        }
        let wanted: [HarnessQuestion] = [
            .init(id: "scope", prompt: "First question about scope", options: choices, multi: true, allowText: false),
            .init(id: "timing", prompt: "Second question about timing", options: [.init(id: "later", label: "Continue later")], multi: false, allowText: false)
        ]
        let item = HarnessItem(id: "geometry-question", seq: 1, at: HarnessSamples.at,
                               body: .question(.init(questions: wanted)))
        store.setRows([.needs(item)], arrivals: false)
        let model = CommandLineModel(store: store)
        let host = try mount(store, model: model, layout: layout, type: type, keyboard: false)
        defer { host.close() }
        let field = try fieldFrame(in: host)
        let viewport = UIAccessibility.convertToScreenCoordinates(host.view.bounds, in: host.view)
        let labels = host.elements().filter { element in
            (element.label.hasPrefix("Choice ") || wanted.contains { $0.prompt == element.label } || element.label == "Continue later")
                && element.frame.intersects(viewport) && element.frame.minY < field.minY
        }
        try #require(labels.contains { $0.label.hasPrefix("Choice ") || $0.label == "Continue later" },
                     "A question option must be visible")
        for choice in labels where choice.label.hasPrefix("Choice ") || choice.label == "Continue later" {
            let needed = textHeight(choice.label, font: .body, width: choice.frame.width, type: type)
            #expect(choice.frame.height >= max(44, needed) - 1, "A choice must fit its full label and touch target")
        }
        let frames = labels.map(\.frame).sorted { $0.minY < $1.minY }
        for frame in frames {
            #expect(frame.maxY <= field.minY + 1, "Question content must end above the composer")
        }
        for (before, after) in zip(frames, frames.dropFirst()) {
            #expect(before.maxY <= after.minY + 1, "Questions and options must not overlap")
        }
    }

    @Test(arguments: ScreenSize.allCases, geometryTextSizes)
    func approvalCardFitsAboveTheComposer(layout: ScreenSize, type: DynamicTypeSize) throws {
        let store = HarnessSessionStore.preview()
        let card = RequestCard(
            id: "workspace-check", hostID: store.subject.hostID, sessionID: store.subject.sessionID, bib: "01",
            agentName: "Claude Code", title: "Allow the workspace check?", detail: nil,
            options: [.init(n: 1, label: "Yes", role: .approve), .init(n: 2, label: "No", role: .deny)],
            allowText: false, phase: .open
        )
        let item = HarnessItem(id: "geometry-approval", seq: 1, at: HarnessSamples.at,
                               body: .approval(.init(requestId: card.id)))
        store.setRows([.needs(item)], arrivals: false)
        let actions = ChatActions(card: { $0 == card.id ? card : nil }, answer: { _, _ in }, quote: { _ in })
        let host = try mount(store, model: CommandLineModel(store: store), layout: layout, type: type,
                             keyboard: false, actions: actions)
        defer { host.close() }
        let field = try fieldFrame(in: host)
        let label = ChatCopy.spokenNeeds(card.title)
        let elements = host.elements()
        let approval = try #require(elements.first { $0.id == "chat.needs.approval" || $0.label == label },
                                    "Approval card missing from visible controls: \(elements.map(\.label))")
        try requireFiniteFrame(approval.frame)
        let viewport = UIAccessibility.convertToScreenCoordinates(host.view.bounds.inset(by: layout.safeArea), in: host.view)
        #expect(viewport.insetBy(dx: -1, dy: -1).contains(approval.frame), "The short request must fit in the initial viewport")
        #expect(approval.frame.maxY <= field.minY + 1, "The request card must stay above the input")
    }

    @Test(arguments: ScreenSize.allCases, geometryTextSizes)
    func picturesDraftAndComposerActionsDoNotOverlap(layout: ScreenSize, type: DynamicTypeSize) async throws {
        let image = UIGraphicsImageRenderer(size: .init(width: 120, height: 80)).image { context in
            UIColor.gray.setFill()
            context.fill(.init(x: 0, y: 0, width: 120, height: 80))
        }
        let pictures = ComposerAttachments(upload: { name, _ in "/uploads/\(name)" }, prepare: { data in
            .init(preview: image, encoded: .init(data: data, fileExtension: "jpg"))
        })
        await pictures.add([Data([1]), Data([2]), Data([3])])
        let services = PreviewHarnessServices()
        let store = HarnessSessionStore.preview(services)
        var routes = CommandLineModel.Routes.preview(services)
        routes.screens = { HarnessSamples.descriptor.screens }
        let model = CommandLineModel(store: store, routes: routes, attachments: pictures)
        model.draft = (0..<12).map { "Draft line \($0): inspect the workspace" }.joined(separator: "\n")
        let host = try mount(store, model: model, layout: layout, type: type, keyboard: true)
        defer { host.close() }
        let field = try fieldFrame(in: host)
        let elements = host.elements()
        let first = try #require(elements.first { $0.label == ComposerCopy.attachment(1, of: 3, state: nil) })
        let chip = try #require(elements.first { $0.id == "composer.state" || $0.label == ComposerCopy.sessionState })
        try requireFiniteFrame(first.frame)
        try requireFiniteFrame(chip.frame)
        #expect(first.frame.maxY <= field.minY + 1, "Photo thumbnails must finish above the draft")
        #expect(field.maxY <= chip.frame.minY + 1, "The draft must finish above the model chip")
        #expect(host.native(ComposerTextView.self)?.isScrollEnabled == true)
    }

    @Test(arguments: ScreenSize.allCases, geometryTextSizes)
    func refusalTextWrapsAboveTheField(layout: ScreenSize, type: DynamicTypeSize) async throws {
        let services = PreviewHarnessServices()
        let store = HarnessSessionStore.preview(services)
        var routes = CommandLineModel.Routes.preview(services)
        let explanation = "The remote input holds a draft. Close the open dialog on the machine and try again. Your message was kept."
        routes.send = { _, _ in
            throw HelperError.rpc(.init(code: -32008, message: "Refused", data: .object(["message": .string(explanation)])))
        }
        let model = CommandLineModel(store: store, routes: routes)
        model.draft = "Check the workspace"
        await model.submit()
        let notice = try #require(model.notice)
        let host = try mount(store, model: model, layout: layout, type: type, keyboard: true)
        defer { host.close() }
        let field = try fieldFrame(in: host)
        let words = try #require(host.elements().first { $0.label == notice })
        try requireFiniteFrame(words.frame)
        let needed = textHeight(notice, font: .footnote.weight(.semibold), width: words.frame.width, type: type)
        #expect(words.frame.height >= needed - 1, "The refusal must make room for its full explanation")
        #expect(words.frame.maxY <= field.minY + 1, "The refusal must remain above the input")
    }

    private func mount(_ store: HarnessSessionStore, model: CommandLineModel, layout: ScreenSize,
                       type: DynamicTypeSize, keyboard: Bool, actions: ChatActions = .inert) throws -> GeometryHost {
        let height = layout.size.height - (keyboard ? min(300, layout.size.height * 0.4) : 0)
        let inset = layout.safeArea
        let view = ChatView(store: store, actions: actions)
            .safeAreaInset(edge: .bottom, spacing: 0) { ChatComposerBar(model: model) }
            .padding(EdgeInsets(top: inset.top, leading: inset.left, bottom: inset.bottom, trailing: inset.right))
            .frame(width: layout.size.width, height: height)
            .environment(\.dynamicTypeSize, type)
            .environment(\.horizontalSizeClass, layout.sizeClass)
            .environment(\.fold, layout.inSafeArea(layout.fold))
            .environment(\.barIsVerticalOverride, layout == .duoFolded ? true : nil)
            .environment(\.loopsPaused, true)
        return try GeometryHost(view, width: layout.size.width, height: height)
    }

    private func textHeight(_ text: String, font: Font, width: CGFloat, type: DynamicTypeSize) -> CGFloat {
        let reference = UIHostingController(rootView: Text(verbatim: text).font(font)
            .environment(\.dynamicTypeSize, type).fixedSize(horizontal: false, vertical: true))
        return reference.sizeThatFits(in: .init(width: width, height: 2000)).height
    }

    private func fieldFrame(in host: GeometryHost) throws -> CGRect {
        let field = try #require(host.native(ComposerTextView.self))
        let frame = UIAccessibility.convertToScreenCoordinates(field.bounds, in: field)
        try requireFiniteFrame(frame)
        return frame
    }

    private func requireFiniteFrame(_ frame: CGRect) throws {
        try #require(!frame.isEmpty && [frame.minX, frame.minY, frame.width, frame.height].allSatisfy(\.isFinite),
                     "The expected visible control must have a finite, nonempty frame")
    }
}
