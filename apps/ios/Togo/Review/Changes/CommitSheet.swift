import SwiftUI
import TogoKit

/// Writes a commit message and commits what is staged, or replaces the last commit. Amend starts
/// off every time the sheet opens, and replacing a commit asks once more.
struct CommitSheet: View {
    /// A first line longer than this is counted under the field.
    static let subjectLimit = 72

    @Bindable var model: ChangesModel
    @State private var confirmingAmend = false
    @FocusState private var messageFocused: Bool

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
            .confirmationDialog(ChangesCopy.amendQuestion, isPresented: $confirmingAmend, titleVisibility: .visible) {
                Button(ChangesCopy.amendButton, role: .destructive) { submit() }
                Button(ChangesCopy.cancel, role: .cancel) {}
            } message: {
                Text([model.amendReplaces, model.amendWarning].compactMap(\.self).joined(separator: " "))
            }
        }
        .togoControls()
        .interactiveDismissDisabled(model.isWorking)
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
            if model.amend {
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
            if let error = model.commitError {
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
                if model.isWorking {
                    ProgressView().tint(Color.togoInk)
                } else {
                    Text(model.amend ? ChangesCopy.amendButton : ChangesCopy.commit)
                }
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
