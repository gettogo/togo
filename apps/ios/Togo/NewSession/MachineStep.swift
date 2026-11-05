import SwiftUI

struct MachineStep: View {
    let model: NewSessionModel
    let onChoose: (NewSessionModel.Machine) async -> Void

    var body: some View {
        StepScaffold(title: NewSessionCopy.machineTitle, prompt: NewSessionCopy.machinePrompt) {
            VStack(spacing: 0) {
                ForEach(model.machines) { machine in
                    ChoiceRow(
                        symbol: machine.symbol.systemImage, title: Text(verbatim: machine.name),
                        chevron: machine.unavailable == nil, enabled: machine.unavailable == nil,
                        action: { Task { await onChoose(machine) } }
                    ) {
                        if let reason = machine.unavailable {
                            Text(verbatim: reason)
                        } else if machine.helperMissing {
                            Text(verbatim: NewSessionCopy.helperMissing)
                        }
                    }
                }
            }
        }
    }
}
