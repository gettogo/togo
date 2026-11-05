import Foundation
import Synchronization
import Testing
import TogoKit
@testable import Togo

@MainActor struct SignInOnceTests {
    @Test func twoWindowsSigningInShareOneAttempt() async throws {
        let watches = Watches()
        let calls = Mutex(0)
        let now = Samples.now
        let services = LiveServices(
            hostRecords: SyncedRecordStore(prefix: "host.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { now }),
            settingsRecords: SyncedRecordStore(prefix: "settings.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { now }),
            syncSwitch: SyncSwitch(backend: MemoryBackend()),
            dependencies: LiveServices.Dependencies(
                watch: { record in
                    let watch = FakeWatch(record: record)
                    watches.made.withLock { $0[record.id] = watch }
                    return watch
                },
                helpers: NoHelper(),
                connect: { _, _, _, _ in { _, _, _ in throw NoHelper.Unreachable() } },
                signIn: { _, _ in
                    calls.withLock { $0 += 1 }
                    try await Task.sleep(for: .milliseconds(100))
                },
                now: { now }
            )
        )
        await services.start()
        let id = Samples.studio
        try await services.saveHost(HostRecord(
            id: id, profile: HostProfile(id: id, name: "Studio", endpoint: HostEndpoint(host: "studio.local", user: "sam"), auth: .password),
            symbol: "desktop"
        ))
        async let first: Void = services.signIn(hostID: id, prompter: CountingPrompter())
        async let second: Void = services.signIn(hostID: id, prompter: CountingPrompter())
        _ = try await (first, second)
        #expect(calls.withLock { $0 } == 1)
        #expect(watches[id]?.refreshes.withLock { $0 } == 1)
    }
}

@MainActor struct AttachPromptTests {
    private let codePrompt = AuthPrompt.keyboardInteractive(
        name: "", instruction: "", prompts: [KIPrompt(text: "Verification code:", echo: false)]
    )

    private func prompter(_ prompts: AttachPrompts, _ host: HostID = Samples.studio, userStarted: Bool = true) -> AttachPrompter {
        AttachPrompter(
            prompts: prompts, hostID: host, machine: host == Samples.studio ? "Studio" : "Build box",
            address: host == Samples.studio ? "studio.local" : "build.local", userStarted: userStarted
        )
    }

    private func waitForQuestions(_ prompts: AttachPrompts, _ count: Int) async {
        for _ in 0..<1000 where prompts.queue.count != count { await Task.yield() }
    }

    @Test func openingATerminalPutsTheCodeToTheUser() async {
        let prompts = AttachPrompts()
        let prompter = prompter(prompts)
        let attempt = prompter.beginAttempt()
        let reply = Task { await prompter.answer(codePrompt) }
        await waitForQuestions(prompts, 1)
        guard let question = prompts.current else {
            Issue.record("expected a question")
            return
        }
        #expect(question.request.title == "Studio is asking for a code")
        #expect(question.address == "studio.local")
        prompts.answer(question.id, ["123456"])
        guard case .responses(let values)? = await reply.value else {
            Issue.record("expected the code to be passed on")
            return
        }
        #expect(values == ["123456"])
        await prompter.endAttempt(attempt)
        #expect(prompter.askedLastAttempt)
        #expect(prompts.queue.isEmpty)
    }

    /// A server that asks and then hangs up, attempt after attempt, never piles up questions.
    @Test func askThenDropNeverGrowsTheQueue() async {
        let prompts = AttachPrompts()
        let prompter = prompter(prompts)
        for _ in 0..<10 {
            let attempt = prompter.beginAttempt()
            let reply = Task { await prompter.answer(codePrompt) }
            await waitForQuestions(prompts, 1)
            #expect(prompts.queue.count <= 1)
            reply.cancel()
            #expect(await reply.value == nil)
            await prompter.endAttempt(attempt)
            #expect(prompts.queue.isEmpty)
        }
    }

    @Test func closingTheTabRemovesItsQuestion() async {
        let prompts = AttachPrompts()
        let prompter = prompter(prompts)
        _ = prompter.beginAttempt()
        let reply = Task { await prompter.answer(codePrompt) }
        await waitForQuestions(prompts, 1)
        prompts.withdraw(owner: ObjectIdentifier(prompter))
        #expect(await reply.value == nil)
        #expect(prompts.queue.isEmpty)
    }

    @Test func queueIsCappedAndDeclinesTheOldest() async {
        let prompts = AttachPrompts()
        var replies: [Task<AuthAnswer?, Never>] = []
        for count in 1...(AttachPrompts.limit + 1) {
            let prompter = prompter(prompts)
            _ = prompter.beginAttempt()
            let codePrompt = self.codePrompt
            replies.append(Task { await prompter.answer(codePrompt) })
            await waitForQuestions(prompts, min(count, AttachPrompts.limit))
        }
        #expect(prompts.queue.count == AttachPrompts.limit)
        #expect(await replies[0].value == nil)
        prompts.withdrawAll()
        for reply in replies { _ = await reply.value }
    }

    /// Answering one machine's question by its id never reaches another machine's.
    @Test func answerGoesOnlyToTheQuestionShown() async {
        let prompts = AttachPrompts()
        let studio = prompter(prompts, Samples.studio)
        let build = prompter(prompts, Samples.build)
        _ = studio.beginAttempt()
        _ = build.beginAttempt()
        let codePrompt = self.codePrompt
        let studioReply = Task { await studio.answer(codePrompt) }
        await waitForQuestions(prompts, 1)
        let buildReply = Task { await build.answer(codePrompt) }
        await waitForQuestions(prompts, 2)
        let buildQuestion = prompts.queue.first { $0.hostID == Samples.build }!
        prompts.answer(buildQuestion.id, ["222222"])
        prompts.answer(buildQuestion.id, ["333333"])
        guard case .responses(let values)? = await buildReply.value else {
            Issue.record("Build box should get its answer")
            return
        }
        #expect(values == ["222222"])
        #expect(prompts.queue.map(\.hostID) == [Samples.studio])
        prompts.withdraw(hostID: Samples.studio)
        #expect(await studioReply.value == nil)
    }

    @Test func reconnectsAndAutomaticAttachesNeverAsk() async {
        let prompts = AttachPrompts()
        let opened = prompter(prompts)
        let first = opened.beginAttempt()
        await opened.endAttempt(first)
        _ = opened.beginAttempt()
        #expect(await opened.answer(codePrompt) == nil)
        #expect(prompts.queue.isEmpty)

        let automatic = prompter(prompts, userStarted: false)
        let attempt = automatic.beginAttempt()
        #expect(await automatic.answer(codePrompt) == nil)
        await automatic.endAttempt(attempt)
        #expect(automatic.askedLastAttempt)
        #expect(LiveTerminal.phase(.failed("cancelled"), asked: automatic.askedLastAttempt) == .signedOut)
    }

    @Test func laterFailureWithoutAQuestionKeepsItsReason() async {
        let prompts = AttachPrompts()
        let prompter = prompter(prompts, userStarted: false)
        let asking = prompter.beginAttempt()
        _ = await prompter.answer(codePrompt)
        await prompter.endAttempt(asking)
        let quiet = prompter.beginAttempt()
        await prompter.endAttempt(quiet)
        #expect(!prompter.askedLastAttempt)
        #expect(LiveTerminal.phase(.failed("Host key changed"), asked: prompter.askedLastAttempt) == .failed("Host key changed"))
        #expect(ConnectingOverlay.Kind(link: .signedOut, machine: nil, name: "Studio") == .signedOut("Studio"))
    }

    @Test func nothingIsAskedInTheBackground() async {
        let prompts = AttachPrompts()
        let prompter = prompter(prompts)
        _ = prompter.beginAttempt()
        let codePrompt = self.codePrompt
        let reply = Task { await prompter.answer(codePrompt) }
        await waitForQuestions(prompts, 1)
        prompts.isForeground = false
        #expect(await reply.value == nil)
        #expect(await prompter.answer(codePrompt) == nil)
        #expect(prompts.queue.isEmpty)
    }

    @Test func serverTextIsCappedAndCleaned() throws {
        let long = String(repeating: "a", count: 900)
        let request = try #require(SignInRequest(
            .keyboardInteractive(name: "", instruction: "Code\u{202E}evil\u{1B}[31m" + long, prompts: [KIPrompt(text: "Code:", echo: false)]),
            machine: "Studio"
        ))
        #expect(!request.instruction.unicodeScalars.contains { $0 == "\u{202E}" || $0 == "\u{1B}" })
        #expect(request.instruction.count == SignInRequest.maxText + 1)
        let many = (0..<9).map { KIPrompt(text: "Q\($0)", echo: true) }
        #expect(SignInRequest(.keyboardInteractive(name: "", instruction: "", prompts: many), machine: "Studio") == nil)
    }
}
