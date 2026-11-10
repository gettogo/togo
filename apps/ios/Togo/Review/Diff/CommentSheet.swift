import SwiftUI

/// A comment on one line of a diff, sent to the session as "path:line: text".
struct CommentSheet: View {
    let draft: DiffModel.CommentDraft
    let actionTitle: String
    let canSend: Bool
    /// False when the comment did not reach the session: the sheet stays, words and all.
    let send: (String) -> Bool

    @Environment(\.dismiss) private var dismiss
    @State private var text = ""
    /// The last Send did not reach the session; Try again sends the same words.
    @State private var notSent = false
    @State private var failures = 0
    @FocusState private var focused: Bool

    private var trimmed: String { text.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    quote
                    TextField(DiffCopy.commentField, text: $text, axis: .vertical)
                        .lineLimit(3...8)
                        // The sheet opens to type: its field takes the keyboard once it is up.
                        .focusOnReveal($focused, revealed: canSend)
                        .padding(12)
                        .background(Color.togoRaised, in: .rect(cornerRadius: 12))
                        .disabled(!canSend)
                    Button(notSent ? DiffCopy.tryAgain : actionTitle) {
                        if send(text) {
                            dismiss()
                        } else {
                            notSent = true
                            failures += 1
                            AccessibilityNotification.Announcement(DiffCopy.commentNotSent).post()
                        }
                    }
                    .buttonStyle(.primary)
                    // Command-Return sends from the field; Return there starts a new line.
                    .keyboardShortcut(.return, modifiers: .command)
                    .disabled(!canSend || trimmed.isEmpty)
                    if notSent {
                        Text(DiffCopy.commentNotSent)
                            .font(.footnote)
                            .foregroundStyle(Color.togoSnow)
                            .fixedSize(horizontal: false, vertical: true)
                            .accessibilityIdentifier("review.comment.notSent")
                    }
                }
                .padding(Layout.margin)
            }
            .background(Color.togoInk)
            .navigationTitle(CodeCopy.comment)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    // Escape closes this sheet only, never the review under it.
                    Button(DiffCopy.cancel, systemImage: "xmark", role: .cancel) { dismiss() }
                        .keyboardShortcut(.cancelAction)
                }
            }
        }
        .tint(.primary)
        .haptic(.error, trigger: failures)
        .togoSheet(.prompt)
    }

    /// The line commented on, as it reads in the diff, with where it is.
    private var quote: some View {
        HStack(alignment: .top, spacing: 10) {
            Capsule()
                .fill(Color.togoLine)
                .frame(width: 3)
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: draft.location)
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(Color.togoMute)
                Text(verbatim: DisplayText.clean(draft.quote.trimmingCharacters(in: .whitespaces)))
                    .font(.system(.body, design: .monospaced))
                    .foregroundStyle(Color.togoSnow)
                    // trunc-ok: the quoted lines; the whole range stays visible in the diff under the sheet
                    .lineLimit(4)
            }
        }
        .fixedSize(horizontal: false, vertical: true)
        .accessibilityElement(children: .combine)
    }
}

#if DEBUG
#Preview {
    CommentSheet(
        draft: .init(row: 4, quote: "  app.get(\"/health\", health)", location: "src/router.ts:12"),
        actionTitle: DiffCopy.sendToSession, canSend: true
    ) { _ in true }
}
#endif
