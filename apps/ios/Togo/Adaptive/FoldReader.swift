import SwiftUI

extension View {
    /// Puts the fold, in this view's coordinates, into `\.fold` for everything inside. A no-op
    /// before iOS 27.1 and on devices without a fold.
    func readsFold() -> some View {
        modifier(FoldReader())
    }
}

private struct FoldReader: ViewModifier {
    @Environment(\.fold) private var inherited
    @Environment(\.foldOrigin) private var anchor

    func body(content: Content) -> some View {
        if #available(iOS 27.1, *) {
            GeometryReader { proxy in
                let regions = proxy.reservedRegions(
                    kind: .division, options: .includeInactive, layoutDirectionBehavior: .fixed
                )
                let active = regions.first { $0.isActive }?.frame
                let inactive = regions.first { !$0.isActive }?.frame ?? active
                let frame = proxy.frame(in: .global)
                // No fold reported here, as for a detail column beside the sidebar or on the outer
                // display: the fold read further out, moved into this view's coordinates, and
                // none at all when it misses this view.
                let fold = regions.isEmpty
                    ? inherited.seen(from: anchor, at: frame.origin, size: proxy.size)
                    : .from(active: active, inactive: inactive)
                content
                    .environment(\.fold, fold)
                    .environment(\.foldOrigin, frame.origin)
            }
        } else {
            content
        }
    }
}
