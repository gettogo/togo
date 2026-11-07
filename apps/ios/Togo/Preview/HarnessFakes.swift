#if DEBUG
import Foundation
import TogoKit

/// Harness services that serve `HarnessSamples`: screens, completions and actions that answer
/// at once. Selecting in a picker or committing a setting changes the screen it serves, and
/// every call is recorded.
@MainActor final class PreviewHarnessServices: HarnessServices {
    struct Act: Equatable {
        var action: String
        var target: String?
        var args: [String: JSONValue]?
    }

    var descriptor = HarnessSamples.descriptor
    var screens: [String: HarnessScreen] = HarnessSamples.screens
    var commands = HarnessSamples.commands
    var files = HarnessSamples.files
    /// Screens whose read fails, with a fallback refusal when the value is true.
    var failing: [String: Bool] = [:]
    /// What an action answers, in place of the default (applied, or typed for `/` routes).
    var answer: ((Act) -> HarnessActionResult)?
    private(set) var acts: [Act] = []
    /// What Stop answers, and how many times it was asked.
    var interruptResult = HarnessActionResult(result: .applied)
    private(set) var interrupts = 0
    private(set) var sends: [(text: String, mode: HarnessSendParams.Mode)] = []
    private(set) var completes: [(kind: HarnessCompleteParams.Kind, query: String)] = []
    private var tick = 0

    func describe(_ s: HarnessSubject) async throws -> HarnessDescriptor { descriptor }

    func feed(_ s: HarnessSubject, after: Int) -> AsyncThrowingStream<HarnessFeed, any Error> {
        AsyncThrowingStream { $0.finish() }
    }

    func history(_ s: HarnessSubject, before: Int) async throws -> HarnessPage { HarnessPage(items: [], more: false) }

    func screen(_ s: HarnessSubject, id: String, query: [String: String]?) async throws -> HarnessScreen {
        if let fallback = failing[id] {
            throw fallback ? HelperError.refused("fallback") : HelperError.closed
        }
        guard let screen = screens[id] else { throw HelperError.refused("fallback") }
        return screen
    }

    func act(_ s: HarnessSubject, action: String, target: String?, args: [String: JSONValue]?) async throws -> HarnessActionResult {
        let act = Act(action: action, target: target, args: args)
        acts.append(act)
        if let answer { return answer(act) }
        return HarnessActionResult(result: .applied, screen: apply(act))
    }

    func send(_ s: HarnessSubject, text: String, mode: HarnessSendParams.Mode, images: [String]) async throws -> HarnessItem {
        sends.append((text, mode))
        let at = WireDate(Date(timeIntervalSince1970: 1_793_000_000))
        return HarnessItem(id: "q\(sends.count)", seq: 100 + sends.count, at: at,
                           body: .user(.init(text: text, queued: mode == .queue, source: "phone",
                                             images: images.map { HarnessImage(path: $0) })))
    }

    func upload(_ s: HarnessSubject, name: String, data: Data) async throws -> String { "/tmp/uploads/\(name)" }

    func image(_ s: HarnessSubject, path: String) async throws -> Data { throw HarnessUnavailable() }

    func interrupt(_ s: HarnessSubject) async throws -> HarnessActionResult {
        interrupts += 1
        return interruptResult
    }

    func complete(_ s: HarnessSubject, kind: HarnessCompleteParams.Kind, query: String) async throws -> [HarnessCompletion] {
        completes.append((kind, query))
        return CommandLineLogic.filter(kind == .command ? commands : files, query: query)
    }

    func locate(_ s: HarnessSubject, anchor: HarnessAnchor) async throws -> HarnessLocation {
        HarnessLocation(found: false, linesFromBottom: 0)
    }

    /// A picker selection or a setting change, applied to the screen that offered it.
    private func apply(_ act: Act) -> HarnessScreen? {
        tick += 1
        for (id, var screen) in screens {
            if var picker = screen.picker, picker.onSelect == act.action, case let .string(item)? = act.args?["id"] {
                picker.selected = item
                screen.picker = picker
            } else if var settings = screen.settings, let value = act.args?["value"] {
                var changed = false
                for s in settings.sections.indices {
                    for f in settings.sections[s].fields.indices where settings.sections[s].fields[f].id == act.target {
                        if settings.sections[s].fields[f].kind == .secret {
                            settings.sections[s].fields[f].isSet = true
                        } else {
                            settings.sections[s].fields[f].value = value
                        }
                        changed = true
                    }
                }
                guard changed else { continue }
                screen.settings = settings
            } else {
                continue
            }
            screen.fetchedAt = WireDate(screen.fetchedAt.date.addingTimeInterval(Double(tick)))
            screens[id] = screen
            return screen
        }
        return nil
    }
}

extension HarnessSessionStore {
    /// A store over preview services, for previews and tests.
    static func preview(_ services: PreviewHarnessServices = PreviewHarnessServices()) -> HarnessSessionStore {
        HarnessSessionStore(
            subject: HarnessSubject(
                hostID: UUID(uuidString: "6F1D2B7C-2A55-4C1E-9D7A-0C1B2D3E4F50")!, sessionID: "s_preview",
                capabilities: [Capability.harness]
            ),
            services: services
        )
    }
}

extension CommandLineModel.Routes {
    /// Routes over preview services with the session state given here, since a preview store
    /// is never started.
    @MainActor static func preview(
        _ services: PreviewHarnessServices, working: Bool = false, mode: String = "default",
        queued: [HarnessItem] = [], capabilities: Set<String>? = nil
    ) -> Self {
        let caps = capabilities ?? Set(services.descriptor.capabilities)
        let subject = HarnessSessionStore.preview(services).subject
        return Self(
            has: { caps.contains($0) },
            working: { working },
            mode: { mode },
            modes: { services.descriptor.modes },
            queued: { queued },
            scope: { "preview" },
            complete: { try await services.complete(subject, kind: $0, query: $1) },
            send: { try await services.send(subject, text: $0, mode: $1) },
            interrupt: { try await services.interrupt(subject) },
            run: { try await services.act(subject, action: $0, target: $1, args: $2) }
        )
    }
}

/// Sample harness data drawn from real sessions: Claude Code's model and usage screens, omp's
/// model roles, Codex's settings, a plan, background jobs, a patch and an agent's questions.
enum HarnessSamples {
    static let at = WireDate(Date(timeIntervalSince1970: 1_793_000_000))

    static let descriptor = HarnessDescriptor(
        adapter: .init(id: "claude", name: "Claude Code", harnessVersion: "2.1.319", revision: 1),
        capabilities: ["chat", "chat.anchors", "chat.history", "send.queue", "interrupt", "commands", "files", "mode",
                       "model", "effort", "needs.approvals", "needs.questions", "agents", "background", "plan",
                       "changes", "ports", "status", "status.usage", "status.context", "resume", "compact"],
        screens: [
            .init(id: "model", primitive: .picker, title: "Model", section: "composer"),
            .init(id: "models", primitive: .picker, title: "Models", section: "more"),
            .init(id: "settings", primitive: .settings, title: "Settings", section: "more"),
            .init(id: "plan", primitive: .checklist, title: "Plan", section: "plan"),
            .init(id: "jobs", primitive: .liveList, title: "Background", section: "background"),
            .init(id: "patch", primitive: .detail, title: "Patch", section: "changes"),
            .init(id: "question", primitive: .confirm, title: "Question", section: "needs"),
            .init(id: "compact", primitive: .confirm, title: "Compact", section: "status"),
            .init(id: "usage", primitive: .status, title: "Usage", section: "status"),
        ],
        modes: [.init(id: "default", label: "Ask"), .init(id: "acceptEdits", label: "Auto edit"), .init(id: "plan", label: "Plan")],
        mode: "default"
    )

    static var screens: [String: HarnessScreen] {
        Dictionary(uniqueKeysWithValues: [model, models, settings, plan, jobs, patch, notes, question, compact, usage].map { ($0.id, $0) })
    }

    static let model = HarnessScreen(id: "model", primitive: .picker, title: "Model", fetchedAt: at, picker: .init(items: [
        .init(id: "opus", title: "Opus", subtitle: "Most capable"),
        .init(id: "sonnet", title: "Sonnet", subtitle: "Fast and capable"),
        .init(id: "haiku", title: "Haiku", subtitle: "Fastest"),
        .init(id: "opus[1m]", title: "Opus", subtitle: "1M context", badge: "1M"),
    ], selected: "opus", searchable: false, onSelect: "set_model"))

    private static func m(_ id: String, _ title: String, _ group: String, kind: String = "chat", ctx: String, price: String,
                          roles: [String] = []) -> HarnessScreen.Picker.Item {
        var facets = ["kind": kind, "context": ctx, "price": price, "free": price == "free" ? "true" : "false"]
        for role in roles { facets[role] = "true" }
        return .init(id: id, title: title, subtitle: id, badge: ctx, group: group, facets: facets)
    }

    static let models = HarnessScreen(id: "models", primitive: .picker, title: "Models", fetchedAt: at, picker: .init(
        items: [
            m("anthropic/claude-opus-4-1", "Claude Opus 4.1", "anthropic", ctx: "200K", price: "$15/$75", roles: ["slow"]),
            m("anthropic/claude-sonnet-4-5", "Claude Sonnet 4.5", "anthropic", ctx: "1M", price: "$3/$15", roles: ["default", "plan"]),
            m("anthropic/claude-haiku-4-5", "Claude Haiku 4.5", "anthropic", ctx: "200K", price: "$1/$5", roles: ["smol"]),
            m("openai/gpt-5.1-codex", "GPT-5.1 Codex", "openai", kind: "reasoning", ctx: "400K", price: "$1.25/$10"),
            m("openai/gpt-5.1-mini", "GPT-5.1 mini", "openai", ctx: "400K", price: "$0.25/$2"),
            m("openai/text-embedding-3-large", "Embedding 3 large", "openai", kind: "embedding", ctx: "8K", price: "$0.13"),
            m("google/gemini-3-pro", "Gemini 3 Pro", "google", kind: "reasoning", ctx: "1M", price: "$2/$12"),
            m("google/gemini-3-flash", "Gemini 3 Flash", "google", ctx: "1M", price: "$0.30/$2.50"),
            m("openrouter/qwen3-coder:free", "Qwen3 Coder", "openrouter", ctx: "262K", price: "free"),
            m("openrouter/deepseek-v3.2", "DeepSeek V3.2", "openrouter", kind: "reasoning", ctx: "164K", price: "$0.27/$0.40"),
        ],
        selected: "anthropic/claude-sonnet-4-5", searchable: true,
        filters: [
            .init(id: "kind", label: "Kind", kind: "choice", options: [
                .init(id: "chat", label: "Chat"), .init(id: "reasoning", label: "Reasoning"), .init(id: "embedding", label: "Embedding"),
            ]),
            .init(id: "free", label: "Free", kind: "toggle"),
        ],
        onSelect: "set_default",
        itemActions: [
            .init(id: "default", label: "default"), .init(id: "smol", label: "smol"),
            .init(id: "slow", label: "slow"), .init(id: "plan", label: "plan"),
        ]
    ))

    static let settings = HarnessScreen(id: "settings", primitive: .settings, title: "Settings", fetchedAt: at, settings: .init(sections: [
        .init(title: "Model", fields: [
            .init(id: "model", label: "Model", kind: .choice, value: .string("gpt-5.1-codex"),
                  options: [.init(id: "gpt-5.1-codex", label: "GPT-5.1 Codex"), .init(id: "gpt-5.1", label: "GPT-5.1")], action: "set_model"),
            .init(id: "effort", label: "Reasoning effort", kind: .choice, value: .string("medium"), options: [
                .init(id: "low", label: "Low", detail: "Quick answers, light reasoning"),
                .init(id: "medium", label: "Medium", detail: "The default balance"),
                .init(id: "high", label: "High", detail: "Slower, for hard problems"),
            ], action: "set_effort"),
        ]),
        .init(title: "Sandbox", footer: "Applies from the next turn.", fields: [
            .init(id: "network", label: "Network access", help: "Commands may reach the internet", kind: .toggle,
                  value: .bool(false), action: "set_network"),
            .init(id: "turns", label: "Max turns", kind: .number, value: .number(24), min: 1, max: 50, action: "set_turns"),
            .init(id: "notes", label: "Instructions file", kind: .text, value: .string("AGENTS.md"), placeholder: "path", action: "set_notes"),
            .init(id: "home", label: "Codex home", kind: .text, value: .string("~/.codex"), readOnly: true),
        ]),
        .init(title: "Providers", fields: [
            .init(id: "openai.key", label: "OpenAI API key", kind: .secret, value: .null, isSet: true, action: "set_key"),
            .init(id: "azure.key", label: "Azure key", kind: .secret, value: .null, isSet: false, action: "set_key"),
        ]),
    ]))

    static let plan = HarnessScreen(id: "plan", primitive: .checklist, title: "Plan", fetchedAt: at, checklist: .init(items: [
        .init(id: "1", text: "Read the failing date tests", state: .done),
        .init(id: "2", text: "Parse timestamps in UTC", state: .done, children: [
            .init(id: "2a", text: "Honour explicit offsets", state: .done),
            .init(id: "2b", text: "Fall back to loose parsing", state: .done),
        ]),
        .init(id: "3", text: "Pin the test timezone", state: .active),
        .init(id: "4", text: "Run the whole suite", state: .pending),
        .init(id: "5", text: "Update the changelog", state: .skipped),
    ], done: 2, total: 5))

    static let jobs = HarnessScreen(id: "jobs", primitive: .liveList, title: "Background", fetchedAt: at, liveList: .init(rows: [
        .init(id: "dev", title: "npm run dev", subtitle: "server  :5173", state: .running, startedAt: WireDate(at.date.addingTimeInterval(-372)),
              tail: ["  VITE v6.2.0  ready in 412 ms", "  ➜  Local:   http://localhost:5173/"],
              open: .init(kind: .terminal, ref: "job-dev"), actions: [.init(id: "stop", label: "Stop", role: .destructive, confirm: "Stop the dev server?")]),
        .init(id: "test", title: "go test ./...", subtitle: "shell", state: .running, startedAt: WireDate(at.date.addingTimeInterval(-48)),
              progress: 0.62, tail: ["ok   internal/clock  0.41s", "ok   internal/parse  1.02s", "=== RUN   TestOffsets"]),
        .init(id: "build", title: "Build", subtitle: "exec", state: .ok, startedAt: WireDate(at.date.addingTimeInterval(-900))),
        .init(id: "lint", title: "Lint", subtitle: "exec  exit 1", state: .failed, tail: ["src/date.ts:14:7  no-unused-vars"]),
    ], refreshSeconds: 5))

    static let patchText = """
    diff --git a/src/date.ts b/src/date.ts
    --- a/src/date.ts
    +++ b/src/date.ts
    @@ -12,5 +12,6 @@ export function parse(s: string) {
       const m = ISO.exec(s)
    -  if (!m) throw new Error(`bad date: ${s}`)
    +  if (!m) return parseLoose(s)
       const [, y, mo, d] = m
    -  return new Date(+y, +mo - 1, +d)
    +  const off = m[7] ? offsetMs(m[7]) : 0
    +  return new Date(Date.UTC(+y, +mo - 1, +d) - off)
     }
    """

    static let patch = HarnessScreen(id: "patch", primitive: .detail, title: "Patch", fetchedAt: at, detail: .init(
        subtitle: "apply_patch  1 file  +3 -2", body: .init(kind: "diff", text: patchText),
        actions: [.init(id: "reject", label: "Reject", role: .destructive), .init(id: "apply", label: "Apply", role: .primary)]
    ))

    static let notes = HarnessScreen(id: "notes", primitive: .detail, title: "Summary", fetchedAt: at, detail: .init(
        subtitle: "turn 14", body: .init(kind: "markdown", text: """
        ## What changed
        Dates now parse in **UTC**, and explicit offsets are honoured.

        - `parse()` falls back to loose parsing
        - Tests pin `TZ=UTC`

        ```sh
        npm test -- date
        ```
        """)
    ))

    static let question = HarnessScreen(id: "question", primitive: .confirm, title: "Question", fetchedAt: at, confirm: .init(
        message: "Two things before I migrate staging.",
        questions: [
            HarnessQuestion(id: "env", header: "Target", prompt: "Which environment should the backfill run against?", options: [
                .init(id: "staging", label: "Staging", detail: "84,210 rows, about a minute"),
                .init(id: "prod", label: "Production", detail: "Needs a maintenance window"),
            ], multi: false, allowText: false),
            HarnessQuestion(id: "checks", header: "Checks", prompt: "Which checks should run after?", options: [
                .init(id: "unit", label: "Unit tests"), .init(id: "session", label: "Session tests"), .init(id: "e2e", label: "End to end"),
            ], multi: true, allowText: true),
        ],
        submit: "answer"
    ))

    static let compact = HarnessScreen(id: "compact", primitive: .confirm, title: "Compact", fetchedAt: at, confirm: .init(
        message: "Summarise the conversation to free context? The agent keeps a summary and drops the rest.", submit: "compact"
    ))

    static let usage = HarnessScreen(id: "usage", primitive: .status, title: "Usage", fetchedAt: at, status: .init(
        model: "opus", mode: "Ask", effort: "high", contextPercent: 62, costUsd: 1.84,
        meters: [
            .init(id: "session", label: "Current session", percent: 41, resetsAt: WireDate(at.date.addingTimeInterval(7_800))),
            .init(id: "week", label: "This week", percent: 86, resetsAt: WireDate(at.date.addingTimeInterval(302_400))),
        ],
        facts: [.init(label: "Session", value: "8f2c1e"), .init(label: "Version", value: "2.1.319")],
        actions: [.init(id: "compact", label: "Compact", confirm: "Summarise the conversation to free context?")]
    ))

    static let commands: [HarnessCompletion] = [
        .init(insert: "/compact ", title: "/compact", detail: "Summarise the conversation to free context", hint: "[instructions]"),
        .init(insert: "/clear", title: "/clear", detail: "Start fresh, keep nothing"),
        .init(insert: "/model ", title: "/model", detail: "Set the AI model", hint: "[model]", screen: "model"),
        .init(insert: "/resume", title: "/resume", detail: "Resume an earlier session"),
        .init(insert: "/context", title: "/context", detail: "Show what fills the context window", screen: "usage"),
        .init(insert: "/review", title: "/review", detail: "Review the current changes"),
        .init(insert: "/init", title: "/init", detail: "Write a CLAUDE.md for this repo"),
        .init(insert: "/deploy-check ", title: "/deploy-check", detail: "Checks before a deploy", hint: "[env]"),
    ]

    static let files: [HarnessCompletion] = [
        .init(insert: "@internal/clock/parse.go ", title: "parse.go", detail: "internal/clock"),
        .init(insert: "@internal/clock/parse_test.go ", title: "parse_test.go", detail: "internal/clock"),
        .init(insert: "@src/date.ts ", title: "date.ts", detail: "src"),
    ]

    static let queuedItem = HarnessItem(id: "q1", seq: 8, at: at, body: .user(.init(text: "also update the changelog", queued: true, source: "phone")))
}
#endif
