import SwiftUI
import TogoKit

/// The same request controls as the prompt bar, explicitly opened from the terminal's More key.
struct RequestPromptSheet: View {
    @State private var model: RequestPromptModel

    init(store: AppStore, hostID: HostID, requestID: String) {
        _model = State(initialValue: RequestPromptModel(store: store, hostID: hostID, requestID: requestID))
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                Group {
                    switch model.bar.stage {
                    case let .request(request, position, count):
                        PromptRequestView(model: model.bar, request: request, position: position, count: count, replying: false)
                    case let .replying(request, position, count):
                        PromptRequestView(model: model.bar, request: request, position: position, count: count, replying: true)
                    default:
                        Text(PromptCopy.withdrawn).foregroundStyle(Color.togoMute)
                    }
                }
                .padding(16)
            }
            .background(Color.togoInk)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(PromptCopy.done, systemImage: "checkmark", role: .confirm) { model.close() }
                }
            }
        }
        .onChange(of: model.request, initial: true) { model.update() }
        .onChange(of: model.bar.dismissals) { model.update() }
    }
}

@MainActor final class RequestPromptModel {
    let bar: PromptBarModel
    private let store: AppStore
    private let hostID: HostID
    private let requestID: String
    private var sheet: AppSheet { .requestPrompt(hostID, requestID) }

    init(store: AppStore, hostID: HostID, requestID: String, armDelay: Duration = PromptBarModel.defaultArmDelay) {
        self.store = store
        self.hostID = hostID
        self.requestID = requestID
        bar = PromptBarModel(armDelay: armDelay)
        bar.onAnswer = { [weak self] request, answer in self?.answer(answer, to: request) }
        bar.onOpen = { [weak self] _ in self?.openSession() }
    }

    private var card: RequestCard? {
        store.controlCenter.requests.first { $0.hostID == hostID && $0.id == requestID }
    }

    var request: PromptRequest? {
        card.map { card in
            let row = card.sessionID.flatMap { store.session(hostID: hostID, sessionID: $0) }
            return PromptFeed.request(card, row: row, machineName: store.machineName(for: hostID),
                                      failure: store.answerFailures[card.id])
        }
    }

    func update() {
        guard store.sheet == sheet else { return }
        let next = PromptFeed.mode(requests: request.map { [$0] } ?? [], current: bar.mode, hasMachines: true)
        if next != bar.mode { bar.mode = next }
    }

    func close() {
        if store.sheet == sheet { store.sheet = nil }
    }

    private func answer(_ answer: PromptAnswer, to request: PromptRequest) {
        guard store.sheet == sheet, let card, card.id == request.id, card.phase == .open,
              let decision = PromptFeed.decision(answer, for: card) else { return }
        let store = store
        Task { await store.answer(card.id, decision) }
    }

    private func openSession() {
        guard store.sheet == sheet, let card, card.sessionID != nil else { return }
        close()
        store.openRequest(card)
    }
}

extension AppStore {
    /// More opens the request without replacing another form or relying on keyboard visibility.
    @discardableResult
    func showRequest(for tab: TabID) -> Bool {
        guard sheet?.isForm != true,
              let card = PromptKeys.card(for: tab.sessionID, hostID: tab.hostID, in: controlCenter.requests) else { return false }
        showSheet(.requestPrompt(tab.hostID, card.id))
        return true
    }
}
