import Foundation
import Testing
import TogoKit
@testable import Togo

/// The demo machine App Review explores: the recorded session plays into the real chat as the
/// person sends and answers, the roster, review and terminal follow it, and nothing it does
/// reaches a machine, the keychain, a synced store or the person's own defaults.
@MainActor
struct DemoVisitTests {
    private let subject = HarnessSubject(hostID: DemoMachine.id, sessionID: DemoScript.sessionID, capabilities: DemoServices.capabilities)
    private let review = ReviewSubject(
        hostID: DemoMachine.id, sessionID: DemoScript.sessionID, bib: "01", title: "status", cwd: "~/status",
        managed: true, capabilities: DemoServices.capabilities
    )

    /// Waits up to `seconds` for `condition`, checking every few milliseconds.
    private func until(_ seconds: Double = 15, _ condition: () -> Bool) async -> Bool {
        let end = Date().addingTimeInterval(seconds)
        while !condition() {
            guard Date() < end else { return false }
            try? await Task.sleep(for: .milliseconds(20))
        }
        return true
    }

    private func row(_ visit: DemoVisit) -> SessionRow? {
        visit.services.snapshot().sessions.first { $0.id == DemoScript.sessionID }
    }

    @Test func theScriptWaitsThenPlaysTheTurnThroughItsThreeRequests() async throws {
        let visit = DemoVisit(pace: 0.01)
        let script = try #require(visit.script)
        let harness = try #require(visit.harness)
        let reviews = try #require(visit.review)
        #expect(script.prompt.hasPrefix("Add an incident history timeline"))
        #expect(script.phase == .ready)
        #expect(row(visit)?.status == .idle)
        #expect(try await reviews.status(review).files.isEmpty)
        #expect(try await harness.describe(subject).adapter?.name == "OpenCode")

        var feed = harness.feed(subject, after: 0).makeAsyncIterator()
        guard case .opened? = try await feed.next() else { Issue.record("the feed opens first"); return }

        // The prompt waits in the composer of the session's tab until it is sent.
        let tab = TabID(hostID: DemoMachine.id, sessionID: DemoScript.sessionID)
        visit.tabsOpened([tab])
        #expect(visit.store.pendingInput[tab] == script.prompt)

        let sent = try await harness.send(subject, text: script.prompt, mode: .now, images: [])
        guard case .user(let user) = sent.body else { Issue.record("send answers with the prompt"); return }
        #expect(user.text == script.prompt)
        #expect(script.phase == .working)

        for answer in 1...3 {
            #expect(await until { if case .waiting = script.phase { true } else { false } }, "request \(answer) opens")
            #expect(row(visit)?.status == .needsYou)
            #expect(script.items.contains { if case .question = $0.body { true } else { false } })
            // The permission is also a request, as the helper offers it: Allow once and Deny.
            #expect(await until { !visit.services.snapshot().requests.isEmpty })
            let card = try #require(visit.services.snapshot().requests.first)
            #expect(card.title.hasPrefix("Allow shell"))
            #expect(card.agentName == "OpenCode")
            #expect(card.options.map(\.role) == [.approve, .deny])
            if answer == 2 {
                // Answered from the sessions list, as from the Lock Screen.
                try await visit.services.answer(hostID: DemoMachine.id, requestID: card.id, decision: .option(1))
            } else {
                _ = try await harness.act(subject, action: "answer", target: nil, args: ["answers": .object(["0": .array([.string("once")])])])
            }
            #expect(script.phase == .working)
            #expect(await until { visit.services.snapshot().requests.isEmpty }, "request \(answer) closes")
        }
        #expect(await until { script.phase == .done })
        #expect(row(visit)?.status == .done)

        // The seven files the session wrote, as git reports them.
        let status = try await reviews.status(review)
        #expect(status.files.count == 7)
        #expect(status.files.contains { $0.path == "src/render.ts" && $0.worktree == "M" })
        #expect(status.files.contains { $0.path == "src/incidents.ts" && $0.index == "?" })
        let diff = try await reviews.diff(review, .worktree(path: "src/render.ts"))
        #expect(diff.diff.hasPrefix("diff --git a/src/render.ts"))

        // The recording has nothing more: a second prompt, an answer or a stop is refused here.
        await #expect(throws: DemoServices.ReadOnly.self) {
            try await harness.send(subject, text: "And a dark mode", mode: .now, images: [])
        }
        await #expect(throws: DemoServices.ReadOnly.self) {
            try await harness.act(subject, action: "answer", target: nil, args: nil)
        }
        await #expect(throws: DemoServices.ReadOnly.self) { try await harness.interrupt(subject) }
        visit.end()
    }

    @Test func aReopenedChatStartsWhereTheSessionIs() async throws {
        let visit = DemoVisit(pace: 0.01)
        let script = try #require(visit.script)
        let harness = try #require(visit.harness)
        var first = harness.feed(subject, after: 0).makeAsyncIterator()
        _ = try await first.next()
        _ = try await harness.send(subject, text: script.prompt, mode: .now, images: [])
        #expect(await until { if case .waiting = script.phase { true } else { false } })

        var second = harness.feed(subject, after: 0).makeAsyncIterator()
        guard case .opened(let opened)? = try await second.next() else { Issue.record("the feed opens again"); return }
        #expect(opened.items.count == script.items.count)
        #expect(opened.mission.summary.needsYou == 1)
        visit.end()
    }

    @Test func nothingReachesAMachineOrIsSaved() async throws {
        let visit = DemoVisit(pace: 0.01)
        let services = visit.services
        #expect(services.hosts.map(\.address) == [""])
        #expect(services.hostRecord(DemoMachine.id) == nil)
        let machine = DemoMachine.id
        let record = HostRecord(
            id: machine, profile: HostProfile(
                id: machine, name: "Demo", endpoint: HostEndpoint(host: "demo", port: 22, user: "jamie"), auth: .password
            ), symbol: "laptop", helperPath: nil, noHelper: true
        )
        await #expect(throws: DemoServices.ReadOnly.self) { try await services.saveHost(record) }
        await #expect(throws: DemoServices.ReadOnly.self) { try await services.removeHost(machine) }
        await #expect(throws: DemoServices.ReadOnly.self) {
            try await services.create(hostID: machine, params: CreateSessionParams(agent: "claude", cwd: "~"))
        }
        await #expect(throws: DemoServices.ReadOnly.self) { try await services.kill(hostID: machine, sessionID: DemoScript.sessionID) }
        await #expect(throws: DemoServices.ReadOnly.self) {
            try await services.answer(hostID: machine, requestID: "rq_unknown", decision: .option(1))
        }
        await #expect(throws: DemoServices.ReadOnly.self) {
            try await services.upload(hostID: machine, sessionID: DemoScript.sessionID, name: "a.png", data: Data())
        }
        await #expect(throws: DemoServices.ReadOnly.self) {
            _ = try await visit.tools.call(machine) { _ in true }
        }

        // Renaming and muting change only the visit's own list.
        try await services.rename(hostID: machine, sessionID: DemoServices.shellID, name: "scratch")
        try await services.setMute(hostID: machine, sessionID: DemoScript.sessionID, muted: true)
        let rows = services.snapshot().sessions
        #expect(rows.first { $0.id == DemoServices.shellID }?.name == "scratch")
        #expect(rows.first { $0.id == DemoScript.sessionID }?.muted == true)
        #expect(DemoVisit(pace: 0.01).services.snapshot().sessions.allSatisfy { !$0.muted && $0.name != "scratch" })
        #expect(services.attempts == [.saveHost, .removeHost, .create, .kill, .answer, .upload, .rename, .setMute])

        // A port's preview never opens a connection.
        let reviews = try #require(visit.review)
        let port = try #require(try await reviews.ports(review).first)
        await #expect(throws: DemoReviewServices.Unavailable.self) { try await reviews.forward(review, port: port) }

        // Adding a machine never browses, signs in or makes a key.
        let onboarding = DemoOnboardingServices()
        var found = 0
        for await _ in onboarding.discover() { found += 1 }
        #expect(found == 0)
        await #expect(throws: DemoServices.ReadOnly.self) { try await onboarding.deviceKey() }

        // What the composer remembers stays in the visit's own defaults, emptied as it ends.
        let harness = try #require(visit.harness)
        let line = "demo line \(UUID().uuidString)"
        harness.composerMemory.record(line, scope: "opencode")
        #expect(harness.composerMemory.history("opencode").first == line)
        #expect(!CommandMemory.shared.history("opencode").contains(line))
        visit.end()
        #expect(UserDefaults(suiteName: DemoVisit.defaultsSuite)?.dictionaryRepresentation().keys.contains { $0.hasPrefix("demo.") } == false)
    }

    @Test func theReviewStagesAndCommitsInMemory() async throws {
        let project = try #require(DemoProject.load())
        let written = Set(project.status.map(\.path))
        let reviews = DemoReviewServices(project: project) { written }
        let status = try await reviews.status(review)
        #expect(status.files.count == 7)

        let staged = try await reviews.stage(review, paths: ["src/app.ts", "src/incidents.ts"], seen: [:])
        #expect(staged.files.first { $0.path == "src/app.ts" }.map { $0.index + $0.worktree } == "M.")
        #expect(staged.files.first { $0.path == "src/incidents.ts" }.map { $0.index + $0.worktree } == "A.")
        let added = try await reviews.diff(review, .staged(path: "src/incidents.ts"))
        #expect(added.diff.contains("new file mode"))

        let made = try await reviews.commit(review, message: "Add incidents", amend: false, head: nil)
        #expect(made.hash.count == 40)
        let log = try await reviews.log(review, ref: nil, limit: 10)
        #expect(log.first?.subject == "Add incidents")
        #expect(log.count == project.commits.count + 1)
        #expect(log.allSatisfy { $0.author == "Jamie" })
        #expect(try await reviews.status(review).files.count == 5)
        await #expect(throws: DemoServices.ReadOnly.self) {
            try await reviews.commit(review, message: "again", amend: true, head: made.hash)
        }

        // The tree before the session has none of its new files.
        let before = DemoReviewServices(project: project) { [] }
        let src = try await before.listDir(review, path: "src", ref: nil, base: .session)
        #expect(!src.entries.contains { $0.name == "incidents.ts" })
        #expect(src.entries.contains { $0.name == "render.ts" })
        let read = try await before.read(review, path: "src/render.ts", ref: nil, base: .session)
        #expect(read.content == project.head["src/render.ts"])
    }

    @Test func theFixturesNameNoOneReal() throws {
        let project = try #require(DemoProject.load())
        let texts = Array(project.head.values) + Array(project.work.values) + project.commits.flatMap { $0.diffs.values }
        #expect(project.root == "/Users/jamie/status")
        for text in texts {
            #expect(!text.contains("/Users/") || text.contains("/Users/jamie"))
        }
        let url = try #require(Bundle.main.url(forResource: DemoScript.resource, withExtension: "jsonl"))
        let session = try String(contentsOf: url, encoding: .utf8)
        #expect(!session.contains("/Users/"))
        #expect(!session.lowercased().contains("caveman"))
    }

    /// The website's captures (`-ui-replay replay-status`, `-ui-demo-api`) play and review the
    /// same copy of the session the demo machine does.
    @Test func theCapturesShareTheDemosCopy() throws {
        let project = try #require(DemoProject.load())
        let replay = try #require(ReplayScript.load("replay-status"))
        #expect(replay.chapters.count == (try #require(ReplayScript.load(DemoScript.resource))).chapters.count)
        #expect(Bundle.main.url(forResource: "replay-status", withExtension: "jsonl") == nil)
        let status = DemoAPIFixture.status
        #expect(status.root == DemoAPIFixture.root)
        #expect(Set(status.files.map(\.path)) == Set(project.status.map(\.path)))
        #expect(status.files.filter { $0.worktree == "M" }.count == 4)
        #expect(DemoAPIFixture.diffs == project.diffs)
        #expect(DemoAPIFixture.reads.keys.sorted() == ["data/incidents.json", "src/incidents.ts", "test/incidents.test.ts"])
        #expect(DemoAPIFixture.reads["src/incidents.ts"]?.content == project.work["src/incidents.ts"])
    }

    /// The recording names its own agent, and its permissions reach the sessions list, the
    /// Live Activity and the Watch as approvals, as the helper offers them.
    @Test func theReplayIsTheOpenCodeSessionTheHelperWouldSend() async throws {
        let script = try #require(ReplayScript.load(DemoScript.resource))
        #expect(script.agent == ReplayScript.Agent(id: "opencode", name: "OpenCode"))
        #expect(try #require(ReplayScript.load("replay-demo")).agent?.id == "claude")

        let replay = ReplayHarnessServices(script: try ReplayScript(
            jsonl: String(contentsOf: try #require(Bundle.main.url(forResource: DemoScript.resource, withExtension: "jsonl")), encoding: .utf8),
            longestGap: 0.01
        ))
        var opened: [Approval] = []
        var closed: [String] = []
        replay.onApproval = { change, _ in
            switch change {
            case .open(let approval): opened.append(approval)
            case .closed(let id): closed.append(id)
            }
        }
        var feed = replay.feed(subject, after: 0).makeAsyncIterator()
        _ = try await feed.next()
        _ = try await replay.send(subject, text: "", mode: .now, images: [])
        #expect(await until { opened.count == 1 })
        let approval = try #require(opened.first)
        #expect(approval.source == .ask)
        #expect(approval.agent == "opencode")
        #expect(approval.title == "Allow shell: npm run typecheck, npm test")
        #expect(approval.options.map(\.label) == ["Allow once", "Deny"])
        // Approved from the Lock Screen: the recorded answer plays and the request closes.
        replay.trigger("approval.answer")
        #expect(await until { closed == [approval.requestId] })
    }

    @Test func theCapturesTerminalIsTheStatusSession() {
        let text = Samples.statusTerminalText
        #expect(text.contains("OpenCode"))
        #expect(text.contains("16/16 tests pass"))
        #expect(!text.contains("Run the test suite and fix anything that fails"))
        #expect(Samples.terminalText(for: "s-04") == Samples.terminalText)
    }

    @Test func theTerminalDrawsTheConversation() {
        let lines = DemoScreen.wrap("one two three four five six", 14, lead: "> ", style: "")
        #expect(lines.count > 1)
        #expect(lines.allSatisfy { $0.hasPrefix("> ") })
        let screen = DemoScreen.agent(items: [], prompt: "Add a timeline", phase: .ready, cols: 40)
        #expect(screen.contains("OpenCode"))
        #expect(screen.contains("Add a timeline"))
        let project = DemoProject.load()
        let shell = DemoScreen.shell(written: ["src/render.ts"], project: project)
        #expect(shell.contains("src/render.ts"))
        #expect(!shell.contains("src/incidents.ts"))
    }

    /// Opening the terminal again and again adds nothing to what the script calls on a change.
    @Test func reopeningTheTerminalAddsNoHooks() throws {
        let visit = DemoVisit(pace: 0.01)
        let script = try #require(visit.script)
        let hooks = script.onChange.count
        let handles = (0..<3).map { _ in
            visit.services.terminal(hostID: DemoMachine.id, sessionID: DemoScript.sessionID, transport: nil, userStarted: true)
        }
        #expect(handles.count == 3)
        #expect(script.onChange.count == hooks)
        visit.end()
    }
}
