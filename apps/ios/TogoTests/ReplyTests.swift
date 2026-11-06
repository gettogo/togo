import Foundation
import Testing
import TogoKit
import TogoPush
@testable import Togo

/// A machine the phone cannot reach without asking the user something.
@MainActor private final class UnreachableTools: MachineTools {
    struct NoChannel: Error {}
    var calls = 0

    func call<T: Sendable>(_ host: UUID, _ body: @Sendable (HelperClient) async throws -> T) async throws -> T {
        calls += 1
        throw NoChannel()
    }
}

@MainActor private final class ReplyWatchLink: WatchLink {
    var canReachWatchApp = true
    func updateApplicationContext(_ context: [String: Any]) throws {}
}

@MainActor @Suite(.serialized)
struct ReplyTests {
    private let hostID = "h_mmg42klgyqzwneiskrelxms3j7"
    private let directory = FileManager.default.temporaryDirectory.appending(path: "togo-reply-\(UUID().uuidString)")
    private let tools = FakeMachineTools()
    private let store = AppStore(services: FakeAppServices())

    init() {
        tools.defaultCapabilities = [Capability.approvals, Capability.reply]
        var done = Samples.row(bib: "01", name: "app", status: .done, cwd: "/Users/sam/src/app")
        done.id = "s_abcdefghij"
        store.apply(HostViewSnapshot(
            hostID: Samples.studio, reachability: .online, helper: .present(version: "0.5.0", live: true),
            sessions: [done], requests: [], at: Samples.now
        ))
        PairingStore(directory: directory).put(Pairing(
            hostId: hostID, hostKey: "key", relayURL: URL(string: "https://relay.example")!, machineName: "studio",
            appHostID: Samples.studio, pairedAt: Samples.now, tokenHash: "a1b2c3", liveActivityCommand: false
        ))
    }

    private var tab: TabID { TabID(hostID: Samples.studio, sessionID: "s_abcdefghij") }

    private func sender(_ tools: any MachineTools, quiet: Bool = false) -> ReplySender {
        ReplySender(tools: tools, quiet: quiet, label: store.replyLabel)
    }

    /// An answer typed for a request goes as the request's text decision, cleaned to one line.
    @Test func answerReplyUsesTextDecision() async {
        tools.answer("approval.answer", #"{"state":"answered"}"#)
        let outcome = await sender(tools).send(.answer(host: Samples.studio, requestID: "r_aaaaaaaaaa", text: "use\tthe\r\nsecond\u{1B}[2J one\n"))
        #expect(outcome == .sent)
        #expect(tools.requests == [FakeMachineTools.Request(method: "approval.answer", params: [
            "requestId": .string("r_aaaaaaaaaa"), "decision": .string("text:use the second[2J one"),
        ])])
    }

    /// A reply to a done session is typed at its prompt and submitted, with line breaks kept and
    /// tabs and lone carriage returns turned to spaces, and escapes gone.
    @Test func doneReplyUsesSendText() async {
        let reply = Reply.prompt(host: Samples.studio, session: "s_abcdefghij", text: "Now\r\nrun\tthe\rtests\u{1B}\u{9B}\n\n")
        #expect(await store.deliverReply(reply, via: sender(tools)) == .sent)
        #expect(tools.requests == [FakeMachineTools.Request(method: "session.sendText", params: [
            "id": .string("s_abcdefghij"), "text": .string("Now\nrun the tests"), "submit": .bool(true),
        ])])
        #expect(store.notice == Notice(text: "Sent to 01 Claude Code", isError: false))
        #expect(store.replyDrafts.isEmpty)

        let empty = await store.deliverReply(.prompt(host: Samples.studio, session: "s_abcdefghij", text: " \t\r\n "), via: sender(tools))
        #expect(empty == .refused("There's nothing to send."))
        #expect(tools.requests.count == 1, "nothing typed for an empty reply")
    }

    /// The agent ended, or an approval appeared, before the reply arrived: nothing is typed, the
    /// app says why in words, and the words wait in the session's compose bar.
    @Test func refusedReplyKeepsDraft() async {
        tools.fail("session.sendText", code: RPCErrorCode.conflict, data: #"{"reason":"ended"}"#)
        let raw = Reply.prompt(host: Samples.studio, session: "s_abcdefghij", text: "keep\u{1B}[0m going")
        #expect(await store.deliverReply(raw, via: sender(tools)) == .refused("Not sent: 01 Claude Code has ended."))
        #expect(store.takeReplyDraft(tab) == "keep[0m going", "the draft never holds control characters")
        let reply = Reply.prompt(host: Samples.studio, session: "s_abcdefghij", text: "keep going")
        #expect(await store.deliverReply(reply, via: sender(tools)) == .refused("Not sent: 01 Claude Code has ended."))
        #expect(store.notice == Notice(text: "Not sent: 01 Claude Code has ended.", isError: true))
        #expect(store.replyDrafts[tab] == "keep going")
        #expect(store.composing == tab)
        #expect(store.takeReplyDraft(tab) == "keep going")
        #expect(store.replyDrafts[tab] == nil)

        tools.fail("session.sendText", code: RPCErrorCode.conflict, data: #"{"reason":"needs_you"}"#)
        #expect(await store.deliverReply(reply, via: sender(tools))
            == .refused("Not sent: 01 Claude Code is asking something. Open it to answer."))
        #expect(store.replyDrafts[tab] == "keep going")
    }

    private func bridge(_ tools: any MachineTools, defaults: UserDefaults? = nil) -> WatchBridge {
        let bridge = WatchBridge(
            link: ReplyWatchLink(), defaults: defaults ?? UserDefaults(suiteName: "reply-\(UUID().uuidString)")!
        )
        bridge.onReply = WatchReplyGate.handler(
            store: store, pairings: PairingStore(directory: directory),
            pending: PendingStore(directory: directory, clock: { Samples.now }), sender: sender(tools, quiet: true)
        )
        return bridge
    }

    private func textRequest() {
        var card = Samples.request(id: "r_aaaaaaaaaa", bib: "01", title: "Which file?")
        card.sessionID = "s_abcdefghij"
        card.allowText = true
        store.controlCenter.requests = [card]
    }

    /// From the Watch, the phone uses only a channel it can open without asking; otherwise the
    /// Watch hears it needs the phone and keeps the text.
    @Test func watchReplyNeedsPhoneWhenChannelClosed() async {
        textRequest()
        let closed = UnreachableTools()
        let bridge = bridge(closed)
        let prompt = WatchReplyMessage(
            target: .prompt(machineId: Samples.studio.uuidString, sessionId: "s_abcdefghij"), text: "go on"
        )
        #expect(ReplyOutcome(message: await bridge.handle(message: prompt.message)) == .needsPhone)
        #expect(closed.calls == 1)
        let answer = WatchReplyMessage(target: .answer(hostId: hostID, requestId: "r_aaaaaaaaaa"), text: "yes")
        #expect(Reply(answer, pairings: PairingStore(directory: directory))
            == .answer(host: Samples.studio, requestID: "r_aaaaaaaaaa", text: "yes"))
        #expect(ReplyOutcome(message: await bridge.handle(message: answer.message)) == .needsPhone)
        #expect(ReplyOutcome(message: await bridge.handle(message: ["type": "other"])) != .sent)
        #expect(closed.calls == 2, "an unreadable message reaches no machine")

        // Over an open channel the same message goes.
        #expect(ReplyOutcome(message: await self.bridge(tools).handle(message: prompt.message)) == .sent)
    }

    /// The Watch keeps a reply's id across retries: a retry while the first attempt runs, or
    /// after it went, types nothing more. Edited words get a new id and go again.
    @Test func watchRetrySendsOnce() async {
        let defaults = UserDefaults(suiteName: "reply-\(UUID().uuidString)")!
        let bridge = bridge(tools, defaults: defaults)
        let first = WatchReplyMessage(
            id: "A1B2C3D4-0000-4000-8000-000000000001",
            target: .prompt(machineId: Samples.studio.uuidString, sessionId: "s_abcdefghij"), text: "go on"
        )
        async let a = bridge.reply(to: first)
        async let b = bridge.reply(to: first)
        #expect(await [a, b] == [.sent, .sent])
        #expect(await bridge.reply(to: first) == .sent)
        #expect(tools.requests.count == 1, "typed once")
        // A relaunched phone remembers what went.
        #expect(await self.bridge(tools, defaults: defaults).reply(to: first) == .sent)
        #expect(tools.requests.count == 1)

        var edited = first
        edited.id = "A1B2C3D4-0000-4000-8000-000000000002"
        edited.text = "go on, slowly"
        #expect(await bridge.reply(to: edited) == .sent)
        #expect(tools.requests.count == 2)
    }

    /// Whatever the Watch sends is checked before anything is typed: the machine must be in
    /// Togo, the session on it waiting for words, the request open and taking text, the ids
    /// well formed and the target single.
    @Test func watchReplyRejectsUnknownTargets() async {
        let bridge = bridge(tools)
        func send(_ message: [String: Any]) async -> ReplyOutcome {
            ReplyOutcome(message: await bridge.handle(message: message))
        }
        let unknown = WatchReplyMessage(
            target: .prompt(machineId: UUID().uuidString, sessionId: "s_abcdefghij"), text: "hi"
        )
        #expect(await send(unknown.message) == .refused("Not sent: this machine is no longer in Togo."))
        let gone = WatchReplyMessage(
            target: .prompt(machineId: Samples.studio.uuidString, sessionId: "s_zzzzzzzzzz"), text: "hi"
        )
        #expect(await send(gone.message) == .refused("Not sent: the agent has ended."))

        var working = Samples.row(bib: "02", name: "api", status: .working, cwd: "~")
        working.id = "s_bbbbbbbbbb"
        store.controlCenter.machines[0].sessions.append(working)
        let busy = WatchReplyMessage(
            target: .prompt(machineId: Samples.studio.uuidString, sessionId: "s_bbbbbbbbbb"), text: "hi"
        )
        #expect(await send(busy.message) == .refused("Not sent: 02 Claude Code is working. Wait until it's done."))

        let buttonsOnly = WatchReplyMessage(target: .answer(hostId: hostID, requestId: "r_cccccccccc"), text: "hi")
        #expect(await send(buttonsOnly.message) == .refused("Not sent: the agent is no longer asking."))

        var both = unknown.message
        both["hostId"] = hostID
        both["requestId"] = "r_aaaaaaaaaa"
        for bad in [
            both,
            ["type": "reply", "text": "hi", "machineId": Samples.studio.uuidString, "sessionId": "s_1"],
            ["type": "reply", "text": "hi", "machineId": Samples.studio.uuidString, "sessionId": "u_00"],
            ["type": "reply", "text": "hi", "hostId": "h_studio", "requestId": "r_aaaaaaaaaa"],
            ["type": "reply", "text": 4, "machineId": Samples.studio.uuidString, "sessionId": "s_abcdefghij"],
            ["type": "reply", "id": "../../x", "text": "hi", "machineId": Samples.studio.uuidString, "sessionId": "s_abcdefghij"],
            ["type": "reply", "text": String(repeating: "a", count: 40_000), "machineId": Samples.studio.uuidString,
             "sessionId": "s_abcdefghij"],
        ] as [[String: Any]] {
            #expect(await send(bad) == .refused("Not sent: the message could not be read."))
        }
        #expect(tools.requests.isEmpty, "nothing reached the machine")
    }

    /// A request the phone heard of only by push is answered only on the machine that sent it.
    @Test func watchAnswerChecksTheRequestsMachine() async {
        let pending = PendingStore(directory: directory, clock: { Samples.now })
        pending.upsert(PendingRequest(
            requestId: "r_bbbbbbbbbb", hostId: "h_aaaaaaaaaaaaaaaaaaaaaaaaaa", sessionId: nil, bib: "01", agent: "claude",
            host: "laptop", title: "Which file?", subject: nil, options: [], receivedAt: Samples.now, answered: nil,
            allowText: true
        ))
        pending.upsert(PendingRequest(
            requestId: "r_cccccccccc", hostId: hostID, sessionId: nil, bib: "01", agent: "claude",
            host: "studio", title: "Which file?", subject: nil, options: [], receivedAt: Samples.now, answered: nil,
            allowText: true
        ))
        tools.answer("approval.answer", #"{"state":"answered"}"#)
        let bridge = bridge(tools)
        let elsewhere = WatchReplyMessage(target: .answer(hostId: hostID, requestId: "r_bbbbbbbbbb"), text: "the second")
        #expect(await bridge.reply(to: elsewhere) == .refused("Not sent: the agent is no longer asking."))
        #expect(tools.requests.isEmpty)
        let here = WatchReplyMessage(target: .answer(hostId: hostID, requestId: "r_cccccccccc"), text: "the second")
        #expect(await bridge.reply(to: here) == .sent)
    }

    /// An error stop with no exit code leaves the agent at its prompt, so a Watch reply goes;
    /// once the code is in, the agent is gone and the reply is refused.
    @Test func errorStopOffersReplyNotRunAgain() async {
        tools.answer("session.sendText", #"{}"#)
        let bridge = bridge(tools)
        var waiting = Samples.row(bib: "02", name: "api", status: .failed, cwd: "~")
        waiting.id = "s_bbbbbbbbbb"
        waiting.exit = SessionExit(code: nil, reason: .error)
        var gone = waiting
        gone.id = "s_cccccccccc"
        gone.exit = SessionExit(code: 1, reason: .error)
        store.controlCenter.machines[0].sessions += [waiting, gone]
        let toWaiting = WatchReplyMessage(
            target: .prompt(machineId: Samples.studio.uuidString, sessionId: "s_bbbbbbbbbb"), text: "try again"
        )
        #expect(await bridge.reply(to: toWaiting) == .sent)
        let toGone = WatchReplyMessage(
            target: .prompt(machineId: Samples.studio.uuidString, sessionId: "s_cccccccccc"), text: "hi"
        )
        #expect(await bridge.reply(to: toGone) == .refused("Not sent: 02 Claude Code has ended."))
        #expect(tools.requests.map(\.method) == ["session.sendText"])
    }

    /// A reply from a notification goes over SSH only: the relay carries approve, deny and
    /// option answers and never free text.
    @Test func replyNeverUsesRelay() async {
        let relay = FakeRelay()
        let pending = PendingStore(directory: directory, clock: { Samples.now })
        var sent: [(Reply, String?)] = []
        let handler = NotificationActionHandler(dependencies: .init(
            pairings: PairingStore(directory: directory), pending: pending, relay: relay, live: nil,
            notifications: FakeNotifications(),
            reply: { reply, session in sent.append((reply, session)) }
        ))
        let link = await handler.handle(
            actionIdentifier: NotificationActionID.reply,
            userInfo: ["togo": ["hostId": hostID, "sessionId": "s_abcdefghij"]],
            target: "togo://session/01?host=studio", text: "carry on"
        )
        #expect(link == .session(bib: "01", host: "studio"))
        #expect(sent.map(\.0) == [.prompt(host: Samples.studio, session: "s_abcdefghij", text: "carry on")])
        #expect(sent.map(\.1) == ["s_abcdefghij"])

        await handler.handle(
            actionIdentifier: NotificationActionID.reply,
            userInfo: ["togo": ["requestId": "r_aaaaaaaaaa", "hostId": hostID, "sessionId": "s_abcdefghij"]],
            target: nil, text: "the second one"
        )
        #expect(sent.last?.0 == .answer(host: Samples.studio, requestID: "r_aaaaaaaaaa", text: "the second one"))
        #expect(relay.sent.withLock { $0 }.isEmpty)
        #expect(pending.request("r_aaaaaaaaaa") == nil, "a reply claims nothing the relay path would")

        // And the whole way through the sender, still nothing reaches the relay.
        _ = await store.deliverReply(sent[0].0, via: sender(tools))
        #expect(tools.requests.map(\.method) == ["session.sendText"])
        #expect(relay.sent.withLock { $0 }.isEmpty)
    }
}
