import SwiftUI

// Everything in the bar that uses the accent lives here: the beacon dot, the faint light a
// request brings into the bar, and the Yes capsule. Blue in the bar means an agent waits on
// the user, and nothing else in it is blue.

/// The 7 pt beacon with its faint 4 pt halo, before a request's bib. It lights once per
/// request, growing from 0.3 to full size on `dropIn`; under Reduce Motion it fades in.
struct PromptBeacon: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.loopsPaused) private var loopsPaused
    /// Whether this is the request's first showing, when the beacon lights.
    let lights: Bool
    /// Called as the beacon lights, so the owner can note that it has.
    var onLight: () -> Void = {}
    @State private var lit: Bool?
    @ScaledMetric(relativeTo: .footnote) private var side: CGFloat = 7

    var body: some View {
        let on = lit ?? !lights
        Circle()
            .fill(Color.togoAccent)
            .frame(width: side, height: side)
            .background(Circle().fill(Color.togoAccent.opacity(0.14)).padding(-4))
            .scaleEffect(on || reduceMotion ? 1 : 0.3)
            .opacity(on ? 1 : 0)
            .accessibilityHidden(true)
            .onAppear {
                guard lit == nil, lights else { return }
                onLight()
                guard !loopsPaused else {
                    lit = true
                    return
                }
                lit = false
                Task { @MainActor in
                    // motion: attention — the beacon lights once as the request arrives.
                    withAnimation(Motion.animation(.dropIn, reduce: reduceMotion)) { lit = true }
                }
            }
    }
}

/// The faint light a waiting request brings into the bar: a tint from the top-leading corner
/// at most 11 percent strong, and a 1 px border in the same blue. No outer glow.
struct PromptRequestLight: ViewModifier {
    @Environment(\.displayScale) private var displayScale
    @Environment(\.colorSchemeContrast) private var contrast
    let on: Bool
    let radius: CGFloat

    func body(content: Content) -> some View {
        content
            .background {
                EllipticalGradient(
                    // accent-ok: the prompt bar's beacon glow, on only for a request.
                    colors: [Color.togoAccent.opacity(0.11), Color.togoAccent.opacity(0)],
                    center: UnitPoint(x: 0.18, y: 0), startRadiusFraction: 0, endRadiusFraction: 0.65
                )
                .opacity(on ? 1 : 0)
                .allowsHitTesting(false)
            }
            .overlay {
                RoundedRectangle(cornerRadius: radius, style: .continuous)
                    // accent-ok: the beacon's edge, on only for a request.
                    .strokeBorder(Color.togoAccent.opacity(0.22), lineWidth: Contrast.hairline(displayScale, contrast))
                    .opacity(on ? 1 : 0)
                    .allowsHitTesting(false)
            }
    }
}

/// The bar's answer buttons: capsules, Yes filled with the beacon, the rest outlined by the
/// hairline. Light mode fills Yes with the darker text accent and white, because ink on the
/// light accent falls short of 4.5:1.
struct PromptCapsuleStyle: ButtonStyle {
    enum Kind {
        /// Yes: the beacon.
        case beacon
        /// No, Reply, Cancel: outlined.
        case plain
        /// Send: snow on ink.
        case snow
    }

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.isEnabled) private var isEnabled
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.displayScale) private var displayScale
    @Environment(\.colorSchemeContrast) private var contrast
    let kind: Kind

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(foreground)
            // trunc-ok: the bar's answer capsules carry fixed one-word labels (Yes, No, More, Reply, Send)
            .lineLimit(1)
            .padding(.horizontal, 18)
            .frame(minWidth: 44, minHeight: 44)
            .background(fill, in: .capsule)
            .overlay {
                if kind == .plain {
                    Capsule().strokeBorder(Color.togoLine, lineWidth: Contrast.hairline(displayScale, contrast))
                }
            }
            .contentShape(.capsule)
            .opacity(isEnabled ? (configuration.isPressed ? 0.85 : 1) : 0.4)
            .scaleEffect(configuration.isPressed && !reduceMotion ? 0.96 : 1)
            // motion: cause → effect — the button gives under the finger.
            .motion(.confirm, value: configuration.isPressed)
    }

    private var foreground: Color {
        switch kind {
        case .beacon: colorScheme == .dark ? Color.togoInk : Color.white
        case .plain: Color.togoSnow
        case .snow: Color.togoInk
        }
    }

    private var fill: Color {
        switch kind {
        case .beacon: colorScheme == .dark ? Color.togoAccent : Color.togoAccentText
        case .plain: Color.clear
        case .snow: Color.togoSnow
        }
    }
}

extension ButtonStyle where Self == PromptCapsuleStyle {
    static func promptCapsule(_ kind: PromptCapsuleStyle.Kind) -> Self { Self(kind: kind) }
}
