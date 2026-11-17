import SwiftUI

/// Only content that continues beyond the viewport dissolves at its edge. At the
/// conversation's beginning or foot the margin and the last visible line stay opaque.
struct ChatScrollEdgeLengths: Equatable {
    static let depth: CGFloat = 24
    var top: CGFloat = 0
    var bottom: CGFloat = 0

    init(offset: CGFloat, topInset: CGFloat, container: CGFloat, content: CGFloat) {
        let travelled = offset + topInset
        top = min(Self.depth, max(0, travelled))
        bottom = min(Self.depth, max(0, content - travelled - container))
    }

    init() {}
}

/// An alpha mask, independent of the canvas colour, transparency preferences or wallpaper.
/// Geometry changes affect paint only: no content inset, row, scroll anchor or offset changes.
struct ChatScrollEdgeMask: View {
    let lengths: ChatScrollEdgeLengths

    var body: some View {
        GeometryReader { geometry in
            let top = min(lengths.top, geometry.size.height / 2)
            let bottom = min(lengths.bottom, geometry.size.height / 2)
            VStack(spacing: 0) {
                if top > 0 {
                    LinearGradient(colors: [.clear, .black], startPoint: .top, endPoint: .bottom)
                        .frame(height: top)
                }
                Rectangle().fill(.black)
                if bottom > 0 {
                    LinearGradient(colors: [.black, .clear], startPoint: .top, endPoint: .bottom)
                        .frame(height: bottom)
                }
            }
        }
    }
}

struct ChatScrollEdges: ViewModifier {
    @State private var lengths = ChatScrollEdgeLengths()

    func body(content: Content) -> some View {
        content
            // The pane's viewport ends before the shared workspace bars. Use one predictable
            // transcript mask there instead of combining it with the system's bar blur.
            .scrollEdgeEffectHidden(true, for: .vertical)
            .onScrollGeometryChange(for: ChatScrollEdgeLengths.self) { geometry in
                ChatScrollEdgeLengths(offset: geometry.contentOffset.y, topInset: geometry.contentInsets.top,
                                      container: geometry.containerSize.height, content: geometry.contentSize.height)
            } action: { _, value in
                var transaction = Transaction(animation: nil)
                transaction.disablesAnimations = true
                withTransaction(transaction) { lengths = value }
            }
            .mask { ChatScrollEdgeMask(lengths: lengths) }
    }
}
