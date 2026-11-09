import Foundation
import Testing
import UserNotifications
import TogoPush

struct LiveCardTests {
    func card(
        status: String = "needs_you", waiting: Int = 1, requestId: String? = "r_aaaaaaaaaa", command: String? = nil,
        context: LiveContext? = nil, machine: String? = nil, stale: Bool = false, agent: String = "claude",
        showsCommand: Bool = true
    ) -> LiveCard {
        LiveCard(
            agent: agent, status: status, waiting: waiting, requestId: requestId, command: command, context: context,
            machine: machine, stale: stale, showsCommand: showsCommand
        )
    }

    /// The command comes sealed in the context; the machine's "show command text" choice
    /// decides whether it shows. Files and URLs are not command text and always show.
    /// The widget reads the app's per-machine choice from the shared defaults.
    @Test func commandChoiceIsReadFromTheAppGroup() throws {
        let defaults = try #require(UserDefaults(suiteName: "live-\(UUID().uuidString)"))
        let machine = UUID()
        #expect(SharedSettings.readLiveActivityCommandMachines(defaults).isEmpty)
        defaults.set(try JSONEncoder().encode([machine]), forKey: SharedSettings.liveActivityCommandMachines)
        #expect(SharedSettings.readLiveActivityCommandMachines(defaults) == [machine])
    }

    @Test func sealedCommandFollowsTheChoice() {
        let command = LiveContext(ask: "Run it?", subject: PushSubject(kind: "command", text: "rm -rf ./build"))
        #expect(card(context: command).detail == .command("rm -rf ./build"))
        let hidden = card(context: command, showsCommand: false)
        #expect(hidden.detail == nil)
        #expect(hidden.ask == "Run it?")
        let file = LiveContext(subject: PushSubject(kind: "file", text: "~/a.go"))
        #expect(card(context: file, showsCommand: false).detail == .file("~/a.go"))
    }

    @Test func needsYouShowsSessionRequestAndCount() {
        let c = card(waiting: 3, context: LiveContext(
            title: "Fix the login redirect", ask: "Run this command?",
            subject: PushSubject(kind: "command", text: "npm test -- --watch=false"), answer: .approveDeny
        ), machine: "Studio")
        #expect(c.phase == .needsYou)
        #expect(c.title == "Fix the login redirect")
        #expect(c.agentName == "Claude Code")
        #expect(c.machine == "Studio")
        #expect(c.ask == "Run this command?")
        #expect(c.detail == .command("npm test -- --watch=false"))
        #expect(c.detail?.isMachineText == true)
        #expect(c.more == 2)
        #expect(c.moreText == "+2 more")
        #expect(c.answer == .approveDeny)
    }

    @Test func askThatOnlyNamesTheToolGivesWayToTheDetail() {
        let c = card(context: LiveContext(ask: "Bash", subject: PushSubject(kind: "command", text: "make")))
        #expect(c.ask == nil)
        #expect(c.detail == .command("make"))
    }

    @Test func questionWithoutSubjectShowsTheQuestionAlone() {
        let c = card(context: LiveContext(ask: "Which color do you prefer?\nRed or blue"))
        #expect(c.ask == "Which color do you prefer?\nRed or blue")
        #expect(c.detail == nil)
        #expect(c.answer == nil)
    }

    @Test func urlReadsAsHostAndPath() {
        let c = card(context: LiveContext(subject: PushSubject(kind: "url", text: "https://example.com/docs/a?q=1")))
        #expect(c.detail == .url("example.com/docs/a"))
    }

    @Test func optedInCommandWhenNoContext() {
        let c = card(command: "rm -rf ./build")
        #expect(c.detail == .command("rm -rf ./build"))
        #expect(c.title == "Claude Code")
        #expect(c.answer == nil)
    }

    @Test func workingShowsTitleAndStepOnly() {
        let c = card(status: "working", waiting: 0, requestId: nil, context: LiveContext(
            title: "Refactor the parser", step: "Bash", ask: "stale", answer: .approveDeny
        ))
        #expect(c.phase == .working)
        #expect(c.title == "Refactor the parser")
        #expect(c.step == "Bash")
        #expect(c.ask == nil && c.detail == nil && c.answer == nil && c.moreText == nil)
    }

    @Test func staleOffersNothingToAnswer() {
        let c = card(waiting: 2, context: LiveContext(ask: "Run?", answer: .approveDeny), stale: true)
        #expect(c.phase == .other)
        #expect(c.answer == nil && c.ask == nil && c.more == 0)
    }

    @Test func boxDrawingAndCursorsNeverShow() {
        let c = card(context: LiveContext(
            title: "╭ title ╮", subject: PushSubject(kind: "text", text: "❯ │ pick one │")
        ))
        #expect(c.title == "title")
        #expect(c.detail == .text("pick one"))
    }

    @Test func customProgramIsNeverCalledCustom() {
        #expect(AgentName.display("custom") == "")
        #expect(LiveAgentMark.of("custom") == .symbol("terminal"))
        #expect(card(agent: "custom").title == LiveCard.untitled)
        #expect(LiveAgentMark.of("claude") == .logo("agent.claude"))
        #expect(LiveAgentMark.of("codex") == .logo("agent.codex"))
        #expect(LiveAgentMark.of("aider") == .monogram("Ad"))
        #expect(LiveAgentMark.of("goose") == .logo("agent.goose"))
        #expect(LiveAgentMark.of("omp") == .logo("agent.omp"))
        #expect(LiveAgentMark.of("qwen") == .monogram("Qw"))
    }

    @Test func contextOpensOnlyForThisDeviceAndMachine() throws {
        let keys = InMemoryDeviceKeys()
        let host = "h_mmg42klgyqzwneiskrelxms3j7"
        let context = LiveContext(title: "T", step: "Edit", ask: "Q?", subject: PushSubject(kind: "file", text: "~/a.go"))
        let plain = try JSONEncoder().encode(context)
        let aad = Data("\(host)|\(try keys.deviceID())".utf8)
        let envelope = try Seal.seal(to: try keys.agreementKey().publicKey, plain, label: Labels.live, aad: aad)
        #expect(LiveContext.open(envelope, hostId: host, keys: keys) == context)
        #expect(LiveContext.open(envelope, hostId: "h_other", keys: keys) == nil)
        let asPush = try Seal.seal(to: try keys.agreementKey().publicKey, plain, label: Labels.push, aad: aad)
        #expect(LiveContext.open(asPush, hostId: host, keys: keys) == nil)
    }

    @Test func unknownAnswerWordReadsAsNoButtons() throws {
        let raw = Data(#"{"ask":"Q?","answer":"approve_all"}"#.utf8)
        let c = try JSONDecoder().decode(LiveContext.self, from: raw)
        #expect(c.ask == "Q?" && c.answer == nil)
    }
}

struct NotificationContextTests {
    func needsYou(options: [PushOption], subject: PushSubject? = nil, sessionName: String? = nil) -> PushPayload {
        PushPayload(
            kind: .needsYou, sessionId: "s_abcdefghij", bib: "05", agent: "kimi", host: "studio",
            title: "Which color?", body: "", requestId: "r_aaaaaaaaaa", options: options, subject: subject,
            ts: 1_793_707_200_000, sessionName: sessionName
        )
    }

    @Test func titleNamesAgentAndSession() throws {
        let text = try #require(NotificationTextBuilder.build(needsYou(options: [], sessionName: "Fix login")))
        #expect(text.title == "05 Kimi Code · Fix login")
        let plain = try #require(NotificationTextBuilder.build(needsYou(options: [], sessionName: "Kimi Code")))
        #expect(plain.title == "05 Kimi Code")
    }

    @Test func unanswerableQuestionSaysOpenToAnswer() throws {
        let text = try #require(NotificationTextBuilder.build(needsYou(options: [])))
        #expect(text.category == NotificationCategoryID.info)
        #expect(text.body == NotificationTextBuilder.openToAnswer)
    }

    @Test func singleQuestionGetsItsOwnOptions() throws {
        let options = [PushOption(n: 1, label: "Red", role: "other"), PushOption(n: 2, label: "Blue", role: "other")]
        let text = try #require(NotificationTextBuilder.build(needsYou(options: options)))
        #expect(text.category == NotificationCategoryID.choice("r_aaaaaaaaaa"))
        let pending = PendingRequest(
            requestId: "r_aaaaaaaaaa", hostId: "h_x", sessionId: nil, bib: nil, agent: nil, host: nil, title: "Which color?",
            subject: nil, options: options, receivedAt: Date(), answered: nil
        )
        let categories = NotificationCategorySet.choices([pending], approveWithoutUnlock: false)
        let category = try #require(categories.first)
        #expect(category.identifier == "togo.choice.r_aaaaaaaaaa")
        #expect(category.actions.map(\.identifier) == ["togo.option.1", "togo.option.2"])
        #expect(category.actions.map(\.title) == ["Red", "Blue"])
        #expect(category.actions.allSatisfy { $0.options.contains(.authenticationRequired) })
        var answered = pending
        answered.answered = .approved
        #expect(NotificationCategorySet.choices([answered], approveWithoutUnlock: false).isEmpty)
    }

    @Test func approveAlwaysOrTooManyOptionsGetNoChoiceButtons() {
        #expect(!NotificationCategorySet.offersChoices([PushOption(n: 1, label: "Always", role: "approve_always")]))
        #expect(!NotificationCategorySet.offersChoices((1...5).map { PushOption(n: $0, label: "\($0)", role: "other") }))
        #expect(!NotificationCategorySet.offersChoices([]))
    }

    @Test func optionActionIdentifiers() {
        #expect(NotificationActionID.optionNumber("togo.option.3") == 3)
        #expect(NotificationActionID.optionNumber("togo.option.0") == nil)
        #expect(NotificationActionID.optionNumber("togo.option.03") == nil)
        #expect(NotificationActionID.optionNumber("togo.approve") == nil)
    }
}
