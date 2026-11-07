import SwiftUI
import TogoKit

/// The picker's "Not set up" section: what the agent could use once it is set up, such as a
/// provider not signed in, each with the action that sets it up. It folds like a group and
/// starts shut; a search opens it on its matches. A setup that asks for a value (an API key)
/// opens its field under the row, focused, so one tap is enough to type.
struct PickerSetupSection: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let items: [PickerModel.Item]
    let total: Int
    let action: HarnessActionRef
    /// The action asks for a secret value first.
    let asksSecret: Bool
    let expanded: Bool
    let searching: Bool
    let actions: PrimitiveActions
    let toggle: () -> Void
    /// Runs the action for an entry, with its value when it asks for one.
    let run: (PickerModel.Item, String?) -> Void

    @State private var entries = RevealedFields<String>()
    @State private var values: [String: String] = [:]
    @FocusState private var typing: String?

    var body: some View {
        Section {
            if expanded {
                ForEach(items) { item in row(item) }
            }
        } header: {
            header
        } footer: {
            if expanded, asksSecret {
                Text(PrimitiveCopy.secretFooter).font(.footnote).foregroundStyle(Color.togoMute)
            }
        }
        .onChange(of: typing) { _, now in entries.focusMoved(to: now) }
    }

    private var header: some View {
        Button {
            guard !searching else { return }
            // motion: cause → effect — the section folds where it was tapped; rows below glide.
            withAnimation(Motion.animation(.resize, reduce: reduceMotion), toggle)
        } label: {
            HStack(spacing: 8) {
                Text(PrimitiveCopy.notSetUp)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                Text(verbatim: "\(searching ? items.count : total)")
                    .font(.mono(.caption))
                    .foregroundStyle(Color.togoMute)
                Spacer(minLength: 8)
                if !searching {
                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(Color.togoMute)
                        .rotationEffect(.degrees(expanded && !reduceMotion ? 90 : 0))
                }
            }
            .frame(minHeight: 32)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .textCase(nil)
        .accessibilityAddTraits(.isHeader)
        .accessibilityValue(searching ? "" : expanded
            ? String(localized: "primitive.expanded", defaultValue: "Expanded")
            : String(localized: "primitive.collapsed", defaultValue: "Collapsed"))
    }

    private func row(_ item: PickerModel.Item) -> some View {
        let running = actions.isRunning(action.id, target: item.id)
        return VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Text(verbatim: item.title)
                    .font(.body)
                    .foregroundStyle(Color.togoSnow)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                ZStack(alignment: .trailing) {
                    if running {
                        ProgressView().controlSize(.small)
                    } else if !entries.isOpen(item.id) {
                        Button(action.label) { start(item) }
                            .buttonStyle(PrimitiveCapsuleStyle(role: .normal))
                    }
                }
                // motion: cause → effect — the row's button turns into a spinner in its fixed slot.
                .motion(.respond, value: running)
            }
            .frame(minHeight: 44)
            if entries.isOpen(item.id) {
                entry(item)
                    // motion: cause → effect — the key field unfolds in place under its row.
                    .transition(Motion.unfold(reduce: reduceMotion))
            }
        }
        .groupedRows()
        .accessibilityElement(children: .contain)
    }

    private func entry(_ item: PickerModel.Item) -> some View {
        let value = values[item.id] ?? ""
        return HStack(spacing: 8) {
            SecureField(PrimitiveCopy.apiKey, text: Binding(get: { value }, set: { values[item.id] = $0 }),
                        prompt: Text(PrimitiveCopy.apiKey).foregroundStyle(Color.togoMute))
                .font(.mono(.body))
                .textContentType(.password)
                .focusOnReveal($typing, equals: item.id, revealed: entries.focus == item.id)
                .submitLabel(.done)
                .onSubmit { save(item) }
            Button(PrimitiveCopy.cancel) { close(item) }
                .buttonStyle(PrimitiveCapsuleStyle(role: .normal))
            Button(PrimitiveCopy.save) { save(item) }
                .buttonStyle(PrimitiveCapsuleStyle(role: .primary))
                .disabled(value.isEmpty)
        }
    }

    private func start(_ item: PickerModel.Item) {
        guard asksSecret else {
            run(item, nil)
            return
        }
        // motion: cause → effect — the field opens under the row that asked for it.
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { entries.reveal(item.id) }
    }

    private func save(_ item: PickerModel.Item) {
        guard let value = values[item.id], !value.isEmpty else { return }
        run(item, value)
        close(item)
    }

    private func close(_ item: PickerModel.Item) {
        values[item.id] = nil
        // motion: preventing a jump — the field's room closes after it conceals.
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { entries.hide(item.id) }
    }
}
