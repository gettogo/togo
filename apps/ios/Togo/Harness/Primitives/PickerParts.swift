import SwiftUI
import TogoKit

/// One item: a check when selected, the title, the machine's id or detail in mono, a badge,
/// and the roles it holds on the trailing edge.
struct PickerRow: View {
    let item: PickerModel.Item
    let selected: Bool
    let held: [String]
    let running: Bool
    let picking: Bool
    let onTap: () -> Void
    @ScaledMetric(relativeTo: .body) private var checkWidth: CGFloat = 20

    var body: some View {
        Button(action: onTap) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Image(systemName: "checkmark")
                    .font(.body.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                    .opacity(selected ? 1 : 0)
                    .frame(width: checkWidth)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: item.title)
                        .font(.body.weight(.semibold))
                        .foregroundStyle(item.disabled == true ? Color.togoMute : Color.togoSnow)
                    if let subtitle = item.subtitle, !subtitle.isEmpty {
                        Text(verbatim: subtitle)
                            .font(.mono(.caption))
                            .foregroundStyle(Color.togoMute)
                    }
                    if let badge = item.badge, !badge.isEmpty {
                        PrimitiveChip(text: badge, mono: true)
                            .padding(.top, 3)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if running {
                    ProgressView().controlSize(.small)
                } else if !held.isEmpty {
                    Text(verbatim: held.count == 1 ? held[0] : PrimitiveCopy.holds(held.count))
                        .font(.footnote)
                        .foregroundStyle(Color.togoMute)
                }
            }
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .disabled(item.disabled == true)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([item.title, item.subtitle, item.badge].compactMap { $0 }.joined(separator: ", "))
        .accessibilityValue(value)
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
        .accessibilityHint(picking ? PrimitiveCopy.select : "")
    }

    private var value: String {
        var parts: [String] = []
        if selected { parts.append(PrimitiveCopy.selected) }
        parts.append(contentsOf: held)
        if running { parts.append(PrimitiveCopy.running) }
        return parts.joined(separator: ", ")
    }
}

/// A role and what holds it now, as in "default  Opus 4.1". Tap to choose a new holder.
struct PickerRoleChip: View {
    @Environment(\.displayScale) private var displayScale
    let label: String
    let holder: String?
    let on: Bool
    let onTap: () -> Void

    var body: some View {
        Button(action: onTap) {
            VStack(alignment: .leading, spacing: 1) {
                Text(verbatim: label)
                    .font(.caption)
                    .foregroundStyle(Color.togoMute)
                Text(verbatim: holder ?? PrimitiveCopy.notSet)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(holder == nil ? Color.togoMute : Color.togoSnow)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .frame(minWidth: 96, maxWidth: 190, minHeight: 44, alignment: .leading)
            .background(Color.togoRaised, in: .rect(cornerRadius: 14, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .strokeBorder(Color.togoSnow, lineWidth: on ? 1.5 : 0)
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(label), \(holder ?? PrimitiveCopy.notSet)")
        .accessibilityHint(PrimitiveCopy.choosing(label))
        .accessibilityAddTraits(on ? [.isButton, .isSelected] : .isButton)
    }
}

/// The groups beside the list in a regular window: every item, then each group with its count.
struct PickerSidebar: View {
    let names: [(name: String, count: Int)]
    let total: Int
    @Binding var selection: String?

    var body: some View {
        List {
            row(nil, title: PrimitiveCopy.all, count: total)
            Section {
                ForEach(names, id: \.name) { entry in
                    row(entry.name, title: entry.name, count: entry.count)
                }
            }
        }
        .listStyle(.sidebar)
        .scrollContentBackground(.hidden)
        .background(Color.togoInk)
    }

    private func row(_ value: String?, title: String, count: Int) -> some View {
        Button { selection = value } label: {
            HStack {
                Text(verbatim: title)
                    .font(value == nil ? .body.weight(.semibold) : .mono(.subheadline))
                    .foregroundStyle(Color.togoSnow)
                Spacer(minLength: 8)
                Text(verbatim: "\(count)")
                    .font(.mono(.caption))
                    .foregroundStyle(Color.togoMute)
            }
            .frame(minHeight: 36)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .listRowBackground(selection == value ? Color.togoGrouped : Color.clear)
        .accessibilityAddTraits(selection == value ? [.isButton, .isSelected] : .isButton)
    }
}

/// An item's card: what the adapter knows about it, its roles, and Select.
struct PickerItemCard: View {
    @Environment(\.dismiss) private var dismiss
    let item: PickerModel.Item
    let picker: HarnessScreen.Picker
    /// The list's runner, which shows the toast once the card closes.
    let parent: PrimitiveActions
    let context: PrimitiveContext
    @State private var actions = PrimitiveActions()

    private let columns = [GridItem(.adaptive(minimum: 120), spacing: 1)]

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    header
                    let facts = PickerModel.facts(of: item, picker: picker)
                    if !facts.isEmpty {
                        LazyVGrid(columns: columns, spacing: 1) {
                            ForEach(facts, id: \.key) { fact in
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(verbatim: fact.key).font(.caption).foregroundStyle(Color.togoMute)
                                    Text(verbatim: fact.value).font(.mono(.subheadline)).foregroundStyle(Color.togoSnow)
                                }
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(.horizontal, 11)
                                .padding(.vertical, 9)
                                .background(Color.togoGrouped)
                                .accessibilityElement(children: .combine)
                            }
                        }
                        .clipShape(.rect(cornerRadius: 14, style: .continuous))
                    }
                    roles
                    HStack(spacing: 8) {
                        Button(PrimitiveCopy.copyID, systemImage: "doc.on.doc") {
                            UIPasteboard.general.string = item.id
                            actions.show(PrimitiveFeedback(kind: .done, title: PrimitiveCopy.copied, detail: item.id))
                        }
                        Button(PrimitiveCopy.openTerminal, systemImage: "terminal") {
                            dismiss()
                            context.showTerminal()
                        }
                    }
                    .buttonStyle(PrimitiveCapsuleStyle(role: .normal))
                }
                .padding(Layout.margin)
            }
            .background(Color.togoRaised)
            .navigationTitle(item.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(PrimitiveCopy.done, systemImage: "checkmark", role: .confirm) { dismiss() }
                }
            }
            .safeAreaInset(edge: .bottom) {
                if item.id != picker.selected, item.disabled != true {
                    Button {
                        run(picker.onSelect)
                    } label: {
                        HStack(spacing: 8) {
                            if actions.isRunning(picker.onSelect, target: item.id) { ProgressView().controlSize(.small) }
                            Text(PrimitiveCopy.select)
                        }
                    }
                    .buttonStyle(PrimitiveCapsuleStyle(role: .primary, large: true))
                    .padding(.horizontal, Layout.margin)
                    .padding(.bottom, 8)
                }
            }
            .primitiveActions(actions, context: context)
        }
        .presentationDetents([.medium, .large])
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let subtitle = item.subtitle {
                Text(verbatim: subtitle)
                    .font(.mono(.footnote))
                    .foregroundStyle(Color.togoMute)
                    .textSelection(.enabled)
            }
            HStack(spacing: 6) {
                if let badge = item.badge { PrimitiveChip(text: badge, mono: true) }
                if let group = item.group { PrimitiveChip(text: group, mono: true) }
                if item.id == picker.selected { PrimitiveChip(text: PrimitiveCopy.selected, tone: .strong, symbol: "checkmark") }
            }
        }
    }

    @ViewBuilder private var roles: some View {
        let refs = picker.itemActions ?? []
        if !refs.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                Text(PrimitiveCopy.useAs).font(.subheadline).foregroundStyle(Color.togoMute)
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 100), spacing: 6)], spacing: 6) {
                    ForEach(refs) { ref in
                        let holds = PickerModel.isTrue(item.facets?[ref.id])
                        Button {
                            if !holds { run(ref.id, ref: ref) }
                        } label: {
                            HStack {
                                Text(verbatim: ref.label).fixedSize(horizontal: false, vertical: true)
                                Spacer(minLength: 4)
                                if actions.isRunning(ref.id, target: item.id) {
                                    ProgressView().controlSize(.mini)
                                } else if holds {
                                    Image(systemName: "checkmark").font(.caption.weight(.bold))
                                }
                            }
                            .font(.subheadline.weight(.medium))
                            .foregroundStyle(holds ? Color.togoInk : Color.togoSnow)
                            .padding(.horizontal, 10)
                            .frame(minHeight: 44)
                            .background(holds ? Color.togoSnow : Color.togoGrouped, in: .rect(cornerRadius: 12, style: .continuous))
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .accessibilityAddTraits(holds ? .isSelected : [])
                    }
                }
            }
        }
    }

    /// Runs on the card, then closes it and leaves the toast to the list, unless it failed.
    private func run(_ id: String, ref: HarnessActionRef? = nil) {
        Task {
            let before = actions.feedback?.id
            defer {
                if let feedback = actions.feedback, feedback.id != before, !feedback.isFailure {
                    parent.show(feedback)
                    dismiss()
                }
            }
            if let ref {
                await actions.trigger(ref, target: item.id, args: PickerModel.args(for: item), context: context)
            } else {
                await actions.perform(id, target: item.id, args: PickerModel.args(for: item), context: context)
            }
        }
    }
}
