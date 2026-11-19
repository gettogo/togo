import SwiftUI
import UIKit
@testable import Togo

/// The window a screen is drawn in. References are only valid when recorded on an iPhone 17
/// simulator; the other sizes are windows of that size with their own safe area drawn in.
enum ScreenSize: String, CaseIterable, Sendable {
    case iPhone17, duoFolded, duoOpen, duoTabletop, duoBook, iPadPro13, iPadNarrow
    /// iPad Pro 13-inch held landscape, full screen and at half the width beside another app.
    case iPadPro13Landscape, iPadHalf

    var size: CGSize {
        switch self {
        case .iPhone17: CGSize(width: 402, height: 874)
        case .duoFolded: CGSize(width: 466, height: 678)
        case .duoOpen, .duoBook: CGSize(width: 951, height: 669)
        // Tabletop holds the inner display in portrait, folded across the middle.
        case .duoTabletop: CGSize(width: 669, height: 951)
        case .iPadPro13: CGSize(width: 1032, height: 1376)
        case .iPadNarrow: CGSize(width: 375, height: 1000)
        case .iPadPro13Landscape: CGSize(width: 1376, height: 1032)
        case .iPadHalf: CGSize(width: 678, height: 1032)
        }
    }

    var safeArea: UIEdgeInsets {
        switch self {
        case .iPhone17: UIEdgeInsets(top: 62, left: 0, bottom: 34, right: 0)
        // The folded Duo keeps a system bar on the trailing edge in portrait.
        case .duoFolded: UIEdgeInsets(top: 0, left: 0, bottom: 34, right: 84)
        case .duoOpen, .duoTabletop, .duoBook, .iPadPro13, .iPadNarrow, .iPadPro13Landscape, .iPadHalf:
            UIEdgeInsets(top: 24, left: 0, bottom: 20, right: 0)
        }
    }

    var sizeClass: UserInterfaceSizeClass {
        // The widths these layouts report regular at; the split view collapses below.
        size.width >= 700 ? .regular : .compact
    }

    /// The fold this size is held with when a test passes none.
    var fold: FoldInfo {
        switch self {
        case .duoBook:
            let band = CGRect(x: 463, y: 0, width: 25, height: 669)
            return .from(active: band, inactive: band)
        case .duoTabletop:
            let band = CGRect(x: 0, y: 451, width: 669, height: 25)
            return .from(active: band, inactive: band)
        case .duoOpen:
            return .from(active: nil, inactive: CGRect(x: 463, y: 0, width: 25, height: 669))
        default:
            return .flat
        }
    }

    /// A fold given in window coordinates, as `fold` and the tests give it, moved into the safe
    /// area's coordinates, where the device reports its fold regions.
    func inSafeArea(_ fold: FoldInfo) -> FoldInfo {
        self == .iPhone17 ? fold : fold.moved(by: CGPoint(x: -safeArea.left, y: -safeArea.top))
    }

    /// Appended to reference names; empty for the iPhone so existing references keep their names.
    var suffix: String { self == .iPhone17 ? "" : "-\(rawValue)" }
}
