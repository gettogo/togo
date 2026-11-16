import SwiftUI
import TogoKit

/// A sentence about the machine with one trailing action. At accessibility
/// sizes the button goes under the sentence.
struct MachineMessage: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let text: String
    let action: String
    let run: () -> Void

    init(_ text: String, action: String, run: @escaping () -> Void) {
        self.text = text
        self.action = action
        self.run = run
    }

    var body: some View {
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4))
            : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 12))
        layout {
            Text(text)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button(action, action: run)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .buttonStyle(.borderless)
                .frame(minHeight: 44)
        }
        // The 44-point button already gives the row its height; the list's own padding on top
        // of it left a gap under the machine's heading.
        .listRowInsets(.vertical, 0)
        .listRowBackground(Color.togoInk)
    }
}

/// A machine's helper update, done in place: the note offers Update, shows the update running,
/// then its result for a moment before leaving.
struct HelperUpdateLine: View {
    let machine: HostID
    let name: String
    let current: String
    let updates: HelperUpdates

    var body: some View {
        let phase = updates.phase(machine)
        switch phase {
        case .idle:
            if updates.available(machine, current: current) != nil {
                MachineMessage(HelperUpdateCopy.available, action: HelperUpdateCopy.update) {
                    Task { await updates.update(machine) }
                }
                .accessibilityLabel(HelperUpdateCopy.availableOn(name))
                .accessibilityAction(named: HelperUpdateCopy.updateOn(name)) { Task { await updates.update(machine) } }
                .disabled(updates.isChecking(machine))
            }
        case .updating:
            HStack(spacing: 12) {
                Text(HelperUpdateCopy.updating)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .frame(maxWidth: .infinity, alignment: .leading)
                ProgressView()
            }
            .frame(minHeight: 44)
            .listRowInsets(.vertical, 0)
            .listRowBackground(Color.togoInk)
        case .updated(let version):
            Text(HelperUpdateCopy.updated(version.map(HelperUpdate.display)))
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                .listRowInsets(.vertical, 0)
                .listRowBackground(Color.togoInk)
                .task {
                    try? await Task.sleep(for: .seconds(4))
                    updates.settle(machine)
                }
        case .failed(let message):
            MachineMessage("\(HelperUpdateCopy.failed) \(message)", action: HelperUpdateCopy.tryAgain) {
                Task { await updates.update(machine) }
            }
        case .unconfirmed(let message):
            MachineMessage("\(HelperUpdateCopy.unconfirmed) \(message)",
                           action: updates.isChecking(machine) ? HelperUpdateCopy.checking : HelperUpdateCopy.check) {
                Task { await updates.checkIfDue(machine) }
            }
            .disabled(updates.isChecking(machine))
        }
    }
}

