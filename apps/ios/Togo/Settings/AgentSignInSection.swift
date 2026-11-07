import SwiftUI
import TogoKit

/// "Sign in" rows for the agents a machine has installed. Each starts the agent's login
/// command in a muted session on the machine and opens it; the steps it shows (a page, a
/// code, a key prompt, a provider menu) come up as requests to answer here.
struct AgentSignInSection: View {
    let machine: HostID
    @Environment(\.machineTools) private var tools
    @Environment(\.signInStore) private var store
    @State private var commands: [SignInCommand] = []
    @State private var starting: String?
    @State private var failure: String?

    var body: some View {
        Section {
            ForEach(commands) { command in
                Button { Task { await start(command) } } label: {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(command.title)
                            .foregroundStyle(Color.togoSnow)
                        Text(command.note)
                            .font(.footnote)
                            .foregroundStyle(Color.togoMute)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .contentShape(.rect)
                }
                .disabled(starting != nil || store == nil)
                .accessibilityHint(AgentSignInCopy.hint)
            }
            if let failure {
                Text(failure)
                    .font(.footnote)
                    .foregroundStyle(Color.red)
            }
        } header: {
            if !commands.isEmpty { Text(AgentSignInCopy.title) }
        } footer: {
            if !commands.isEmpty { Text(AgentSignInCopy.footer) }
        }
        .task(id: machine) { await load() }
    }

    private func load() async {
        guard let agents = try? await tools.call(machine, { try await $0.agents() }) else { return }
        commands = SignInCatalog.commands(installed: Set(agents.filter(\.installed).map(\.id)))
    }

    private func start(_ command: SignInCommand) async {
        guard let store else { return }
        starting = command.id
        failure = nil
        defer { starting = nil }
        do {
            let row = try await store.services.create(hostID: machine, params: SignInCatalog.params(for: command))
            store.sheet = nil
            store.open(hostID: machine, sessionID: row.id)
            if let text = command.typeAfterStart {
                let tools = tools
                let machine = machine
                Task {
                    // The agent's interface takes a few seconds to come up before it reads input.
                    try? await Task.sleep(for: .seconds(8))
                    _ = try? await tools.call(machine) { try await $0.sendText(id: row.id, text: text, submit: true) }
                }
            }
        } catch {
            failure = ServiceErrors.message(error)
        }
    }
}

enum AgentSignInCopy {
    static let title = String(localized: "settings.signIn.title", defaultValue: "Sign in")
    static let footer = String(
        localized: "settings.signIn.footer",
        defaultValue: "Starts the agent's sign-in on this machine in a muted session. Keys you paste go straight to the agent and are not kept."
    )
    static let hint = String(localized: "settings.signIn.hint", defaultValue: "Starts the sign-in and opens it.")
}

extension EnvironmentValues {
    /// The window's store, for starting sign-in sessions from Settings. Nil outside a window.
    @Entry var signInStore: AppStore?
}
