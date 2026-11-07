import SwiftUI
import TogoKit

/// One item: a check when selected, the title, the selector in mono, the facts as chips, and
/// the roles it holds on the trailing edge. Everything wraps; nothing is cut short.
struct PickerRowLabel: View {
    let item: PickerModel.Item
    let picker: HarnessScreen.Picker
    let selected: Bool
    let held: [String]
    let running: Bool
    @ScaledMetric(relativeTo: .body) private var checkWidth: CGFloat = 20

    var body: some View {
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
                if let selector = PickerModel.selector(of: item, in: picker) {
                    Text(verbatim: selector)
                        .font(.mono(.caption))
                        .foregroundStyle(Color.togoMute)
                }
                if let line = PickerModel.subtitleLine(of: item, in: picker) {
                    Text(verbatim: line)
                        .font(.mono(.caption))
                        .foregroundStyle(Color.togoMute)
                }
                let chips = PickerModel.chips(of: item, in: picker)
                if !chips.isEmpty {
                    WrapLayout(spacing: 4, lineSpacing: 4) {
                        ForEach(chips, id: \.self) { PrimitiveChip(text: $0, mono: true) }
                    }
                    .padding(.top, 3)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            // The trailing accessory crossfades in place (spinner ↔ what the item holds).
            ZStack(alignment: .trailing) {
                if running {
                    ProgressView().controlSize(.small)
                } else if !held.isEmpty {
                    Text(verbatim: held.count == 1 ? held[0] : PrimitiveCopy.holds(held.count))
                        .font(.footnote)
                        .foregroundStyle(Color.togoMute)
                        .multilineTextAlignment(.trailing)
                }
            }
            // motion: cause → effect — the tapped item's accessory turns into a spinner in its fixed slot.
            .motion(.respond, value: running)
        }
        .frame(minHeight: 44)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(label)
        .accessibilityValue(value)
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
    }

    private var label: String {
        ([item.title, PickerModel.selector(of: item, in: picker), PickerModel.subtitleLine(of: item, in: picker)]
            + PickerModel.chips(of: item, in: picker)).compactMap { $0 }.joined(separator: ", ")
    }

    private var value: String {
        var parts: [String] = []
        if selected { parts.append(PrimitiveCopy.selected) }
        parts.append(contentsOf: held)
        if running { parts.append(PrimitiveCopy.running) }
        return parts.joined(separator: ", ")
    }
}

/// The roles summary at the head of the list: every role and what holds it now, one row each,
/// wrapping at any text size. Tapping a role waits for the item to give it; tapping it again
/// stops waiting.
struct PickerRolesSection: View {
    let refs: [HarnessActionRef]
    let items: [PickerModel.Item]
    let picking: String?
    let toggle: (String) -> Void

    var body: some View {
        Section {
            ForEach(refs) { ref in
                let holder = PickerModel.holders(of: ref.id, in: items).first?.title
                let on = picking == ref.id
                Button { toggle(ref.id) } label: {
                    ViewThatFits(in: .horizontal) {
                        HStack(alignment: .firstTextBaseline, spacing: 12) {
                            role(ref, on: on)
                            Spacer(minLength: 8)
                            holderText(holder, on: on).multilineTextAlignment(.trailing)
                        }
                        VStack(alignment: .leading, spacing: 2) {
                            role(ref, on: on)
                            holderText(holder, on: on)
                        }
                    }
                    .frame(minHeight: 44)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .groupedRows()
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("\(ref.label), \(holder ?? PrimitiveCopy.notSet)")
                .accessibilityHint(on ? PrimitiveCopy.cancel : PrimitiveCopy.choosing(ref.label))
                .accessibilityAddTraits(on ? [.isButton, .isSelected] : .isButton)
            }
        } header: {
            Text(PrimitiveCopy.roles).textCase(nil)
        } footer: {
            Text(picking.flatMap { id in refs.first { $0.id == id } }.map { PrimitiveCopy.choosing($0.label) }
                ?? PrimitiveCopy.rolesFooter)
        }
    }

    private func role(_ ref: HarnessActionRef, on: Bool) -> some View {
        Text(verbatim: ref.label)
            .font(.subheadline)
            .foregroundStyle(on ? Color.togoSnow : Color.togoMute)
    }

    @ViewBuilder private func holderText(_ holder: String?, on: Bool) -> some View {
        if on {
            Text(PrimitiveCopy.cancel)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
        } else {
            Text(verbatim: holder ?? PrimitiveCopy.notSet)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(holder == nil ? Color.togoMute : Color.togoSnow)
        }
    }
}

/// The groups beside the list in a split view: every item, then each group with its count,
/// the selection's group first. The chosen row stays highlighted (`C14`).
struct PickerSidebar: View {
    static let allTag = "\u{1}all"

    let names: [(name: String, count: Int)]
    let total: Int
    @Binding var selection: String?

    var body: some View {
        List(selection: Binding<String?>(
            get: { selection ?? Self.allTag },
            set: { tag in
                guard let tag else { return }
                selection = tag == Self.allTag ? nil : tag
            }
        )) {
            row(title: PrimitiveCopy.all, count: total, mono: false).tag(Self.allTag)
            Section {
                ForEach(names, id: \.name) { entry in
                    row(title: entry.name, count: entry.count, mono: true).tag(entry.name)
                }
            }
        }
        .listStyle(.sidebar)
    }

    private func row(title: String, count: Int, mono: Bool) -> some View {
        HStack(alignment: .firstTextBaseline) {
            Text(verbatim: title)
                .font(mono ? .mono(.subheadline) : .body.weight(.semibold))
            Spacer(minLength: 8)
            Text(verbatim: "\(count)")
                .font(.mono(.caption))
                .foregroundStyle(Color.togoMute)
        }
        .accessibilityElement(children: .combine)
    }
}
