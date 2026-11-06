import AppIntents
import Foundation

/// Opens the new-session sheet on a machine with the folder and agent filled in, through the
/// same `togo://new` link anything else can send. Starting is left to the sheet: the machine
/// signs in with the app's keys, which may need Face ID, and the user taps Start.
struct NewSessionIntent: AppIntent {
    static let title = LocalizedStringResource("intent.new.title", defaultValue: "New Session")
    static let description = IntentDescription(LocalizedStringResource(
        "intent.new.description", defaultValue: "Opens a new session on a machine with the folder and agent filled in."
    ))
    static let openAppWhenRun = true

    @Parameter(title: LocalizedStringResource("intent.new.machine", defaultValue: "Machine"))
    var machine: MachineEntity

    @Parameter(title: LocalizedStringResource("intent.new.folder", defaultValue: "Folder"))
    var folder: String?

    @Parameter(title: LocalizedStringResource("intent.new.agent", defaultValue: "Agent"))
    var agent: AgentPresetEntity?

    static var parameterSummary: some ParameterSummary {
        Summary("New session on \(\.$machine)") {
            \.$folder
            \.$agent
        }
    }

    @MainActor func perform() async throws -> some IntentResult {
        IntentHub.current.open(Self.link(machine: machine.name, folder: folder, preset: agent?.id))
        return .result()
    }

    /// Built as a URL and read back, so the intent's link passes the same checks as any other.
    static func link(machine: String, folder: String?, preset: UUID?) -> DeepLink {
        var parts = URLComponents()
        parts.scheme = DeepLink.scheme
        parts.host = "new"
        parts.queryItems = [
            URLQueryItem(name: "host", value: machine),
            folder.map { URLQueryItem(name: "folder", value: $0) },
            preset.map { URLQueryItem(name: "agent", value: $0.uuidString) },
        ].compactMap { $0 }
        return parts.url.flatMap(DeepLink.init(url:)) ?? .newSession(host: machine)
    }
}
