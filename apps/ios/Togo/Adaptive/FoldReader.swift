import SwiftUI

extension View {
    /// Puts the fold, in this view's coordinates, into `\.fold` for everything inside. A no-op
    /// before iOS 27.1 and on devices without a fold.
    func readsFold() -> some View {
        modifier(FoldReader())
    }
}

private struct FoldReader: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 27.1, *) {
            GeometryReader { proxy in
                let regions = proxy.reservedRegions(
                    kind: .division, options: .includeInactive, layoutDirectionBehavior: .fixed
                )
                let active = regions.first { $0.isActive }?.frame
                let inactive = regions.first { !$0.isActive }?.frame ?? active
                content.environment(\.fold, .from(active: active, inactive: inactive))
            }
        } else {
            content
        }
    }
}
