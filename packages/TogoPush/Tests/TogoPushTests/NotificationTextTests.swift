import Foundation
import Testing
import TogoPush

struct NotificationTextTests {
    static let hostID = "h_mmg42klgyqzwneiskrelxms3j7"
    static let approveDeny = [
        PushOption(n: 1, label: "Yes", role: "approve"),
        PushOption(n: 2, label: "Yes, and don't ask again", role: "approve_always"),
        PushOption(n: 3, label: "No", role: "deny"),
    ]

    func needsYou(
        subject: PushSubject? = PushSubject(kind: "command", text: "rm -rf ./build && make"),
        options: [PushOption] = approveDeny, agent: String? = "claude", body: String = ""
    ) -> PushPayload {
        PushPayload(
            kind: .needsYou, sessionId: "s_abcdefghij", bib: "04", agent: agent, host: "studio",
            title: "Do you want to proceed?", body: body, requestId: "r_aaaaaaaaaa", options: options,
            subject: subject, ts: 1_793_707_200_000
        )
    }

    func build(_ p: PushPayload, multipleHosts: Bool = false) -> NotificationText? {
        NotificationTextBuilder.build(p, hostId: Self.hostID, hostName: "studio", multipleHosts: multipleHosts)
    }

    @Test func needsYouCommand() throws {
        let text = try #require(build(needsYou()))
        #expect(text.title == "04 Claude Code")
        #expect(text.subtitle == "Do you want to proceed?")
        #expect(text.body == "rm -rf ./build && make")
        #expect(text.category == "togo.request")
        #expect(text.threadID == "s_abcdefghij")
        #expect(text.interruption == .timeSensitive)
    }

    @Test func boxDrawingNeverRendered() throws {
        let boxed = PushSubject(kind: "text", text: "╭─────────────╮\n│ npm test    │\n╰─────────────╯")
        var p = needsYou(subject: boxed)
        p.title = "╭─ Bash command ─╮"
        let text = try #require(build(p))
        let all = text.title + text.subtitle + text.body
        #expect(!all.unicodeScalars.contains { (0x2500...0x259F).contains($0.value) })
        #expect(text.body == "npm test")
        #expect(text.subtitle == "Bash command")
    }

    @Test func multiHostAddsHostName() throws {
        #expect(try #require(build(needsYou(), multipleHosts: true)).title == "04 Claude Code on studio")
        #expect(try #require(build(needsYou(), multipleHosts: false)).title == "04 Claude Code")
    }

    @Test func denyOnlyCategory() throws {
        let text = try #require(build(needsYou(options: [PushOption(n: 1, label: "Stop", role: "deny")])))
        #expect(text.category == "togo.request.deny")
    }

    @Test func questionWithoutApproveRoleIsInfo() throws {
        let options = [PushOption(n: 1, label: "Red", role: "other"), PushOption(n: 2, label: "Blue", role: "other")]
        let text = try #require(build(needsYou(subject: nil, options: options, body: "Which colour?")))
        #expect(text.category == "togo.info")
        #expect(text.body == "Which colour?")
        #expect(try #require(build(needsYou(options: [Self.approveDeny[0]]))).category == "togo.info")
    }

    @Test func clearBuildsNothing() {
        let p = PushPayload(kind: .clear, title: "", body: "", requestId: "r_aaaaaaaaaa", ts: 1)
        #expect(build(p) == nil)
    }

    @Test func unknownAgentCapitalised() throws {
        #expect(try #require(build(needsYou(agent: "aider"))).title == "04 Aider")
        #expect(AgentName.display("codex") == "Codex")
        #expect(AgentName.display("omp") == "OMP")
        #expect(AgentName.display("opencode") == "OpenCode")
    }

    @Test func subjectKinds() throws {
        let url = PushSubject(kind: "url", text: "https://example.com/docs/api?token=abc#frag")
        #expect(try #require(build(needsYou(subject: url))).body == "example.com/docs/api")
        let file = PushSubject(kind: "file", text: "~/Проекты/app/main.go")
        #expect(try #require(build(needsYou(subject: file))).body == "~/Проекты/app/main.go")
        #expect(try #require(build(needsYou(subject: nil, body: "│ Edit main.go │"))).body == "Edit main.go")
    }

    @Test func doneJobAndNotify() throws {
        let done = PushPayload(
            kind: .done, sessionId: "s_abcdefghij", bib: "07", agent: "codex", title: "Finished", body: "All tests pass",
            ts: 1
        )
        #expect(try #require(build(done)) == NotificationText(
            title: "07 Codex", subtitle: "Done", body: "All tests pass", category: "togo.info",
            threadID: "s_abcdefghij", interruption: .active
        ))
        let job = PushPayload(kind: .job, title: "nightly build", body: "ok", ts: 1)
        let jobText = try #require(build(job))
        #expect(jobText.subtitle == "nightly build")
        #expect(jobText.body == "Finished")
        #expect(jobText.threadID == Self.hostID, "no session: the machine's thread")
        #expect(jobText.interruption == .active)
        #expect(try #require(build(PushPayload(kind: .job, title: "x", body: "failed", ts: 1))).body == "Failed")
        #expect(try #require(build(PushPayload(kind: .job, title: "x", body: "disk full", ts: 1))).body == "disk full")
        let notify = PushPayload(kind: .notify, sessionId: "s_abcdefghij", title: "❯ Look", body: "│ here │", ts: 1)
        let notifyText = try #require(build(notify))
        #expect(notifyText.title == "Look")
        #expect(notifyText.body == "here")
        #expect(notifyText.category == "togo.info")
    }

    /// Every case of the table shared with the helper renders as its cleaned text.
    @Test func cleanTableAsSubject() throws {
        var url = URL(filePath: #filePath)
        for _ in 0..<5 { url.deleteLastPathComponent() }
        url.append(path: "protocol/fixtures/render/clean.json")
        let table = try JSONDecoder().decode(PromptTextTests.Table.self, from: Data(contentsOf: url))
        for c in table.cases {
            let subject = PushSubject(kind: "text", text: c.in)
            let p = needsYou(subject: subject.isValid ? subject : nil, body: c.in)
            #expect(build(p)?.body == c.want, "\(c.name)")
        }
    }
}
