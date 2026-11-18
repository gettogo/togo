import SwiftUI
import TogoKit

/// Optional semantic annotations from a live command. Provider marks and
/// role chips remain content; source icon names never become SF Symbols.
struct PickerSourceMetadata: View {
    let appearance: HarnessPickerItemAppearance

    var body: some View {
        WrapLayout(spacing: 5, lineSpacing: 5) {
            ForEach(Array((appearance.badges ?? []).enumerated()), id: \.offset) { _, badge in
                Text(verbatim: badge.text)
                    .font(.caption.weight(.medium))
                    .foregroundStyle(tint(badge.tone))
            }
            ForEach(Array((appearance.chips ?? []).enumerated()), id: \.offset) { _, chip in
                HStack(spacing: 3) {
                    if chip.on == true { Image(systemName: "checkmark").accessibilityHidden(true) }
                    Text(verbatim: chip.text)
                    if chip.auto == true { Image(systemName: "arrow.triangle.branch").accessibilityHidden(true) }
                }
                .font(.caption)
                .foregroundStyle(chip.on == true ? Color.togoSnow : Color.togoMute)
                .accessibilityLabel(chip.text)
                .accessibilityValue(chip.on == true ? PrimitiveCopy.selected : "")
            }
        }
    }

    private func tint(_ tone: String?) -> Color {
        switch tone {
        case "error", "danger": PrimitiveTint.fail
        case "warning": PrimitiveTint.warn
        case "success": PrimitiveTint.ok
        default: Color.togoMute
        }
    }
}
