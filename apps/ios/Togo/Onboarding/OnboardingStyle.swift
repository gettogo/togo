import SwiftUI

/// The layout every step after the form shares: a title and one sentence at the top,
/// the step's content, and its actions at the bottom within thumb reach.
struct OnboardingStep<Content: View, Actions: View>: View {
    let title: String
    let message: String
    @ViewBuilder var content: Content
    @ViewBuilder var actions: Actions

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                VStack(alignment: .leading, spacing: 8) {
                    Text(title)
                        .font(.title2.weight(.semibold))
                        .foregroundStyle(Color.togoSnow)
                        .accessibilityAddTraits(.isHeader)
                    Text(message)
                        .font(.body)
                        .foregroundStyle(Color.togoMute)
                }
                content
            }
            .screenMargins()
            .padding(.top, 12)
            .padding(.bottom, 24)
        }
        .scrollBounceBehavior(.basedOnSize)
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(spacing: 4) { actions }
                .screenMargins()
                .padding(.top, 12)
                .padding(.bottom, 8)
                .background(Color.togoInk)
        }
        .background(Color.togoInk)
    }
}

/// A key, fingerprint or command in monospace, never cut off. Lines break at spaces; a token
/// too long for any line breaks between characters.
struct CopyableText: View {
    let text: String

    var body: some View {
        Text(verbatim: Self.breakable(text))
            .font(.system(.footnote, design: .monospaced))
            .foregroundStyle(Color.togoSnow)
            .accessibilityLabel(Text(verbatim: text))
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(14)
            .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Color.togoLine))
    }

    /// The text system hyphenates a long word it has to split, showing a "-" that is not part
    /// of the value. A zero-width space between the characters of long tokens gives it break
    /// points instead. The screens copy the original text, never this rendering.
    nonisolated static func breakable(_ text: String) -> String {
        text.split(separator: " ", omittingEmptySubsequences: false)
            .map { token in token.count > longToken ? token.map(String.init).joined(separator: "\u{200B}") : String(token) }
            .joined(separator: " ")
    }

    private nonisolated static let longToken = 16
}

/// One fact about the machine, with the symbol saying whether it is in place.
struct ReadinessLine: View {
    let text: String
    let ready: Bool

    var body: some View {
        Label {
            Text(text)
                .font(.body)
                .foregroundStyle(Color.togoSnow)
        } icon: {
            Image(systemName: ready ? "checkmark" : "minus")
                .font(.body.weight(.semibold))
                .foregroundStyle(ready ? Color.togoSnow : Color.togoMute)
        }
        .accessibilityElement(children: .combine)
    }
}
