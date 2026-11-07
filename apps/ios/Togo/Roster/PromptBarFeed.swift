import SwiftUI
import TogoKit
import TogoTerminal

extension View {
    /// Keeps `model` in step with the store and wires its answers, messages and starts to the
    /// app's own paths. `focused` is the terminal the bar types into, nil when none is on screen.
    func feedsPromptBar(_ model: PromptBarModel, store: AppStore, terminals: TerminalPool, focused: TabID?) -> some View {
        modifier(PromptBarFeed(model: model, store: store, terminals: terminals, focused: focused))
    }
}

private struct PromptBarFeed: ViewModifier {
    @Environment(\.machineTools) private var machineTools
    @Environment(\.agentLibrary) private var agentLibrary
    let model: PromptBarModel
    let store: AppStore
    let terminals: TerminalPool
    let focused: TabID?

    private struct Inputs: Equatable {
        var requests: [PromptRequest]
        var focused: TabID?
        var composing: Bool
        var hasMachines: Bool
        /// The on-screen keyboard types into the terminal, not into a field.
        var terminalTyping: Bool
        /// The terminal the bar would type into has a live link.
        var focusedConnected: Bool
        /// Something else holds the keyboard or covers the bar: a sheet, a pushed screen such as
        /// Settings, a dialog, the compose bar or the terminal.
        var keyboardElsewhere: Bool
    }

    private var suggestions: [StartSuggestion] {
        PromptFeed.suggestions(
            machines: store.controlCenter.machines, archived: store.controlCenter.archived,
            favorites: agentLibrary?.favorites ?? []
        )
    }

    private var inputs: Inputs {
        Inputs(
            requests: PromptFeed.requests(in: store), focused: focused, composing: store.composing != nil,
            hasMachines: !store.services.hosts.isEmpty,
            terminalTyping: focused != nil && KeyboardWatch.shared.terminalTyping && !model.barTextFocused,
            focusedConnected: focused.map(isConnected) ?? true,
            keyboardElsewhere: focused != nil || store.composing != nil || store.sheet != nil || !store.path.isEmpty
                || store.dialogOpen || store.reviewing != nil
        )
    }

    private func isConnected(_ id: TabID) -> Bool {
        switch store.workspace.tabs.first(where: { $0.id == id })?.link {
        case .live?, .roaming?: true
        default: false
        }
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
            .onChange(of: store.services.settings.keyBarConfig, initial: true) { _, bar in
                model.keyBar = bar
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
            requests: inputs.requests, current: current, focused: inputs.focused,
            composing: inputs.composing, hasMachines: inputs.hasMachines, terminalTyping: inputs.terminalTyping,
            focusedConnected: inputs.focusedConnected
        )
        if next != model.mode { model.mode = next }
        // A request shown over a terminal that has the keyboard keeps to its smallest form.
        if model.compact != inputs.terminalTyping { model.compact = inputs.terminalTyping }
        // Typing y, n or r anywhere else stays typing rather than an answer.
        model.otherTextFocused = inputs.keyboardElsewhere
    }

    private func wire() {
        let store = self.store
        let terminals = self.terminals
        let tools = machineTools
        let library = agentLibrary
        model.onAnswer = { request, answer in
            // Words for a prompt that takes none are never typed into it: Enter would pick its
            // highlighted option.
            guard let card = store.controlCenter.requests.first(where: { $0.id == request.id }),
                  let decision = PromptFeed.decision(answer, for: card) else { return }
            Task { await store.answer(card.id, decision) }
        }
        model.onSend = { tab, text in
            send(text, to: tab, store: store, terminals: terminals, tools: tools)
        }
        model.tabName = { tab in
            let open = store.workspace.tabs.first { $0.id == tab }
            return open.flatMap { store.session(for: $0) }.map { $0.name.isEmpty ? $0.agentName : $0.name }
                ?? open?.title ?? tab.sessionID
        }
        model.onKey = { tab, item in
            // The handle types text; keystrokes go through the terminal's own key bar.
            guard case .steps(let steps) = item.action, let handle = terminals.handle(for: tab) else { return }
            for case .text(let text) in steps {
                handle.paste(text)
            }
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
            store.open(hostID: card.hostID, sessionID: sessionID)
        }
    }
}

/// Types `text` into the session's terminal when this app holds one, the way the compose bar
/// does; otherwise it goes through the helper like a reply from a notification.
@MainActor private func send(_ text: String, to tab: TabID, store: AppStore, terminals: TerminalPool, tools: any MachineTools) {
    let message = text.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !message.isEmpty else { return }
    if let handle = terminals.handle(for: tab) {
        handle.paste(message)
        handle.pressEnter()
        return
    }
    let sender = ReplySender(tools: tools, label: { store.replyLabel($0) })
    Task {
        await store.deliverReply(.prompt(host: tab.hostID, session: tab.sessionID, text: message), session: tab.sessionID, via: sender)
    }
}
