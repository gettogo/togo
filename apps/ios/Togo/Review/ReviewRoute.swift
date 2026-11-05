import Foundation
import SwiftUI
import TogoKit

/// A screen pushed inside the review sheet.
enum ReviewRoute: Hashable, Sendable {
    case diff(DiffSource)
    case file(path: String, ref: String?, base: ReviewPathBase = .session)
    case folder(path: String, ref: String?, base: ReviewPathBase = .session)
    case commit(GitCommit)
}

/// What a file or folder path is relative to.
enum ReviewPathBase: Hashable, Sendable {
    /// The session's folder, which the file browser starts from.
    case session
    /// The root of the repository holding the session's folder: how git status, diffs and
    /// commits name files, also when the session runs in a subfolder.
    case repository
}

/// What a diff compares. Paths are relative to the repository root, as git status gives them.
enum DiffSource: Hashable, Sendable {
    case worktree(path: String)
    case staged(path: String)
    case untracked(path: String)
    case commit(hash: String, path: String?)
}

extension EnvironmentValues {
    /// The diff or file open beside a split view's sidebar, so the row that opened it stays marked.
    @Entry var openReviewRoute: ReviewRoute?
}

extension View {
    /// A grouped list row that opens `route`, raised and read as selected while that route is open
    /// in the detail column.
    func reviewRow(opening route: ReviewRoute) -> some View {
        modifier(ReviewRowStyle(route: route))
    }
}

private struct ReviewRowStyle: ViewModifier {
    @Environment(\.openReviewRoute) private var open
    let route: ReviewRoute

    func body(content: Content) -> some View {
        let selected = open == route
        content
            .listRowBackground(selected ? Color.reviewSelection : Color.togoGrouped)
            .listRowSeparatorTint(Color.togoLine)
            .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

extension Color {
    /// A row marked as open: a shade deeper than the grouped rows in both appearances, so muted
    /// text on it keeps 4.5:1. Neutral, since the accent only ever means "needs you".
    static let reviewSelection = Color(uiColor: UIColor { traits in
        traits.userInterfaceStyle == .dark
            ? .togoInk
            : UIColor.togoGrouped.resolvedColor(with: traits)
                .blended(with: UIColor.togoSnow.resolvedColor(with: traits), amount: 0.07)
    })
}

private extension UIColor {
    func blended(with other: UIColor, amount: CGFloat) -> UIColor {
        var (r1, g1, b1, a1): (CGFloat, CGFloat, CGFloat, CGFloat) = (0, 0, 0, 0)
        var (r2, g2, b2, a2): (CGFloat, CGFloat, CGFloat, CGFloat) = (0, 0, 0, 0)
        getRed(&r1, green: &g1, blue: &b1, alpha: &a1)
        other.getRed(&r2, green: &g2, blue: &b2, alpha: &a2)
        func mix(_ x: CGFloat, _ y: CGFloat) -> CGFloat { x + (y - x) * amount }
        return UIColor(red: mix(r1, r2), green: mix(g1, g2), blue: mix(b1, b2), alpha: mix(a1, a2))
    }
}
