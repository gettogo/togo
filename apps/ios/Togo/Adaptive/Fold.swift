import CoreGraphics
import SwiftUI

/// How a foldable is held. Read from the shape of the fold band, never from hinge angles.
enum Posture: Equatable, Sendable {
    case flat, book, tabletop
}

/// Where the fold is, in the coordinates of the view that read it.
struct FoldInfo: Equatable, Sendable {
    var posture: Posture
    /// The fold band while the device is bent; nil when it is flat or has no fold.
    var division: CGRect?
    /// Where the fold would be; nil on devices without one.
    var inactiveDivision: CGRect?

    static let flat = FoldInfo(posture: .flat, division: nil, inactiveDivision: nil)

    /// Active band taller than wide -> .book, wider than tall -> .tabletop, nil -> .flat.
    static func from(active: CGRect?, inactive: CGRect?) -> FoldInfo {
        guard let active else {
            return FoldInfo(posture: .flat, division: nil, inactiveDivision: inactive)
        }
        let posture: Posture = active.height > active.width ? .book : .tabletop
        return FoldInfo(posture: posture, division: active, inactiveDivision: inactive)
    }
}

extension EnvironmentValues {
    @Entry var fold: FoldInfo = .flat
}
