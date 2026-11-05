import SwiftUI
import TogoKit

struct GestureSettingsView: View {
    let gestures: GestureBindings
    let update: (GestureBindings) -> Void

    /// Tap bindings people reach for most. A value set some other way still shows, as Custom.
    static let tapChoices: [(title: String, text: String)] = [
        ("Tab", "\t"),
        ("Esc", "\u{1b}"),
        ("Enter", "\r"),
        ("Ctrl C", "\u{3}"),
        (settingsText("settings.gestures.nothing", "Nothing"), ""),
    ]

    var body: some View {
        List {
            Section {
                tapPicker(settingsText("settings.gestures.doubleTap", "Double tap"), \.doubleTap)
                tapPicker(settingsText("settings.gestures.tripleTap", "Triple tap"), \.tripleTap)
            } footer: {
                Text(settingsText("settings.gestures.taps.footer", "Sent to the session when you tap the terminal."))
            }
            .groupedRows()
            Section {
                toggle(
                    settingsText("settings.gestures.twoFinger", "Two-finger swipe"),
                    settingsText("settings.gestures.twoFinger.detail", "Move between open sessions."),
                    \.twoFingerSwipe
                )
                toggle(
                    settingsText("settings.gestures.edge", "Edge swipe"),
                    settingsText("settings.gestures.edge.detail", "Swipe from the left edge to go back to your sessions."),
                    \.edgeSwipe
                )
                toggle(
                    settingsText("settings.gestures.pinch", "Pinch to zoom"),
                    settingsText("settings.gestures.pinch.detail", "Change the terminal text size."),
                    \.pinchZoom
                )
                toggle(
                    settingsText("settings.gestures.mouse", "Touch as mouse"),
                    settingsText("settings.gestures.mouse.detail", "Taps and drags reach apps that ask for the mouse, such as vim and htop."),
                    \.touchMouse
                )
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(settingsText("settings.gestures.title", "Gestures"))
        .navigationBarTitleDisplayMode(.inline)
    }

    private func binding<Value>(_ keyPath: WritableKeyPath<GestureBindings, Value>) -> Binding<Value> {
        Binding {
            gestures[keyPath: keyPath]
        } set: { value in
            var changed = gestures
            changed[keyPath: keyPath] = value
            update(changed)
        }
    }

    private func tapPicker(_ title: String, _ keyPath: WritableKeyPath<GestureBindings, String>) -> some View {
        let current = gestures[keyPath: keyPath]
        let known = Self.tapChoices.contains { $0.text == current }
        return Picker(title, selection: binding(keyPath)) {
            ForEach(Self.tapChoices, id: \.text) { choice in
                Text(choice.title).tag(choice.text)
            }
            if !known {
                Text(settingsText("settings.gestures.custom", "Custom")).tag(current)
            }
        }
    }

    private func toggle(_ title: String, _ detail: String, _ keyPath: WritableKeyPath<GestureBindings, Bool>) -> some View {
        Toggle(isOn: binding(keyPath)) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                Text(detail)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
            }
        }
        .frame(minHeight: 44)
    }
}
