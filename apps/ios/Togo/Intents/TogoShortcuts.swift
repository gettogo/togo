import AppIntents

/// The phrases Siri, Spotlight and the Action button offer without any setup.
struct TogoShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: NewSessionIntent(),
            phrases: ["New \(.applicationName) session"],
            shortTitle: LocalizedStringResource("intent.new.short", defaultValue: "New Session"),
            systemImageName: "plus.rectangle"
        )
        AppShortcut(
            intent: OpenSessionIntent(),
            phrases: ["Open \(.applicationName) session"],
            shortTitle: LocalizedStringResource("intent.open.short", defaultValue: "Open Session"),
            systemImageName: "arrow.up.forward.app"
        )
        AppShortcut(
            intent: WaitingCountIntent(),
            phrases: ["What's waiting in \(.applicationName)"],
            shortTitle: LocalizedStringResource("intent.waiting.short", defaultValue: "What's Waiting"),
            systemImageName: "bell.badge"
        )
    }
}
