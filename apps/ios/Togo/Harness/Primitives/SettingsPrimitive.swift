import SwiftUI
import TogoKit
import UIKit

/// A form of typed fields: toggles, choices, text, numbers and secrets. Each change commits
/// on its own by running the field's action with `{"value": v}`; the control shows the new
/// value at once and goes back if the harness refuses it.
struct SettingsPrimitive: View {
    let settings: HarnessScreen.Settings
    let context: PrimitiveContext
    @State private var actions = PrimitiveActions()
    /// Values the user set that the harness has not confirmed yet, by field id.
    @State private var pending: [String: JSONValue] = [:]

    var body: some View {
        Form {
            ForEach(Array(settings.sections.enumerated()), id: \.offset) { _, section in
                Section {
                    ForEach(section.fields) { field in
                        SettingsFieldRow(
                            field: field,
                            value: pending[field.id] ?? field.value,
                            running: field.action.map { actions.isRunning($0, target: field.id) } ?? false
                        ) { commit(field, $0) }
                        .listRowBackground(Color.togoGrouped)
                    }
                } header: {
                    if let title = section.title { Text(title).textCase(nil) }
                } footer: {
                    if let footer = section.footer { Text(footer) }
                }
            }
        }
        .scrollContentBackground(.hidden)
        .background(Color.togoInk)
        .togoControls()
        .primitiveActions(actions, context: context)
        // A fresh read is the truth: drop what was waiting on the old one.
        .onChange(of: settings) { pending = [:] }
    }

    private func commit(_ field: SettingsLogic.Field, _ value: JSONValue) {
        guard SettingsLogic.isEditable(field), let action = field.action else { return }
        if field.kind != .secret { pending[field.id] = value }
        Task {
            let result = await actions.perform(action, target: field.id, args: SettingsLogic.commitArgs(value), context: context)
            if result == nil || result?.result == .failed || result?.result == .fallback {
                // The control goes back to the confirmed value in place.
                withAnimation(Motion.animation(.respond, reduce: UIAccessibility.isReduceMotionEnabled)) { pending[field.id] = nil }
            }
        }
    }
}

/// One field, drawn with the system control for its kind.
struct SettingsFieldRow: View {
    let field: SettingsLogic.Field
    let value: JSONValue
    let running: Bool
    let commit: (JSONValue) -> Void

    private var editable: Bool { SettingsLogic.isEditable(field) && !running }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            control
            if let help = field.help, !help.isEmpty, field.kind != .toggle {
                Text(help).font(.footnote).foregroundStyle(Color.togoMute)
            }
        }
        .accessibilityElement(children: .contain)
    }

    @ViewBuilder private var control: some View {
        switch field.kind {
        case .toggle:
            Toggle(isOn: Binding(get: { SettingsLogic.bool(value) }, set: { commit(.bool($0)) })) {
                labelStack
            }
            .disabled(!editable)
        case .choice:
            if SettingsLogic.showsInline(field) {
                ChoiceList(field: field, value: SettingsLogic.string(value), editable: editable, running: running) {
                    commit(.string($0))
                }
            } else {
                Picker(selection: Binding(get: { SettingsLogic.string(value) ?? "" }, set: { commit(.string($0)) })) {
                    ForEach(field.options ?? []) { Text($0.label).tag($0.id) }
                } label: {
                    labelStack
                }
                .pickerStyle(.menu)
                .disabled(!editable)
            }
        case .number:
            NumberField(field: field, value: SettingsLogic.number(value), editable: editable, running: running, commit: commit)
        case .secret:
            SecretField(field: field, editable: editable, running: running) { commit(.string($0)) }
        default:
            TextValueField(field: field, value: SettingsLogic.string(value) ?? "", editable: editable, running: running) {
                commit(.string($0))
            }
        }
    }

    private var labelStack: some View {
        HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(field.label).foregroundStyle(Color.togoSnow)
                if field.kind == .toggle, let help = field.help, !help.isEmpty {
                    Text(help).font(.footnote).foregroundStyle(Color.togoMute)
                }
            }
            RunningMark(running: running)
        }
    }
}

/// A choice whose options explain themselves: one row per option, a check on the chosen one.
private struct ChoiceList: View {
    let field: SettingsLogic.Field
    let value: String?
    let editable: Bool
    let running: Bool
    let choose: (String) -> Void
    @ScaledMetric(relativeTo: .body) private var checkWidth: CGFloat = 20

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(field.label).font(.subheadline).foregroundStyle(Color.togoMute)
                RunningMark(running: running)
            }
            .padding(.bottom, 4)
            ForEach(field.options ?? []) { option in
                Button { if option.id != value { choose(option.id) } } label: {
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Image(systemName: "checkmark")
                            .font(.body.weight(.semibold))
                            .opacity(option.id == value ? 1 : 0)
                            .frame(width: checkWidth)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: option.shownTitle).font(.body)
                            if let detail = option.shownDetail {
                                Text(verbatim: detail).font(.footnote).foregroundStyle(Color.togoMute)
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .foregroundStyle(Color.togoSnow)
                    .padding(.vertical, 8)
                    .frame(minHeight: 44)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .disabled(!editable)
                .accessibilityAddTraits(option.id == value ? [.isButton, .isSelected] : .isButton)
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(field.label)
    }
}

/// A number with a stepper. Taps settle for a moment before the value commits, so a run of
/// taps sends one action.
private struct NumberField: View {
    let field: SettingsLogic.Field
    let value: Double?
    let editable: Bool
    let running: Bool
    let commit: (JSONValue) -> Void
    @State private var draft: Double?

    var body: some View {
        let shown = draft ?? value ?? field.min ?? 0
        Stepper(
            value: Binding(get: { shown }, set: { draft = SettingsLogic.clamp($0, field) }),
            in: (field.min ?? -Double.greatestFiniteMagnitude)...(field.max ?? Double.greatestFiniteMagnitude),
            step: SettingsLogic.step(field)
        ) {
            HStack(spacing: 8) {
                Text(field.label).foregroundStyle(Color.togoSnow)
                Spacer(minLength: 8)
                RunningMark(running: running)
                Text(verbatim: value == nil && draft == nil ? PrimitiveCopy.notSet : SettingsLogic.numberText(shown))
                    .font(.mono(.body))
                    .monospacedDigit()
                    .foregroundStyle(Color.togoSnow)
            }
        }
        .disabled(!editable)
        .task(id: draft) {
            guard let draft else { return }
            try? await Task.sleep(for: .milliseconds(700))
            guard !Task.isCancelled else { return }
            if draft != value { commit(.number(draft)) }
            self.draft = nil
        }
        .accessibilityValue(SettingsLogic.numberText(shown))
    }
}

/// Text that commits on Return or Save.
private struct TextValueField: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let field: SettingsLogic.Field
    let value: String
    let editable: Bool
    let running: Bool
    let commit: (String) -> Void
    @State private var draft: String?
    @FocusState private var focused: Bool

    var body: some View {
        let text = draft ?? value
        HStack(spacing: 8) {
            LabeledContent {
                TextField(field.label, text: Binding(get: { text }, set: { draft = $0 }), prompt: field.placeholder.map { Text($0) })
                    .font(.mono(.body))
                    .multilineTextAlignment(.trailing)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .focused($focused)
                    .submitLabel(.done)
                    .onSubmit(save)
            } label: {
                Text(field.label).foregroundStyle(Color.togoSnow)
            }
            ZStack {
                if running {
                    ProgressView().controlSize(.small).transition(.opacity)
                } else if let draft, draft != value {
                    Button(PrimitiveCopy.save, action: save)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(Color.togoSnow)
                        .frame(minWidth: 44, minHeight: 44)
                        .transition(Motion.revealConceal(reduce: reduceMotion))
                }
            }
            .motion(.respond, value: running)
            .motion(.respond, value: draft != nil && draft != value)
        }
        .disabled(!editable)
        .onChange(of: value) { draft = nil }
    }

    private func save() {
        guard let draft, draft != value else { return }
        commit(draft)
        focused = false
    }
}

/// A secret: whether one is set, and a way to replace it. The field never holds or shows the
/// stored value; what the user types leaves with the action and is cleared.
private struct SecretField: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let field: SettingsLogic.Field
    let editable: Bool
    let running: Bool
    let commit: (String) -> Void
    @State private var editing = false
    @State private var entry = ""
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: field.isSet == true ? "key.fill" : "key")
                    .contentTransition(reduceMotion ? .identity : .symbolEffect(.replace))
                    .foregroundStyle(Color.togoMute)
                    .accessibilityHidden(true)
                Text(field.label).foregroundStyle(Color.togoSnow)
                Spacer(minLength: 8)
                RunningMark(running: running)
                Text(SettingsLogic.secretStatus(field))
                    .foregroundStyle(Color.togoMute)
                if editable, !editing {
                    Button(field.isSet == true ? PrimitiveCopy.replace : PrimitiveCopy.set) {
                        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { editing = true }
                        focused = true
                    }
                    .transition(Motion.revealConceal(reduce: reduceMotion))
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                    .frame(minWidth: 44, minHeight: 44)
                }
            }
            .accessibilityElement(children: .combine)
            if editing {
                VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 8) {
                    SecureField(PrimitiveCopy.newValue, text: $entry, prompt: Text(field.placeholder ?? PrimitiveCopy.newValue))
                        .font(.mono(.body))
                        .textContentType(.password)
                        .focused($focused)
                        .submitLabel(.done)
                        .onSubmit(save)
                    Button(PrimitiveCopy.cancel) { close() }
                        .buttonStyle(PrimitiveCapsuleStyle(role: .normal))
                    Button(PrimitiveCopy.save, action: save)
                        .buttonStyle(PrimitiveCapsuleStyle(role: .primary))
                        .disabled(entry.isEmpty)
                }
                Text(PrimitiveCopy.secretFooter).font(.footnote).foregroundStyle(Color.togoMute)
                }
                .transition(Motion.revealConceal(reduce: reduceMotion))
            }
        }
        .onDisappear { entry = "" }
    }

    private func save() {
        guard !entry.isEmpty else { return }
        commit(entry)
        close()
    }

    private func close() {
        entry = ""
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { editing = false }
        focused = false
    }
}

/// A field's progress in a fixed slot beside it, so the control never shifts when it shows.
private struct RunningMark: View {
    let running: Bool

    var body: some View {
        ZStack {
            if running { ProgressView().controlSize(.small).transition(.opacity) }
        }
        .slot(width: 20, key: running)
    }
}
