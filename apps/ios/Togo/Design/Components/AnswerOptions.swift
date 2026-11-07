import SwiftUI

/// One extra answer to a request, beyond yes and no: its id on the wire and the label the
/// agent gave it, such as "Yes, and switch to auto mode · auto mode handles these prompts for you".
struct AnswerOption: Identifiable, Equatable, Sendable {
    var id: Int
    var label: String

    /// The label's own words, before any " · " explanation.
    var title: String { AnswerOptionText.split(label).title }
    /// The explanation after " · ", when the agent gave one.
    var explanation: String? { AnswerOptionText.split(label).explanation }
}

enum AnswerOptionText {
    /// Splits "Title · why" into its title and explanation. Only the first " · " splits, so an
    /// explanation keeps any dots of its own; a label without one is all title.
    static func split(_ label: String) -> (title: String, explanation: String?) {
        let text = " " + label.trimmingCharacters(in: .whitespacesAndNewlines) + " "
        guard let range = text.range(of: " · ") else {
            return (label.trimmingCharacters(in: .whitespacesAndNewlines), nil)
        }
        let title = text[..<range.lowerBound].trimmingCharacters(in: .whitespaces)
        let rest = text[range.upperBound...].trimmingCharacters(in: .whitespaces)
        guard !title.isEmpty else { return (rest, nil) }
        return (title, rest.isEmpty ? nil : rest)
    }
}

/// The extra answers as a list: each one's full title with its explanation under it, both
/// wrapping freely. Inline under a card's buttons, or as the body of `AnswerOptionsSheet`.
struct AnswerOptionList: View {
    let options: [AnswerOption]
    let pick: (AnswerOption) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(options.enumerated()), id: \.element.id) { index, option in
                if index > 0 { Rectangle().fill(Color.togoLine).frame(height: 1) }
                Button { pick(option) } label: { AnswerOptionRow(option: option) }
                    .buttonStyle(AnswerOptionStyle())
                    .accessibilityLabel(option.title)
                    .accessibilityHint(option.explanation ?? "")
            }
        }
        .background(Color.togoDeepest, in: .rect(cornerRadius: 12, style: .continuous))
        .clipShape(.rect(cornerRadius: 12, style: .continuous))
    }
}

private struct AnswerOptionRow: View {
    let option: AnswerOption

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: option.title)
                .font(.body.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
            if let explanation = option.explanation {
                Text(verbatim: explanation)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
            }
        }
        .fixedSize(horizontal: false, vertical: true)
        .multilineTextAlignment(.leading)
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .contentShape(.rect)
    }
}

private struct AnswerOptionStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(configuration.isPressed ? Color.togoRaised : Color.clear)
            .opacity(isEnabled ? 1 : 0.5)
    }
}

/// The extra answers in a sheet, for places with no room to grow inline such as the prompt bar.
struct AnswerOptionsSheet: View {
    let question: String
    let options: [AnswerOption]
    let pick: (AnswerOption) -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    Text(verbatim: question)
                        .font(.headline)
                        .foregroundStyle(Color.togoSnow)
                        .fixedSize(horizontal: false, vertical: true)
                    AnswerOptionList(options: options) { option in
                        dismiss()
                        pick(option)
                    }
                }
                .padding(.horizontal, 20)
                .padding(.vertical, 12)
            }
            .background(Color.togoInk)
            .navigationTitle(AnswerOptionsCopy.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(AnswerOptionsCopy.cancel, systemImage: "xmark") { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }
}

enum AnswerOptionsCopy {
    static let title = String(localized: "answers.more.title", defaultValue: "More answers")
    static let cancel = String(localized: "answers.more.cancel", defaultValue: "Cancel")
    static let show = String(localized: "answers.more.show", defaultValue: "More")
    static let hide = String(localized: "answers.more.hide", defaultValue: "Fewer")
}
