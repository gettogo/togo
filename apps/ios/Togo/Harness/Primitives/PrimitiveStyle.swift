import SwiftUI
import TogoKit

/// The few colours the native screens use beyond the surfaces: a quiet green for finished work,
/// red for a failure, amber for a meter running out. System colours, so they hold up in both
/// appearances and with Increase Contrast. The beacon is not here: needs-you lives in
/// `ApproveButton` and `PromptBeacon`.
enum PrimitiveTint {
    static let ok = Color(uiColor: .systemGreen)
    static let fail = Color(uiColor: .systemRed)
    static let warn = Color(uiColor: .systemOrange)
}

/// How a run state reads: a symbol, its colour and the word VoiceOver says.
struct PrimitiveStateLook: Equatable {
    var symbol: String
    var color: Color
    var label: String

    init(_ state: HarnessState) {
        switch state {
        case .running, .active:
            self.init(symbol: "circle.inset.filled", color: .togoSnow, label: String(localized: "primitive.state.running", defaultValue: "Running"))
        case .waiting:
            self.init(symbol: "hourglass.circle", color: .togoSnow, label: String(localized: "primitive.state.waiting", defaultValue: "Waiting"))
        case .ok, .done:
            self.init(symbol: "checkmark.circle.fill", color: PrimitiveTint.ok, label: String(localized: "primitive.state.done", defaultValue: "Done"))
        case .failed:
            self.init(symbol: "xmark.circle.fill", color: PrimitiveTint.fail, label: String(localized: "primitive.state.failed", defaultValue: "Failed"))
        case .denied:
            self.init(symbol: "hand.raised.circle", color: .togoMute, label: String(localized: "primitive.state.denied", defaultValue: "Denied"))
        case .cancelled:
            self.init(symbol: "stop.circle", color: .togoMute, label: String(localized: "primitive.state.cancelled", defaultValue: "Cancelled"))
        case .interrupted:
            self.init(symbol: "stop.circle", color: .togoMute, label: String(localized: "primitive.state.interrupted", defaultValue: "Interrupted"))
        case .idle:
            self.init(symbol: "pause.circle", color: .togoMute, label: String(localized: "primitive.state.idle", defaultValue: "Idle"))
        case .skipped:
            self.init(symbol: "minus.circle", color: .togoMuteDim, label: String(localized: "primitive.state.skipped", defaultValue: "Skipped"))
        default:
            self.init(symbol: "circle", color: .togoMuteDim, label: String(localized: "primitive.state.pending", defaultValue: "Not started"))
        }
    }

    private init(symbol: String, color: Color, label: String) {
        self.symbol = symbol
        self.color = color
        self.label = label
    }
}

/// A run state as its symbol, sized with the text beside it.
struct PrimitiveStateGlyph: View {
    let state: HarnessState
    var font: Font = .body

    var body: some View {
        let look = PrimitiveStateLook(state)
        Image(systemName: look.symbol)
            .font(font)
            .foregroundStyle(look.color)
            .accessibilityHidden(true)
    }
}

/// A small capsule label inside a row: a badge, a facet, "Opens Model". Mono for what the
/// machine says.
struct PrimitiveChip: View {
    enum Tone { case plain, strong, ok, fail }

    let text: String
    var mono = false
    var tone: Tone = .plain
    var symbol: String?

    var body: some View {
        HStack(spacing: 4) {
            if let symbol {
                Image(systemName: symbol).imageScale(.small)
            }
            Text(verbatim: text)
                .font(mono ? .system(.caption2, design: .monospaced) : .caption)
                .fixedSize(horizontal: false, vertical: true)
        }
        .foregroundStyle(foreground)
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(background, in: .capsule)
    }

    private var foreground: Color {
        switch tone {
        case .plain: .togoMute
        case .strong: .togoSnow
        case .ok: PrimitiveTint.ok
        case .fail: PrimitiveTint.fail
        }
    }

    private var background: Color {
        switch tone {
        case .plain: .togoSnow.opacity(0.06)
        case .strong: .togoSnow.opacity(0.1)
        case .ok: PrimitiveTint.ok.opacity(0.12)
        case .fail: PrimitiveTint.fail.opacity(0.12)
        }
    }
}

/// A filter or role chip that toggles: snow when on, outlined by the hairline when off.
struct PrimitiveToggleChipStyle: ButtonStyle {
    @Environment(\.displayScale) private var displayScale
    let on: Bool

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.subheadline.weight(.medium))
            .foregroundStyle(on ? Color.togoInk : Color.togoSnow)
            .padding(.horizontal, 12)
            .frame(minHeight: 34)
            .background(on ? Color.togoSnow : Color.clear, in: .capsule)
            .overlay {
                if !on { Capsule().strokeBorder(Color.togoLine, lineWidth: 1 / displayScale) }
            }
            .contentShape(.capsule)
            .frame(minHeight: 44)
            .opacity(configuration.isPressed ? 0.8 : 1)
    }
}

/// An action button by its role: primary snow, destructive outlined in red, the rest outlined.
struct PrimitiveCapsuleStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled
    @Environment(\.displayScale) private var displayScale
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let role: HarnessActionRef.Role
    var large = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font((large ? Font.body : .subheadline).weight(.semibold))
            .foregroundStyle(foreground)
            .fixedSize(horizontal: false, vertical: true)
            .multilineTextAlignment(.center)
            .padding(.horizontal, large ? 20 : 16)
            .frame(minWidth: 44, maxWidth: large ? .infinity : nil, minHeight: large ? 50 : 44)
            .background(role == .primary ? Color.togoSnow : Color.clear, in: .capsule)
            .overlay {
                if role != .primary {
                    Capsule().strokeBorder(role == .destructive ? PrimitiveTint.fail.opacity(0.4) : Color.togoLine, lineWidth: 1 / displayScale)
                }
            }
            .contentShape(.capsule)
            .opacity(isEnabled ? (configuration.isPressed ? 0.85 : 1) : 0.4)
            .scaleEffect(configuration.isPressed && !reduceMotion ? 0.97 : 1)
            .motion(.confirm, value: configuration.isPressed)
    }

    private var foreground: Color {
        switch role {
        case .primary: .togoInk
        case .destructive: PrimitiveTint.fail
        default: .togoSnow
        }
    }
}

extension View {
    /// A content card on the raised surface: one radius, no border, no shadow.
    func primitiveCard(radius: CGFloat = 20, padding: CGFloat = 16) -> some View {
        self.padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.togoRaised, in: .rect(cornerRadius: radius, style: .continuous))
    }

    /// A well on the deepest surface, for machine text: output tails, commands, diffs.
    func primitiveWell(radius: CGFloat = 12) -> some View {
        self.padding(.horizontal, 10)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.togoDeepest, in: .rect(cornerRadius: radius, style: .continuous))
    }
}

extension Font {
    /// SF Mono at a text style's size, scaling with Dynamic Type.
    static func mono(_ style: Font.TextStyle, weight: Font.Weight = .regular) -> Font {
        .system(style, design: .monospaced).weight(weight)
    }
}

/// The symbol an action gets in a bar, where every item needs one.
enum PrimitiveActionSymbol {
    static func of(_ ref: HarnessActionRef) -> String {
        let id = ref.id.lowercased()
        if ref.role == .destructive { return "trash" }
        if id.contains("compact") { return "arrow.down.right.and.arrow.up.left" }
        if id.contains("stop") || id.contains("kill") { return "stop.circle" }
        if id.contains("restart") || id.contains("relaunch") || id.contains("refresh") { return "arrow.clockwise" }
        if id.contains("open") { return "arrow.up.forward.square" }
        if id.contains("copy") { return "doc.on.doc" }
        if id.contains("resume") { return "play.circle" }
        return ref.role == .primary ? "checkmark.circle" : "bolt.circle"
    }
}

/// Durations as the machine says them: "45s", "1m 12s", "2h 05m".
enum PrimitiveElapsed {
    static func string(_ seconds: TimeInterval) -> String {
        let total = max(0, Int(seconds))
        let h = total / 3600, m = (total % 3600) / 60, s = total % 60
        if h > 0 { return String(format: "%dh %02dm", h, m) }
        if m > 0 { return String(format: "%dm %02ds", m, s) }
        return "\(s)s"
    }
}
