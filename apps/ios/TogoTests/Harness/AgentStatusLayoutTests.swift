import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor struct AgentStatusLayoutTests {
    private func render(_ root: some View, width: CGFloat) throws -> (UIWindow, UIView) {
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: width, height: 1600)
        let host = UIHostingController(rootView: root)
        host.safeAreaRegions = []
        window.rootViewController = host
        window.isHidden = false
        for _ in 0..<4 {
            window.layoutIfNeeded()
            RunLoop.main.run(until: .now + 0.05)
        }
        return (window, host.view)
    }

    private func elements(_ root: NSObject) -> [(String, CGRect)] {
        var seen: Set<ObjectIdentifier> = []
        func visit(_ object: NSObject) -> [(String, CGRect)] {
            guard seen.insert(ObjectIdentifier(object)).inserted else { return [] }
            var result: [(String, CGRect)] = []
            if object.isAccessibilityElement, let label = object.accessibilityLabel {
                result.append((label, object.accessibilityFrame))
            }
            if let children = object.accessibilityElements {
                for child in children { if let child = child as? NSObject { result += visit(child) } }
            }
            let count = object.accessibilityElementCount()
            if count > 0, count < 1000 {
                for index in 0..<count {
                    if let child = object.accessibilityElement(at: index) as? NSObject { result += visit(child) }
                }
            }
            if let view = object as? UIView { for child in view.subviews { result += visit(child) } }
            return result
        }
        return visit(root)
    }

    @Test func anUnnamedAgentStillHasAReadableLabel() throws {
        let row = HarnessRow(id: "worker-42", title: " \n", state: .running)
        let (window, view) = try render(MissionRowView(row: row, opens: true), width: 340)
        defer { window.isHidden = true }
        let labels = elements(view).map(\.0)
        #expect(labels.contains { $0.contains("worker-42") }, "Rendered labels: \(labels)")
    }

    @Test(arguments: [CGFloat(320), CGFloat(700)], [DynamicTypeSize.large, .accessibility5])
    func wrappedStatusCellsShareTheirRowTopAndHeight(width: CGFloat, type: DynamicTypeSize) throws {
        let status = HarnessStatus(model: "moonshotai/kimi-k2-thinking-turbo", mode: "Manual", effort: "High", costUsd: 1.25)
        let (window, view) = try render(
            StatusPrimitive(status: status, context: .inert, embedded: true).environment(\.dynamicTypeSize, type), width: width
        )
        defer { window.isHidden = true }
        let cells = elements(view)
        let model = try #require(cells.first { $0.0.contains("moonshotai/") })
        let mode = try #require(cells.first { $0.0.contains("Manual") })
        #expect(abs(model.1.minY - mode.1.minY) < 1, "Cell tops: \(model), \(mode)")
        #expect(abs(model.1.height - mode.1.height) < 1, "Cell heights: \(model), \(mode)")
        #expect(model.1.maxX <= view.frame.maxX + 1)
    }

    @Test func statusCellsKeepTheirReadingOrderInRightToLeftLayouts() throws {
        let status = HarnessStatus(model: "moonshotai/kimi-k2-thinking-turbo", mode: "Manual", effort: "High")
        let (window, view) = try render(
            StatusPrimitive(status: status, context: .inert, embedded: true).environment(\.layoutDirection, .rightToLeft), width: 700
        )
        defer { window.isHidden = true }
        let cells = elements(view)
        let model = try #require(cells.first { $0.0.contains("moonshotai/") })
        let mode = try #require(cells.first { $0.0.contains("Manual") })
        #expect(model.1.minX > mode.1.minX)
        #expect(abs(model.1.minY - mode.1.minY) < 1)
    }
}
