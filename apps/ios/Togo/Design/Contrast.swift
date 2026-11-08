import SwiftUI

/// Increase Contrast beyond what the colour assets do. The assets already raise mute text,
/// lines and the accent (their `contrast: high` variants); this covers what is drawn with
/// opacity or hairlines, which no asset variant reaches.
enum Contrast {
    /// A border one pixel wide normally, a full point under Increase Contrast.
    static func hairline(_ displayScale: CGFloat, _ contrast: ColorSchemeContrast) -> CGFloat {
        contrast == .increased ? 1 : 1 / max(displayScale, 1)
    }

    /// A tint drawn at `base` opacity, stronger under Increase Contrast.
    static func fill(_ base: Double, _ contrast: ColorSchemeContrast) -> Double {
        contrast == .increased ? min(1, base * 2.5) : base
    }

    /// Surfaces that sit on the background with no border of their own get one under
    /// Increase Contrast, so their edge does not rely on a small shade difference.
    static func outlinesSurfaces(_ contrast: ColorSchemeContrast) -> Bool {
        contrast == .increased
    }
}
