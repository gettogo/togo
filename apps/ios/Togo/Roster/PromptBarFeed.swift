import SwiftUI
import TogoKit

extension View {
    /// Keeps `model` in step with the store and wires its answers and starts to the app's own
    /// paths. `focused` is the tab on screen, nil when none is.
    func feedsPromptBar(_ model: PromptBarModel, store: AppStore, focused: TabID?) -> some View {
        modifier(PromptBarFeed(model: model, store: store, focused: focused))
    }
}

private struct PromptBarFeed: ViewModifier {
    @Environment(\.agentLibrary) private var agentLibrary
    let model: PromptBarModel
    let store: AppStore
    let focused: TabID?

    private struct Inputs: Equatable {
        var requests: [PromptRequest]
        var hasMachines: Bool
        /// The on-screen keyboard types into the terminal, not into a field.
        var terminalTyping: Bool
        /// Something else holds the keyboard or covers the bar: a sheet, a pushed screen such as
        /// Settings, a dialog or the terminal.
        var keyboardElsewhere: Bool
        /// The on-screen keyboard types into the chat's composer.
        var composerTyping: Bool
        /// The focused session shows its terminal: its requests are answered there, not here.
        var terminalLayer: Bool
    }

    private var suggestions: [StartSuggestion] {
        PromptFeed.suggestions(
            machines: store.controlCenter.machines, archived: store.controlCenter.archived,
            favorites: agentLibrary?.favorites ?? []
        )
    }

    private var inputs: Inputs {
        let chat = focused.flatMap { store.layer(for: $0) == .chat ? $0 : nil }
        // A session showing its chat, or still deciding between chat and terminal (S4), types
        // into its composer when anything does.
        let typesElsewhere = focused.map { SessionInput(store.layer(for: $0)).hidesPromptBar } ?? false
        return Inputs(
            requests: PromptFeed.requests(in: store, inlineIn: chat),
            hasMachines: !store.services.hosts.isEmpty,
            terminalTyping: focused != nil && KeyboardWatch.shared.terminalTyping && !model.barTextFocused,
            keyboardElsewhere: focused != nil || store.sheet != nil || !store.path.isEmpty
                || store.dialogOpen || store.reviewing != nil,
            composerTyping: typesElsewhere && KeyboardWatch.shared.keyboardUp && !KeyboardWatch.shared.terminalTyping
                && !model.barTextFocused,
            terminalLayer: focused.map { store.layer(for: $0) == .terminal } ?? false
        )
    }

    func body(content: Content) -> some View {
        content
            .onChange(of: inputs, initial: true) { _, inputs in
                update(inputs, current: model.mode)
            }
            // A reply whose request went away was put away: show what comes next.
            .onChange(of: model.dismissals) {
                update(inputs, current: .hidden)
            }
            .onChange(of: suggestions, initial: true) { _, found in
                model.startSuggestions = found
            }
            .onChange(of: store.services.settings.singleKeyAnswers, initial: true) { _, on in
                model.singleKeyAnswers = on
            }
            .onChange(of: store.startRequests) {
                if case .start = model.mode {
                    model.openStart()
                } else {
                    store.startNewSession()
                }
            }
            .onAppear(perform: wire)
    }

    private func update(_ inputs: Inputs, current: PromptBarModel.Mode) {
        let next = PromptFeed.mode(
            requests: inputs.requests, current: current, hasMachines: inputs.hasMachines,
            composerTyping: inputs.composerTyping, terminalLayer: inputs.terminalLayer
        )
        if next != model.mode { model.mode = next }
        // A request shown over a terminal that has the keyboard keeps to its smallest form.
        if model.compact != inputs.terminalTyping { model.compact = inputs.terminalTyping }
        // Typing y, n or r anywhere else stays typing rather than an answer.
        model.otherTextFocused = inputs.keyboardElsewhere
    }

    private func wire() {
        let store = self.store
        let library = agentLibrary
        model.onAnswer = { request, answer in
            // Words for a prompt that takes none are never typed into it: Enter would pick its
            // highlighted option.
            guard let card = store.controlCenter.requests.first(where: { $0.id == request.id }),
                  let decision = PromptFeed.decision(answer, for: card) else { return }
            Task { await store.answer(card.id, decision) }
        }
        model.onStart = { suggestion in
            guard let target = PromptFeed.target(of: suggestion) else {
                store.startNewSession()
                return
            }
            // The agent comes back as the first saved preset that runs it, when there is one.
            let preset = library?.presets.first { $0.agent == target.agent }?.id
            store.newSessionPrefill = NewSessionPrefill(hostID: target.hostID, folder: target.folder, presetID: preset)
            store.showSheet(.newSession(target.hostID))
        }
        model.onOpenStartSheet = { store.startNewSession() }
        model.onOpen = { request in
            guard let card = store.controlCenter.requests.first(where: { $0.id == request.id }),
                  let sessionID = card.sessionID else { return }
            // As a tap on its line: beside the roster out from under whatever the detail had
            // pushed, into the focused pane.
            if store.workspaceInline {
                store.showSession(TabID(hostID: card.hostID, sessionID: sessionID))
            } else {
                store.open(hostID: card.hostID, sessionID: sessionID)
            }
        }
    }
}
