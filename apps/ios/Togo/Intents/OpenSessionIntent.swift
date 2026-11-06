import AppIntents
import Foundation

/// Opens a session in the app, the same way its notification does. Spotlight runs it when a
/// session result is tapped.
struct OpenSessionIntent: OpenIntent {
    static let title = LocalizedStringResource("intent.open.title", defaultValue: "Open Session")
    static let description = IntentDescription(LocalizedStringResource(
        "intent.open.description", defaultValue: "Opens a session in Togo."
    ))
    static let openAppWhenRun = true

    @Parameter(title: LocalizedStringResource("intent.open.session", defaultValue: "Session"))
    var target: SessionEntity

    var session: SessionEntity {
        get { target }
        nonmutating set { target = newValue }
    }

    static var parameterSummary: some ParameterSummary {
        Summary("Open \(\.$target)")
    }

    @MainActor func perform() async throws -> some IntentResult {
        IntentHub.current.open(target.deepLink)
        return .result()
    }
}
