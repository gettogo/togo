import SwiftUI
import Testing
import UIKit
@testable import Togo

@MainActor struct AppUpdateNoticeTests {
    private func elements(_ root: NSObject) -> [(String, CGRect)] {
        var seen: Set<ObjectIdentifier> = []
        func visit(_ object: NSObject) -> [(String, CGRect)] {
            guard seen.insert(ObjectIdentifier(object)).inserted else { return [] }
            var result: [(String, CGRect)] = []
            if object.isAccessibilityElement, let label = object.accessibilityLabel {
                result.append((label, object.accessibilityFrame))
            }
            for child in object.accessibilityElements ?? [] {
                if let child = child as? NSObject { result += visit(child) }
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

    @Test(arguments: [CGFloat(280), CGFloat(700)], [DynamicTypeSize.large, .accessibility5])
    func updateNoticeKeepsItsTextAndActionsReadable(width: CGFloat, type: DynamicTypeSize) throws {
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: width, height: 1000)
        let root = AppUpdateNotice(version: "1.10.123") {}
            .environment(\.dynamicTypeSize, type)
            .frame(maxWidth: .infinity, alignment: .leading)
        let host = UIHostingController(rootView: root)
        host.safeAreaRegions = []
        window.rootViewController = host
        window.isHidden = false
        defer { window.isHidden = true }
        for _ in 0..<4 {
            window.layoutIfNeeded()
            RunLoop.main.run(until: .now + 0.05)
        }
        let labels = elements(host.view)
        let title = try #require(labels.first { $0.0.contains("1.10.123") })
        let store = try #require(labels.first { $0.0 == "Open Togo in the App Store" })
        let dismiss = try #require(labels.first { $0.0 == "Dismiss this app update" })
        for item in [title, store, dismiss] {
            #expect(item.1.minX >= -1, "\(item)")
            #expect(item.1.maxX <= width + 1, "\(item)")
        }
        #expect(store.1.height >= 44)
        #expect(dismiss.1.height >= 44)
        #expect(store.1.minY >= title.1.maxY - 1)
        #expect(!store.1.intersects(dismiss.1))
    }
}
