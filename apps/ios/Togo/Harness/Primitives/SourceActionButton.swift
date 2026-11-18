import SwiftUI
import TogoKit

/// A declared source toggle sends its action once; the next source frame
/// supplies the state. It never writes an invented on/off argument.
struct SourceActionButton: View {
    let ref: HarnessActionRef
    var outcome: PrimitiveFeedback?
    var busy = false
    let run: () -> Void

    var body: some View {
        Group {
            if let on = ref.sourceOn {
                Toggle(isOn: Binding(get: { on }, set: { _ in run() })) {
                    label
                }
                .toggleStyle(.button)
            } else {
                Button(role: ref.role == .destructive ? .destructive : nil, action: run) { label }
            }
        }
        .disabled(busy || ref.sourceDisabled)
        .accessibilityHint(ref.sourceDisabled ? SurfaceValues.string(ref.args?[NativePickerScreen.disabledKey]) ?? "" : "")
    }

    private var label: some View {
        OutcomeLabel(title: ref.label, outcome: outcome, busy: busy)
    }
}

extension HarnessActionRef {
    var sourceOn: Bool? {
        guard id.hasPrefix(NativePickerScreen.actionPrefix), case .bool(let on)? = args?[NativePickerScreen.onKey] else { return nil }
        return on
    }

    var sourceDisabled: Bool {
        id.hasPrefix(NativePickerScreen.actionPrefix) && SurfaceValues.disabled(args?[NativePickerScreen.disabledKey])
    }
}
