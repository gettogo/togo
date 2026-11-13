import SwiftUI

/// The status block has at most four cells. Measure one adaptive row at a
/// time so wrapped values share a top edge and a full-height background.
struct StatusCellsLayout: SwiftUI.Layout {
    var minimum: CGFloat = 104
    private let spacing: CGFloat = 1

    private struct Rows {
        var columns: Int
        var width: CGFloat
        var heights: [CGFloat]
    }

    private func rows(width: CGFloat, subviews: Subviews) -> Rows {
        let columns = min(subviews.count, max(1, Int((width + spacing) / (minimum + spacing))))
        guard columns > 0 else { return Rows(columns: 0, width: 0, heights: []) }
        let cellWidth = max(0, (width - CGFloat(columns - 1) * spacing) / CGFloat(columns))
        var heights = Array(repeating: CGFloat.zero, count: (subviews.count + columns - 1) / columns)
        for (index, view) in subviews.enumerated() {
            let height = view.sizeThatFits(ProposedViewSize(width: cellWidth, height: nil)).height
            heights[index / columns] = max(heights[index / columns], height)
        }
        return Rows(columns: columns, width: cellWidth, heights: heights)
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard !subviews.isEmpty else { return .zero }
        let ideal = subviews.map { $0.sizeThatFits(.unspecified).width }.max() ?? minimum
        let width = proposal.width.flatMap { $0.isFinite ? max(0, $0) : nil } ?? ideal
        let rows = rows(width: width, subviews: subviews)
        return CGSize(width: width, height: rows.heights.reduce(0, +) + CGFloat(max(0, rows.heights.count - 1)) * spacing)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let rows = rows(width: bounds.width, subviews: subviews)
        guard rows.columns > 0 else { return }
        var y = bounds.minY
        for (index, view) in subviews.enumerated() {
            let column = index % rows.columns
            let row = index / rows.columns
            if column == 0, row > 0 { y += rows.heights[row - 1] + spacing }
            // SwiftUI mirrors custom layout placement in RTL environments.
            let x = bounds.minX + CGFloat(column) * (rows.width + spacing)
            view.place(at: CGPoint(x: x, y: y), anchor: .topLeading,
                       proposal: ProposedViewSize(width: rows.width, height: rows.heights[row]))
        }
    }
}
