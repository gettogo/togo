import SwiftUI
import TogoKit

/// One item in full: its name, selector and facts, Select, and a button per role. Pushed on the
/// list's stack on a compact width; the detail column of the split view on a regular one.
/// Never a sheet over the list's sheet (`C8`).
struct PickerItemDetail: View {
    let item: PickerModel.Item
    let picker: HarnessScreen.Picker
    /// The list's runner, so a role's outcome shows in the list's toast.
    let actions: PrimitiveActions
    let context: PrimitiveContext
    /// Selects the item through the list's own rule (`ActionGate`, close on success).
    let onSelect: () -> Void
    let onAssign: (HarnessActionRef) -> Void

    var body: some View {
        List {
            Section {
                VStack(alignment: .leading, spacing: 8) {
                    Text(verbatim: item.title)
                        .font(.title3.weight(.semibold))
                        .foregroundStyle(Color.togoSnow)
                    Text(verbatim: item.id)
                        .font(.mono(.footnote))
                        .foregroundStyle(Color.togoMute)
                        .textSelection(.enabled)
                    let shown = chips
                    if !shown.isEmpty {
                        WrapLayout(spacing: 6, lineSpacing: 6) {
                            ForEach(shown, id: \.self) { PrimitiveChip(text: $0, mono: true) }
                            if item.id == picker.selected {
                                PrimitiveChip(text: PrimitiveCopy.selected, tone: .strong, symbol: "checkmark")
                            }
                        }
                    }
                }
                .padding(.vertical, 4)
                .listRowBackground(Color.clear)
            }
            if item.id != picker.selected, item.disabled != true {
                Section {
                    Button(action: onSelect) {
                        HStack {
                            Label(picker.onSelectLabel ?? PrimitiveCopy.select, systemImage: "checkmark.circle")
                            Spacer(minLength: 8)
                            if actions.isRunning(picker.onSelect, target: item.id) {
                                ProgressView().controlSize(.small)
                            }
                        }
                        .frame(minHeight: 44)
                        .contentShape(.rect)
                    }
                    .foregroundStyle(Color.togoSnow)
                    .groupedRows()
                }
            }
            let facts = PickerModel.facts(of: item, picker: picker)
            if !facts.isEmpty || item.subtitle != nil {
                Section(PrimitiveCopy.details) {
                    if let subtitle = item.subtitle, !subtitle.isEmpty {
                        Text(verbatim: subtitle)
                            .font(.mono(.subheadline))
                            .foregroundStyle(Color.togoSnow)
                            .groupedRows()
                    }
                    ForEach(facts, id: \.key) { fact in
                        LabeledContent {
                            Text(verbatim: fact.value)
                                .font(.mono(.subheadline))
                                .foregroundStyle(Color.togoSnow)
                                .multilineTextAlignment(.trailing)
                        } label: {
                            Text(verbatim: fact.key).foregroundStyle(Color.togoMute)
                        }
                        .groupedRows()
                    }
                }
            }
            roles
            Section {
                Button(PrimitiveCopy.copyID, systemImage: "doc.on.doc") {
                    UIPasteboard.general.string = item.id
                    actions.show(PrimitiveFeedback(kind: .done, title: PrimitiveCopy.copied, detail: item.id))
                }
                Button(PrimitiveCopy.openTerminal, systemImage: "terminal") { context.showTerminal() }
            }
            .foregroundStyle(Color.togoSnow)
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(item.title)
        .navigationBarTitleDisplayMode(.inline)
    }

    private var chips: [String] {
        var chips = PickerModel.chips(of: item, in: picker)
        if let group = item.group, !chips.contains(group) { chips.insert(group, at: 0) }
        return chips
    }

    @ViewBuilder private var roles: some View {
        let refs = picker.itemActions ?? []
        if !refs.isEmpty {
            Section(PrimitiveCopy.useAs) {
                ForEach(refs) { ref in
                    let holds = PickerModel.isTrue(item.facets?[ref.id])
                    Button {
                        if !holds { onAssign(ref) }
                    } label: {
                        HStack {
                            Text(verbatim: ref.label).foregroundStyle(Color.togoSnow)
                            Spacer(minLength: 8)
                            ZStack {
                                if actions.isRunning(ref.id, target: item.id) {
                                    ProgressView().controlSize(.small)
                                } else if holds {
                                    Image(systemName: "checkmark").font(.body.weight(.semibold))
                                }
                            }
                            .slot(width: 20, key: actions.isRunning(ref.id, target: item.id) || holds)
                        }
                        .frame(minHeight: 44)
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .groupedRows()
                    .accessibilityAddTraits(holds ? .isSelected : [])
                }
            }
        }
    }
}
