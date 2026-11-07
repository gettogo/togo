import SwiftUI
import TogoKit

/// Writes a commit message and commits what is staged, or replaces the last commit. Amend starts
/// off every time the sheet opens, and replacing a commit asks once more.
struct CommitSheet: View {
    /// A first line longer than this is counted under the field.
    static let subjectLimit = 72

    @Bindable var model: ChangesModel
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var confirmingAmend = false
    @FocusState private var messageFocused: Bool
    /// The amend footer and the commit error as drawn: each arrives in a `resize` transaction.
    @State private var amendShown = LiveMirror<Bool>()
    @State private var errorShown = LiveMirror<String?>()

    var body: some View {
        NavigationStack {
            Form {
                messageSection
                amendSection
                commitSection
            }
            .scrollContentBackground(.hidden)
            .background(Color.togoInk)
            .navigationTitle(ChangesCopy.commitTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(ChangesCopy.cancel, systemImage: "xmark", role: .cancel) { model.isCommitSheetOpen = false }
                }
            }
            .coordinated("review.amend", .dialog, isPresented: $confirmingAmend) { view, shown in
                view.confirmationDialog(ChangesCopy.amendQuestion, isPresented: shown, titleVisibility: .visible) {
                    Button(ChangesCopy.amendButton, role: .destructive) { submit() }
                    Button(ChangesCopy.cancel, role: .cancel) {}
                } message: {
                    Text([model.amendReplaces, model.amendWarning].compactMap(\.self).joined(separator: " "))
                }
            }
        }
        .togoControls()
        .interactiveDismissDisabled(model.isWorking)
        .mirrors(model.amend, into: $amendShown)
        .mirrors(model.commitError, into: $errorShown)
        .haptic(.error, trigger: model.commitError) { _, new in new != nil }
    }

    private var firstLineLength: Int {
        model.message.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: false).first?.count ?? 0
    }

    private var messageSection: some View {
        Section {
            TextField(ChangesCopy.messagePlaceholder, text: $model.message, axis: .vertical)
                .font(.body)
                .lineLimit(5...14)
                // Quotes and dashes go into git history as typed, so the keyboard's curly versions are undone.
                .onChange(of: model.message) { old, new in
                    let plain = CommitMessageText.straightened(new, previous: old)
                    if plain != new { model.message = plain }
                }
                .focused($messageFocused)
                .accessibilityLabel(ChangesCopy.messageLabel)
                .listRowBackground(Color.togoGrouped)
        } footer: {
            HStack(alignment: .firstTextBaseline) {
                Text(ChangesCopy.stagedCount(model.stagedCount))
                Spacer(minLength: 8)
                if firstLineLength > Self.subjectLimit {
                    Text(ChangesCopy.firstLineLength(firstLineLength))
                        .monospacedDigit()
                }
            }
            .font(.footnote)
            .foregroundStyle(Color.togoMute)
        }
    }

    private var amendSection: some View {
        Section {
            Toggle(ChangesCopy.amend, isOn: $model.amend)
                .foregroundStyle(Color.togoSnow)
                .disabled(model.lastCommit == nil)
                .listRowBackground(Color.togoGrouped)
        } footer: {
            if amendShown.read(model.amend) {
                VStack(alignment: .leading, spacing: 6) {
                    if let replaces = model.amendReplaces {
                        Text(replaces)
                            .foregroundStyle(Color.togoMute)
                    }
                    if let warning = model.amendWarning {
                        Label(warning, systemImage: "exclamationmark.triangle")
                            .foregroundStyle(Color.togoSnow)
                    }
                }
                .font(.footnote)
                // motion: cause → effect — turning on Amend opens what it replaces under the toggle
                .transition(Motion.arrive(reduce: reduceMotion))
            }
        }
    }

    private var commitSection: some View {
        Section {
            if let ended = model.ended {
                Label(ReviewEntryCopy.ended(ended), systemImage: "lock")
                    .font(.subheadline)
                    .foregroundStyle(Color.togoSnow)
                    .listRowBackground(Color.togoGrouped)
            }
            if let error = errorShown.read(model.commitError) {
                Label(error, systemImage: "exclamationmark.triangle")
                    .font(.subheadline)
                    .foregroundStyle(Color.togoSnow)
                    .listRowBackground(Color.togoGrouped)
            }
            Button {
                if model.amend {
                    confirmingAmend = true
                } else {
                    submit()
                }
            } label: {
                // One slot: the spinner and the words crossfade, and the button keeps its size.
                ZStack {
                    // motion: cause → effect — the tapped button now shows the commit running
                    if model.isWorking {
                        ProgressView().tint(Color.togoInk)
                            .transition(.opacity)
                    } else {
                        Text(model.amend ? ChangesCopy.amendButton : ChangesCopy.commit)
                            .contentTransition(.interpolate)
                            // motion: cause → effect — the label swaps back in the same place
                            .transition(.opacity)
                    }
                }
                // motion: cause → effect — the tap turns the label into the spinner in place
                .motion(.respond, value: model.isWorking)
                // motion: cause → effect — the Amend toggle renames the button in place
                .motion(.respond, value: model.amend)
            }
            .buttonStyle(.primary)
            .disabled(!model.canCommit)
            .listRowBackground(Color.clear)
            .listRowInsets(EdgeInsets())
        } footer: {
            Text(ChangesCopy.footnote)
                .font(.footnote)
                .foregroundStyle(Color.togoMute)
                .frame(maxWidth: .infinity)
                .multilineTextAlignment(.center)
                .padding(.top, 8)
        }
    }

    private func submit() {
        messageFocused = false
        Task { _ = await model.commit(message: model.message, amend: model.amend) }
    }
}
