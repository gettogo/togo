import Foundation
import Synchronization
import Testing
import TogoPush
@testable import TogoWatch

private final class NoSender: AnswerSending {
    func send(requestId: String, decision: String, pairing: Pairing) async throws {}
}

/// The phone's side of the link, as the Watch sees it.
@MainActor private final class Phone {
    var outcome = ReplyOutcome.needsPhone
    var sent: [WatchReplyMessage] = []
}

@MainActor private final class RunAgainPhone {
    var outcome = ReplyOutcome.needsPhone
    var sent: [WatchRunAgainMessage] = []
}

private let machine = "6F1C2D3E-0000-4000-8000-000000000001"
private let host = "h_mmg42klgyqzwneiskrelxms3j7"
private let session = "s_abcdefghij"

@MainActor struct WatchReplyTests {
    private let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
    private let now = Date(timeIntervalSinceReferenceDate: 815_000_000)

    private func store() -> WatchStore {
        let now = self.now
        return WatchStore(dependencies: .init(
            pending: PendingStore(directory: directory, clock: { now }), pairings: PairingStore(directory: directory),
            sender: NoSender(), clock: { now }
        ))
    }

    private func request(_ id: String, allowText: Bool? = nil) -> PushPayload {
        PushPayload(
            kind: .needsYou, sessionId: session, bib: "04", agent: "claude", host: "studio", title: "Which one?",
            body: "", requestId: id, options: [PushOption(n: 1, label: "No", role: "deny")],
            ts: Int64(now.timeIntervalSince1970 * 1000), allowText: allowText
        )
    }

    private func watchSession(_ status: String, reason: String? = nil, ids: Bool = true) -> WatchSession {
        WatchSession(
            hostName: "studio", bib: "01", agentName: "Claude Code", status: status, activity: nil, exitReason: reason,
            machineId: ids ? machine : nil, sessionId: ids ? session : nil
        )
    }

    /// The phone could not reach the machine without asking: the Watch says to open the phone
    /// and keeps every word typed, so sending again later needs no retyping.
    @Test func watchReplyNeedsPhoneWhenChannelClosed() async {
        let phone = Phone()
        let model = WatchReplyModel(target: .prompt(machineId: machine, sessionId: session)) { message in
            phone.sent.append(message)
            return phone.outcome
        }
        #expect(!model.canSend, "nothing typed yet")
        model.text = "  "
        #expect(!model.canSend)
        model.text = "Run the tests again"
        await model.send()
        #expect(phone.sent.map(\.text) == ["Run the tests again"])
        #expect(phone.sent.map(\.target) == [.prompt(machineId: machine, sessionId: session)])
        #expect(model.state == .problem("Open Togo on your iPhone to send this."))
        #expect(model.text == "Run the tests again")

        phone.outcome = .refused("Not sent: 01 Claude Code has ended.")
        await model.send()
        #expect(model.state == .problem("Not sent: 01 Claude Code has ended."))
        #expect(model.text == "Run the tests again")

        phone.outcome = .sent
        await model.send()
        #expect(model.state == .sent)
        #expect(model.text == "")
    }

    /// A retry of the same words keeps their id, so the phone types them once; edited words, or
    /// words after a send went, get a new one. Typing again after a send clears "Sent".
    @Test func retryKeepsTheReplyID() async {
        let phone = Phone()
        let model = WatchReplyModel(target: .prompt(machineId: machine, sessionId: session)) { message in
            phone.sent.append(message)
            return phone.outcome
        }
        model.text = "go on"
        await model.send()
        model.edited()
        await model.send()
        #expect(phone.sent.count == 2)
        #expect(phone.sent[0].id == phone.sent[1].id, "a retry of the same words")

        model.text = "go on, slowly"
        model.edited()
        #expect(model.state == .editing)
        phone.outcome = .sent
        await model.send()
        #expect(phone.sent[2].id != phone.sent[1].id, "edited words")
        model.edited()
        #expect(model.state == .sent, "clearing the field after a send is not an edit")

        model.text = "and then stop"
        model.edited()
        #expect(model.state == .editing)
        await model.send()
        #expect(Set(phone.sent.map(\.id)).count == 3)
    }

    /// Reply is offered on a request when the phone named it, or its own push said it takes text,
    /// while it is open; and on a session only when its agent waits at the prompt.
    @Test func replyOfferedOnlyWhereTextAllowed() {
        let store = store()
        store.ingest(request("r_aaaaaaaaaa"), hostId: host)
        store.ingest(request("r_bbbbbbbbbb"), hostId: host)
        store.ingest(request("r_cccccccccc", allowText: true), hostId: host)
        #expect(!store.canReply("r_aaaaaaaaaa"), "nothing from the phone yet")
        #expect(store.canReply("r_cccccccccc"), "its push said so")
        store.apply(context: WatchContext(sessions: [], closedRequestIds: [], at: now, textRequestIds: ["r_aaaaaaaaaa"]))
        #expect(store.canReply("r_aaaaaaaaaa"))
        #expect(!store.canReply("r_bbbbbbbbbb"))
        store.apply(context: WatchContext(
            sessions: [], closedRequestIds: ["r_aaaaaaaaaa", "r_cccccccccc"], at: now.addingTimeInterval(1)
        ))
        #expect(!store.canReply("r_aaaaaaaaaa"), "closed")
        #expect(!store.canReply("r_cccccccccc"), "closed")

        #expect(watchSession("done").acceptsReply)
        #expect(watchSession("idle").acceptsReply)
        #expect(watchSession("failed", reason: "usage_limit").acceptsReply)
        #expect(!watchSession("ended", reason: "usage_limit").acceptsReply, "the helper refuses an ended agent")
        #expect(!watchSession("failed", reason: "exit").acceptsReply)
        #expect(!watchSession("ended", reason: "interrupted").acceptsReply)
        #expect(!watchSession("working").acceptsReply)
        #expect(!watchSession("needs_you").acceptsReply)
        #expect(!watchSession("done", ids: false).acceptsReply, "a phone that predates replies")
    }

    /// A Reply typed on a notification shown on the Watch that does not go stays in that
    /// session's reply screen, which the notice opens, instead of being lost.
    @Test func notificationReplyKeepsTheWords() async {
        let store = store()
        let phone = Phone()
        store.sendReply = { message in
            phone.sent.append(message)
            return phone.outcome
        }
        let target = WatchReplyMessage.Target.prompt(machineId: host, sessionId: session)
        let notice = await store.replyFromNotification(target: target, text: "keep going", notification: "n-1")
        #expect(notice == "Open Togo on your iPhone to send this.")
        #expect(store.replyModel(target).text == "keep going")
        #expect(store.replyModel(target).state == .problem("Open Togo on your iPhone to send this."))
        // The same notification answered again with the same words is the same reply.
        _ = await store.replyFromNotification(target: target, text: "keep going", notification: "n-1")
        #expect(phone.sent.count == 2)
        #expect(phone.sent[0].id == phone.sent[1].id)
        #expect(store.replyModel(target).text == "keep going", "not doubled")

        store.sendReply = { _ in .sent }
        let other = WatchReplyMessage.Target.answer(hostId: host, requestId: "r_aaaaaaaaaa")
        #expect(await store.replyFromNotification(target: other, text: "the second", notification: "n-2") == nil)
        #expect(store.replyModel(other).text == "")
    }

    /// A notification's Reply sends its own words only, never a draft left on the Watch; the
    /// draft stays as it was.
    @Test func notificationReplyNeverSendsOldDraft() async {
        let store = store()
        let phone = Phone()
        phone.outcome = .sent
        store.sendReply = { message in
            phone.sent.append(message)
            return phone.outcome
        }
        let target = WatchReplyMessage.Target.prompt(machineId: host, sessionId: session)
        store.replyModel(target).text = "an old thought"
        #expect(await store.replyFromNotification(target: target, text: "carry on", notification: "n-1") == nil)
        #expect(phone.sent.map(\.text) == ["carry on"])
        #expect(store.replyModel(target).text == "an old thought")
    }

    /// A notification's Reply that does not go becomes the draft only when there is none; an
    /// existing draft is kept and the notice still says why it did not go.
    @Test func failedNotificationReplyDoesNotClobberDraft() async {
        let store = store()
        store.sendReply = { _ in .refused("Not sent: 01 Claude Code is working. Wait until it's done.") }
        let target = WatchReplyMessage.Target.prompt(machineId: host, sessionId: session)
        store.replyModel(target).text = "an old thought"
        let notice = await store.replyFromNotification(target: target, text: "carry on", notification: "n-1")
        #expect(notice == "Not sent: 01 Claude Code is working. Wait until it's done.")
        #expect(store.replyModel(target).text == "an old thought")

        let other = WatchReplyMessage.Target.prompt(machineId: host, sessionId: "s_bbbbbbbbbb")
        _ = await store.replyFromNotification(target: other, text: "carry on", notification: "n-2")
        #expect(store.replyModel(other).text == "carry on", "no draft yet: the words are kept")
    }

    /// Run again on a notification shown on the Watch goes to the phone, which starts the
    /// session over its own connection. A retry of the same tap is the same message; a tap that
    /// does not go gets a notice that says why.
    @Test func notificationRunAgainGoesThroughThePhone() async {
        let store = store()
        let phone = RunAgainPhone()
        store.sendRunAgain = { message in
            phone.sent.append(message)
            return phone.outcome
        }
        #expect(await store.runAgainFromNotification(hostId: host, sessionId: session, notification: "n-1")
            == "Open Togo on your iPhone to run it again.")
        #expect(phone.sent.first?.machineId == host)
        #expect(phone.sent.first?.sessionId == session)
        phone.outcome = .refused("Couldn't run 01 Claude Code again: The agent is still running.")
        #expect(await store.runAgainFromNotification(hostId: host, sessionId: session, notification: "n-1")
            == "Couldn't run 01 Claude Code again: The agent is still running.")
        #expect(phone.sent.count == 2)
        #expect(phone.sent[0].id == phone.sent[1].id, "the same tap")
        phone.outcome = .sent
        #expect(await store.runAgainFromNotification(hostId: host, sessionId: session, notification: "n-2") == nil)
        #expect(phone.sent[2].id != phone.sent[0].id)
    }

    /// The message the phone reads, and the outcome the Watch reads back.
    @Test func replyMessageRoundTrips() {
        let answer = WatchReplyMessage(target: .answer(hostId: host, requestId: "r_abcdefghij"), text: "the second")
        #expect(answer.message["type"] as? String == "reply")
        #expect(WatchReplyMessage(message: answer.message) == answer)
        let prompt = WatchReplyMessage(target: .prompt(machineId: machine, sessionId: session), text: "go")
        #expect(WatchReplyMessage(message: prompt.message) == prompt)
        #expect(WatchReplyMessage(message: ["type": "device", "text": "x"]) == nil)
        for outcome in [ReplyOutcome.sent, .needsPhone, .refused("a"), .failed("b")] {
            #expect(ReplyOutcome(message: outcome.message) == outcome)
        }
        #expect(ReplyOutcome(message: [:]) == .needsPhone)
    }
}
