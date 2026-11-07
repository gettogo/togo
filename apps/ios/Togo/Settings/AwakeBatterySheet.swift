import SwiftUI

/// Explains the one step battery mode needs on the machine itself, with the command to copy.
struct AwakeBatterySheet: View {
    let name: String

    @Environment(\.dismiss) private var dismiss
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var copied = false
    @State private var copies = 0

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                Text(AwakeCopy.sheetTitle(name: name))
                    .font(.title2.bold())
                    .foregroundStyle(Color.togoSnow)
                    .accessibilityAddTraits(.isHeader)
                Text(AwakeCopy.sheetBody(name: name))
                    .font(.body)
                    .foregroundStyle(Color.togoMute)
                    .fixedSize(horizontal: false, vertical: true)
                CopyableText(text: AwakeCopy.command)
                VStack(spacing: 12) {
                    Button {
                        UIPasteboard.general.string = AwakeCopy.command
                        copies += 1
                        copied = true
                        AccessibilityNotification.Announcement(AwakeCopy.copied).post()
                    } label: {
                        // "Copied" crossfades in on `respond`, in a button that keeps its size.
                        Text(copied ? AwakeCopy.copied : settingsText("awake.sheet.copy", "Copy command"))
                            .contentTransition(reduceMotion ? .opacity : .interpolate)
                    }
                    .buttonStyle(.primary(.full))
                    .motion(.respond, value: copied)
                    Button(settingsText("awake.sheet.done", "Done")) { dismiss() }
                        .buttonStyle(.secondary(.full))
                }
                .padding(.top, 4)
            }
            .padding(24)
        }
        .background(Color.togoInk)
        .presentationDetents([.medium, .large])
        .haptic(.done, trigger: copies)
        .task(id: copies) {
            guard copies > 0 else { return }
            try? await Task.sleep(for: .seconds(2))
            if !Task.isCancelled { copied = false }
        }
    }
}
