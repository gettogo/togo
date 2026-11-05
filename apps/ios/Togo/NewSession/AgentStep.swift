import SwiftUI
import TogoKit

struct AgentStep: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let model: NewSessionModel
    let onStart: () async -> Void

    var body: some View {
        StepScaffold(title: NewSessionCopy.agentTitle) {
            VStack(alignment: .leading, spacing: 24) {
                agents
                nameField
                // At large text sizes the pinned footer would cover the list, so the error scrolls.
                if dynamicTypeSize.isAccessibilitySize, let error = model.createError {
                    InlineError(message: error)
                }
            }
        }
        // At large text sizes the list runs under the Start button; the hard edge marks where.
        .safeAreaBar(edge: .bottom) { footer }
        .scrollEdgeEffectStyle(.hard, for: .bottom)
    }

    @ViewBuilder private var agents: some View {
        switch model.agents {
        case .idle, .loading:
            PawWalk(NewSessionCopy.agentLoading).padding(.vertical, 12)
        case .failed(let message):
            InlineError(message: message)
            RetryButton { Task { await model.loadAgents() } }
        case .loaded:
            VStack(spacing: 0) {
                ForEach(model.offeredAgents, id: \.id) { agent in
                    ChoiceRow(
                        symbol: Self.symbol(for: agent.id), title: Text(verbatim: agent.name),
                        selected: agent.installed && agent.id == model.selectedAgent?.id,
                        enabled: agent.installed,
                        action: { model.agentID = agent.id }
                    ) {
                        if !agent.installed {
                            Text(verbatim: NewSessionCopy.notInstalled(on: model.machineName))
                        }
                    }
                }
            }
        }
    }

    private var nameField: some View {
        TextField(NewSessionCopy.namePlaceholder, text: Bindable(model).name, axis: .vertical)
            .font(.body)
            .lineLimit(1...4)
            .onChange(of: model.name) {
                if model.name.contains(where: \.isNewline) { model.name = model.name.filter { !$0.isNewline } }
            }
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .submitLabel(.done)
            .padding(.vertical, 10)
            .frame(minHeight: 44)
            .overlay(alignment: .bottom) { Color.togoLine.frame(height: 1) }
    }

    private var footer: some View {
        VStack(spacing: 12) {
            if !dynamicTypeSize.isAccessibilitySize, let error = model.createError { InlineError(message: error) }
            StartButton(title: NewSessionCopy.start(model.selectedAgent?.name ?? ""), creating: model.creating, onStart: onStart)
                .disabled(!model.canStart)
        }
        .screenMargins()
        .padding(.vertical, 12)
    }

    private static func symbol(for agentID: String) -> String {
        switch agentID {
        case "claude": "asterisk"
        case "codex": "curlybraces"
        case "omp": "cube"
        default: "terminal"
        }
    }
}
