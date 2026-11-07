import SwiftUI
import TogoKit

struct AgentStep: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var showsMissing = false
    let model: NewSessionModel
    let onStart: () async -> Void

    var body: some View {
        StepScaffold(title: NewSessionCopy.agentTitle) {
            VStack(alignment: .leading, spacing: 24) {
                agents
                if case .loaded = model.agents, model.showsFlags { launchOptions }
                nameField
                // At large text sizes the pinned footer would cover the list, so the error scrolls.
                if dynamicTypeSize.isAccessibilitySize, let error = model.createError {
                    InlineError(message: error)
                }
            }
        }
        // The list scrolls under the Start button behind the system's soft edge.
        .safeAreaBar(edge: .bottom) { footer }
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
                ForEach(model.installedAgents, id: \.id) { agent in
                    agentRow(agent)
                }
                ForEach(model.presets) { preset in
                    presetRow(preset)
                }
                if !model.missingAgents.isEmpty {
                    missingToggle
                    if showsMissing {
                        ForEach(model.missingAgents, id: \.id) { agent in
                            agentRow(agent)
                        }
                    }
                }
            }
        }
    }

    /// A registry agent with its star. The star sits beside the row, not in it, so starring
    /// never chooses the agent.
    private func agentRow(_ agent: AgentInfo) -> some View {
        HStack(spacing: 0) {
            ChoiceRow(
                symbol: "terminal", agent: agent.id, title: Text(verbatim: agent.name),
                selected: agent.installed && agent.id == model.selectedRegistryID,
                enabled: agent.installed,
                action: { model.choose(agent: agent.id) }
            ) {
                if !agent.installed {
                    Text(verbatim: NewSessionCopy.notInstalled(on: model.machineName))
                }
            }
            if agent.id != "shell" { star(agent) }
        }
        .overlay(alignment: .bottom) { Color.togoLine.frame(height: 1) }
    }

    private func star(_ agent: AgentInfo) -> some View {
        let on = model.isFavorite(agent.id)
        return Button {
            withAnimation(Motion.animation(.confirm, reduce: reduceMotion)) { model.toggleFavorite(agent.id) }
        } label: {
            Image(systemName: on ? "star.fill" : "star")
                .font(.body)
                .foregroundStyle(on ? Color.togoSnow : Color.togoMute)
                .frame(width: 44, height: 44)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(on ? NewSessionCopy.removeFavorite : NewSessionCopy.addFavorite)
        .accessibilityValue(Text(verbatim: agent.name))
    }

    /// Agents the machine lacks stay folded away, so the list is the ones that can start.
    private var missingToggle: some View {
        Button {
            withAnimation(Motion.animation(.collapse, reduce: reduceMotion)) { showsMissing.toggle() }
        } label: {
            HStack(spacing: 8) {
                Text(NewSessionCopy.notInstalledHeading)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                Text(verbatim: NewSessionCopy.notInstalledCount(model.missingAgents.count))
                    .font(.system(.subheadline, design: .monospaced))
                    .foregroundStyle(Color.togoMute)
                Spacer(minLength: 8)
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.togoMute)
                    .rotationEffect(.degrees(showsMissing ? 90 : 0))
            }
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(.isHeader)
        .accessibilityValue(showsMissing ? NewSessionCopy.expanded : NewSessionCopy.collapsed)
        .padding(.top, 8)
    }

    private func presetRow(_ preset: AgentPreset) -> some View {
        ChoiceRow(
            symbol: "terminal", agent: preset.agent, title: Text(verbatim: preset.name),
            selected: preset.id == model.selectedPreset?.id,
            enabled: model.canUse(preset),
            action: { model.choose(preset: preset) }
        ) {
            if !model.flagsAvailable {
                Text(verbatim: NewSessionCopy.flagsNeedUpdate(model.machineName))
            } else if !model.canUse(preset) {
                Text(verbatim: NewSessionCopy.notInstalled(on: model.machineName))
            } else if model.isRemembered(preset) {
                Text(NewSessionCopy.lastUsedHere)
            } else {
                Text(verbatim: AgentPresetText.commandLine(preset))
                    .font(.system(.subheadline, design: .monospaced))
                    .lineLimit(2)
                    .truncationMode(.middle)
            }
        }
    }

    private var launchOptions: some View {
        VStack(alignment: .leading, spacing: 12) {
            FlagsField(
                text: Bindable(model).flags, enabled: model.flagsAvailable,
                footer: model.flagsFooter, error: model.flagsError
            )
            if model.selectedPreset != nil {
                Toggle(NewSessionCopy.rememberFolder, isOn: Bindable(model).rememberFolder)
                    .font(.body)
                    .foregroundStyle(Color.togoSnow)
                    .frame(minHeight: 44)
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
            StartButton(title: model.startTitle, creating: model.creating, onStart: onStart)
                .disabled(!model.canStart)
        }
        .screenMargins()
        .padding(.vertical, 12)
    }
}
