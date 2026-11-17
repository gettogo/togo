import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor struct AgentsComposerLayoutTests {
    static func fixture(agents: Int = 4, question: Bool = false, supportsAgents: Bool = true) async throws -> HarnessSessionStore {
        var descriptor = ChatTests.claude
        descriptor.capabilities += [HarnessCapability.background, HarnessCapability.needsQuestions]
        if supportsAgents { descriptor.capabilities.append(HarnessCapability.agents) }
        let services = FakeHarnessServices(descriptor: descriptor)
        let items: [HarnessItem] = (0..<24).map { n in
            .init(id: "reply-\(n)", seq: n + 1, turnId: "turn", at: HarnessSamples.at,
                  body: .assistant(.init(text: "Response \(n). " + String(repeating: "The deployment checks preserve workspace identity and show clear progress on a slow connection.\n\n", count: 4), partial: false)))
        }
        var snapshot = HarnessOpenResult(epoch: "agents-layout", cursor: items.count, items: items, more: false,
            mission: .init(rev: 1, summary: .init(agents: agents, working: true), sections: [
                .init(kind: .agents, rows: (0..<agents).map { .init(id: "agent-\($0)", title: "Review workspace \($0)", state: .running) })
            ]))
        if question {
            snapshot.items.append(.init(id: "request-layout", seq: 25, turnId: "turn", at: HarnessSamples.at,
                body: .question(.init(questions: [.init(id: "deployment", prompt: "Which deployment environment should receive the workspace configuration changes? Review the pending checks before continuing.", options: [.init(id: "preview", label: "Preview environment with isolated workspace data"), .init(id: "production", label: "Production after all required checks pass")], multi: false, allowText: true)]))))
            snapshot.cursor = 25
        }
        services.feeds = [.init(elements: [.opened(snapshot)], end: .hold)]
        let store = HarnessSessionStore(subject: ChatTests.subject, services: services)
        store.launch()
        let deadline = ContinuousClock.now + .seconds(3)
        while store.phase != .live, ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        try #require(store.phase == .live)
        return store
    }

    @Test(arguments: [CGFloat(320), CGFloat(700)], [DynamicTypeSize.large, .accessibility3])
    func zeroAgentsLeaveNoHeaderSpace(width: CGFloat, type: DynamicTypeSize) async throws {
        let store = try await Self.fixture(agents: 0)
        defer { store.stop() }
        let model = CommandLineModel(store: store)
        var heights: [CGFloat] = []
        for count in [0, 1, 4, 0] {
            var snapshot = HarnessOpenResult(epoch: store.timeline.epoch ?? "agents-layout", cursor: 24, items: store.timeline.items, more: false,
                mission: .init(rev: 1, summary: .init(agents: count, working: true), sections: [
                    .init(kind: .agents, rows: (0..<count).map { .init(id: "agent-\($0)", title: "Review", state: .running) })
                ]))
            snapshot.epoch = "agents-layout"
            _ = store.feed.load(snapshot)
            let host = UIHostingController(rootView: SmartCommandLine(model: model, agentsReconciled: true).environment(\.dynamicTypeSize, type))
            heights.append(host.sizeThatFits(in: .init(width: width, height: 2000)).height)
        }
        let bare = try await Self.fixture(agents: 0, supportsAgents: false)
        defer { bare.stop() }
        let host = UIHostingController(rootView: SmartCommandLine(model: CommandLineModel(store: bare)).environment(\.dynamicTypeSize, type))
        let baseline = host.sizeThatFits(in: .init(width: width, height: 2000)).height
        #expect(abs(heights[0] - baseline) < 1 && abs(heights[3] - baseline) < 1, "Zero agents must add no height: \(heights), baseline \(baseline)")
        #expect(heights[1] > baseline && heights[2] > baseline)
    }
    @Test func openingMissionControlDismissesTheActiveComposer() throws {
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let previous = scene.windows.first(where: \.isKeyWindow)
        let window = UIWindow(windowScene: scene)
        let controller = UIViewController()
        let field = UITextField(frame: .init(x: 16, y: 16, width: 250, height: 44))
        controller.view.addSubview(field)
        window.rootViewController = controller
        window.makeKeyAndVisible()
        defer { window.isHidden = true; previous?.makeKey() }
        #expect(field.becomeFirstResponder())
        let store = HarnessSessionStore(subject: ChatTests.subject, services: FakeHarnessServices(descriptor: ChatTests.claude))
        store.showsMission = true
        #expect(!field.isFirstResponder)
        #expect(field.becomeFirstResponder())
        store.showsMission = false
        #expect(field.isFirstResponder, "Closing must not dismiss focus in another screen")
        store.showAgents()
        #expect(!field.isFirstResponder)
        #expect(store.missionSection == .agents)
    }

    @Test func nativeInputUsesTheSameLargeTypeSizeAsItsPlaceholder() {
        let normal = ComposerTextInput.font(for: .large)
        let large = ComposerTextInput.font(for: .accessibility3)
        #expect(large.pointSize > normal.pointSize * 1.5)
        let expected = UIFont.preferredFont(forTextStyle: .body, compatibleWith: UITraitCollection(preferredContentSizeCategory: .accessibilityExtraLarge))
        #expect(large.pointSize == expected.pointSize)
    }

    @Test func requestNavigationCanShowTheQuestionWithoutChangingSendAnchors() {
        let scroll = ChatScrollModel()
        scroll.go(.row("question"), reduce: true, anchor: .top)
        #expect(scroll.request?.anchor == .top)
        scroll.go(.row("prompt"), reduce: true)
        #expect(scroll.request?.anchor == ChatScroll.foot)
    }

    @Test func sidebarCountsKeepEachNumberWithItsState() {
        let model = RosterModel.make(machines: Samples.controlCenter.machines, requests: [])
        let pairs = model.tallyDisplayText.components(separatedBy: "  ")
        #expect(pairs.allSatisfy { !$0.contains(" ") })
        #expect(model.tallyDisplayText.replacingOccurrences(of: "\u{00a0}", with: " ") == model.tallyText)
    }

}
