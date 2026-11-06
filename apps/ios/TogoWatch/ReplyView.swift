import Observation
import SwiftUI
import TogoPush
import WatchKit

/// One reply being written on the Watch. The words go to the phone, which alone sends them to
/// the machine; whatever did not go stays here to send again.
@MainActor @Observable final class WatchReplyModel {
    enum State: Equatable, Sendable {
        case editing, sending, sent
        /// Not sent, with the sentence that says why.
        case problem(String)
    }

    var text = ""
    private(set) var state: State = .editing
    let target: WatchReplyMessage.Target
    /// The id the phone sees for these words. It stays the same while the same words are tried
    /// again, so the phone types them once, and changes when they change or after they went.
    @ObservationIgnored private(set) var replyID = UUID().uuidString
    /// The words of the last attempt, to tell an edit from a retry.
    @ObservationIgnored private var attempted: String?
    @ObservationIgnored private let deliver: @MainActor (WatchReplyMessage) async -> ReplyOutcome

    init(target: WatchReplyMessage.Target, send: @escaping @MainActor (WatchReplyMessage) async -> ReplyOutcome) {
        self.target = target
        deliver = send
    }

    var canSend: Bool {
        state != .sending && !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    /// `id` names the attempt instead of the model's own, as a notification's reply does.
    func send(id: String? = nil) async {
        guard canSend else { return }
        if let id { replyID = id }
        attempted = text
        state = .sending
        switch await deliver(WatchReplyMessage(id: replyID, target: target, text: text)) {
        case .sent:
            replyID = UUID().uuidString
            attempted = ""
            text = ""
            state = .sent
        case .needsPhone:
            state = .problem(WatchCopy.needsPhone)
        case .refused(let sentence), .failed(let sentence):
            state = .problem(sentence)
        }
    }

    /// Words that did not go from elsewhere, such as a notification, kept here with why.
    func keep(_ words: String, problem: String) {
        text = words
        attempted = words
        state = .problem(problem)
    }

    /// Changed words are a new reply: a new id, and the last result's line goes.
    func edited() {
        guard text != attempted, state != .sending else { return }
        attempted = nil
        replyID = UUID().uuidString
        switch state {
        case .problem, .sent: state = .editing
        case .editing, .sending: break
        }
    }
}

/// Writes a reply with dictation, Scribble or the keyboard through the system text field, and
/// sends it through the phone.
struct ReplyView: View {
    @Bindable var model: WatchReplyModel
    /// "01 Claude Code", the session the words go to.
    let title: String
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                Text(PromptText.clean(title, maxLines: 1))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                TextField(WatchCopy.replyPlaceholder, text: $model.text, axis: .vertical)
                    .disabled(model.state == .sending)
                status
                Button {
                    Task { await model.send() }
                } label: {
                    Text(model.state == .sending ? WatchCopy.sending : WatchCopy.send)
                        .frame(maxWidth: .infinity, minHeight: 32)
                }
                .disabled(!model.canSend)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .animation(.snappy, value: model.state)
        }
        .navigationTitle(WatchCopy.reply)
        .onChange(of: model.text) { model.edited() }
        .onChange(of: model.state) { _, state in
            switch state {
            case .sent:
                WKInterfaceDevice.current().play(.success)
                Task {
                    try? await Task.sleep(for: .seconds(1.2))
                    dismiss()
                }
            case .problem:
                WKInterfaceDevice.current().play(.failure)
            case .editing, .sending:
                break
            }
        }
    }

    @ViewBuilder private var status: some View {
        switch model.state {
        case .sent:
            Label(WatchCopy.replySent, systemImage: "checkmark")
                .font(.footnote.weight(.semibold))
        case .problem(let sentence):
            Text(sentence)
                .font(.footnote)
                .fixedSize(horizontal: false, vertical: true)
        case .editing, .sending:
            EmptyView()
        }
    }
}

/// The row or button that opens a reply.
struct ReplyLink: View {
    let store: WatchStore
    let target: WatchReplyMessage.Target
    let title: String

    var body: some View {
        NavigationLink {
            ReplyView(model: store.replyModel(target), title: title)
        } label: {
            Label(WatchCopy.reply, systemImage: "arrowshape.turn.up.left")
                .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
        }
    }
}

/// A reply screen to open from outside the lists: its target and the session it names.
struct ReplyRoute: Identifiable, Hashable, Sendable {
    static let key = "togoReply"

    var target: WatchReplyMessage.Target
    var title: String
    var id: WatchReplyMessage.Target { target }

    init(target: WatchReplyMessage.Target, title: String) {
        self.target = target
        self.title = title
    }

    /// Kept in a notice's `userInfo`: ids and the title only.
    var userInfo: [String: String] {
        var info = ["title": title]
        switch target {
        case .answer(let hostId, let requestId):
            info["hostId"] = hostId
            info["requestId"] = requestId
        case .prompt(let machineId, let sessionId):
            info["machineId"] = machineId
            info["sessionId"] = sessionId
        }
        return info
    }

    init?(userInfo: [String: String]?) {
        guard let userInfo else { return nil }
        var message: [String: Any] = ["type": WatchReplyMessage.type, "text": ""]
        for key in ["hostId", "requestId", "machineId", "sessionId"] { message[key] = userInfo[key] }
        guard let parsed = WatchReplyMessage(message: message) else { return nil }
        self.init(target: parsed.target, title: userInfo["title"] ?? "")
    }
}
