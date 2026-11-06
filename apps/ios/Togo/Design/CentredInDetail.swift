import SwiftUI

extension View {
    /// Centres a state (empty, nothing to show) in the column without its trailing safe area: a
    /// vertical system bar on that side would otherwise push it off centre. The leading inset
    /// stays, since beside a floating sidebar it is the sidebar's width. In book
    /// posture, where the fold runs through the column, it centres on the trailing page so it
    /// never sits on the fold. The content keeps itself narrow enough to clear the bars.
    func centredInDetail() -> some View {
        modifier(CentredInDetail())
    }
}

private struct CentredInDetail: ViewModifier {
    func body(content: Content) -> some View {
        content
            .modifier(TrailingPage())
            .readsFold()
            .ignoresSafeArea(.container, edges: .trailing)
    }
}

private struct TrailingPage: ViewModifier {
    @Environment(\.fold) private var fold

    func body(content: Content) -> some View {
        GeometryReader { proxy in
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .padding(.leading, Self.leadingInset(width: proxy.size.width, fold: fold))
        }
    }

    /// How far the trailing page starts in, or zero when no fold runs through this view.
    static func leadingInset(width: CGFloat, fold: FoldInfo) -> CGFloat {
        guard fold.posture == .book, let division = fold.division,
              division.minX > 0, division.maxX < width else { return 0 }
        return division.maxX
    }
}
