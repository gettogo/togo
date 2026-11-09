import SwiftUI

/// The one step battery mode needs on the machine itself, with the command to copy. It sits in
/// the keep-awake section, under the choice that was refused, rather than in a sheet: inside the
/// Settings sheet another sheet would stack on it (C8), and here it is next to what caused it.
struct AwakeBatterySetup: View {
    let name: String

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(AwakeCopy.sheetTitle(name: name))
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
                .fixedSize(horizontal: false, vertical: true)
            SetupStepView(step: SetupStep(text: AwakeCopy.sheetBody(name: name), command: AwakeCopy.command))
        }
        .padding(.vertical, 8)
    }
}
