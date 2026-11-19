import SwiftUI
import Testing
import UIKit

/// A hosted view and its own accessibility tree, independent of other tests' windows.
@MainActor final class GeometryHost {
    struct Element {
        let label: String
        let id: String?
        let frame: CGRect
    }

    let window: UIWindow
    let controller: UIHostingController<AnyView>
    private let previous: UIWindow?
    var view: UIView { controller.view }

    init(_ content: some View, width: CGFloat, height: CGFloat) throws {
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        previous = scene.keyWindow
        controller = UIHostingController(rootView: AnyView(content))
        controller.safeAreaRegions = []
        window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: width, height: height)
        window.rootViewController = controller
        window.makeKeyAndVisible()
        settle()
    }

    func settle() {
        for _ in 0..<4 {
            window.layoutIfNeeded()
            controller.view.layoutIfNeeded()
            RunLoop.main.run(until: .now + 0.025)
        }
    }

    func close() {
        window.isHidden = true
        window.rootViewController = nil
        previous?.makeKeyAndVisible()
    }

    func native<T: UIView>(_ type: T.Type) -> T? {
        func find(_ view: UIView) -> T? {
            if let found = view as? T { return found }
            return view.subviews.lazy.compactMap(find).first
        }
        return find(view)
    }

    func elements() -> [Element] {
        var seen: Set<ObjectIdentifier> = []
        func visit(_ object: NSObject) -> [Element] {
            guard seen.insert(ObjectIdentifier(object)).inserted else { return [] }
            var result: [Element] = []
            if object.isAccessibilityElement, let label = object.accessibilityLabel {
                result.append(.init(label: label,
                    id: (object as? UIAccessibilityIdentification)?.accessibilityIdentifier,
                    frame: object.accessibilityFrame))
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
        return visit(view)
    }
}
