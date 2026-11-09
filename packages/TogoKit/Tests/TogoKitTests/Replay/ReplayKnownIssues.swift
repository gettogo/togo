import Testing

/// Product bugs the replays expose, by fixture and check. Each entry maps a key found in the
/// failure's text (an invariant's "[rule" or a scenario message) to why it breaks. The tests
/// expect these failures (`withKnownIssue`): the suite stays green while the bug reproduces
/// and goes red ("known issue was not recorded") once the product is fixed; then delete the
/// entry. Product code is not changed from here.
enum ReplayKnownIssues {
    /// fixture → check → key → reason.
    static let all: [String: [String: [String: String]]] = [
        "claude.approve": ["invariants": [
            "[items-before-first-send": staleTranscript,
        ]],
        "claude.subagent": [
            "invariants": [
                "[items-before-first-send": staleTranscript,
            ],
        ],
        "claude.model": [
            "invariants": [
                "[working-stuck": "After set_model types `/model sonnet` (a local command, no turn), the mission stays working=true for good: the pill says Working on an idle session and held prompts never go.",
            ],
            "scenario": [
                "changed the agent's own config": "Claude's `/model X` saves X as the default for new sessions in ~/.claude/settings.json, so Togo's model picker changes every future Claude session (the recorder restored the file).",
            ],
        ],
        "claude.demo": ["invariants": ["[working-after-turn-end": turnEndLag]],
        "omp.turn": ["invariants": ["[working-after-turn-end": turnEndLag]],
        "opencode.interrupt": ["invariants": ["[working-after-turn-end": turnEndLag]],
        "opencode.turn": ["invariants": [
            "[no-reply": "OpenCode ran a turn (working true, then false, a \"done\" notify with an empty body) but neither a reply nor an error notice reached the chat: the user sees the prompt and nothing else.",
        ]],
        "vibe.turn": ["invariants": [
            "[no-reply": "Mistral Vibe ran a turn (working, then a \"done\" notify with an empty body) but neither a reply nor an error notice reached the chat.",
        ]],
        "claude.interrupt": ["scenario": [
            "working again": "harness.interrupt answered applied and the turn showed Interrupted, but Claude went on: a new Bash call ran and the reply streamed; the mission went working=false then true again, and session.status never left working.",
        ]],
    ]

    static let turnEndLag = "The turn item ends (ok or interrupted) but the mission summary keeps working=true for 6 to 10 s more (it waits for the hub's idle), so the pill, Stop and the queue act as if the agent still works."


    static let staleTranscript = "A new Claude session in a folder used before shows the previous session's conversation (its transcript is tailed before the new session's hook names its own)."

    static func expected(fixture: String, check: String) -> [String: String] {
        all[fixture]?[check] ?? [:]
    }

    /// Runs `body`, treating the issues it records that match a known key for this fixture
    /// and check as expected. Any other issue fails as usual.
    static func run(_ fixture: String, _ check: String, _ body: () throws -> Void) rethrows {
        let known = expected(fixture: fixture, check: check)
        guard !known.isEmpty else { return try body() }
        let reasons = known.sorted { $0.key < $1.key }.map { "\($0.key)…: \($0.value)" }.joined(separator: " | ")
        try withKnownIssue(Comment(rawValue: "\(fixture) \(check): \(reasons)")) {
            try body()
        } matching: { issue in
            let text = String(describing: issue)
            return known.keys.contains { text.contains($0) }
        }
    }
}
