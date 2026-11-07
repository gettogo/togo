import Foundation
@testable import TogoKit

/// One broken rule, with where it broke.
struct ReplayViolation: Hashable, CustomStringConvertible {
    /// Stable name of the rule, for known-issue lists.
    var rule: String
    var detail: String
    var description: String { "[\(rule)] \(detail)" }
}

/// Rules every replay must keep, whatever the harness or scenario. Each mirrors a class of
/// bug the owner found on device.
enum ReplayInvariants {
    /// How long the helper may take to tell the chat what the events already said (a close,
    /// an open), in fixture milliseconds.
    static let grace = 3000
    /// How long the pill may say "working" with nothing arriving from the agent.
    static let stuck = 45_000
    /// Rules about how a settled run ends.
    static let endRules: Set<String> = [
        "working-after-settle", "work-row-running-after-settle", "pending-after-settle",
        "needs-open-after-settle", "needs-count-mismatch", "no-reply",
    ]

    static func check(_ replay: Replay) -> [ReplayViolation] {
        var out: [ReplayViolation] = []
        for (index, state) in replay.states.enumerated() {
            let t = replay.t(index)
            let at = "after record \(index + 2) (t=\(t)ms)"
            out += uniqueIDs(state, at)
            out += oneWorking(state, at)
            out += resolvedNotShown(state, t: t, at)
            out += requestsVisible(state, t: t, at)
        }
        out += decodes(replay)
        out += foreignItems(replay)
        out += stuckWorking(replay)
        out += workingAfterTurnEnd(replay)
        if replay.fixture.header.complete ?? false {
            out += settled(replay.final)
            out += answered(replay)
        }
        // A rule broken at many points is one bug: report its first place only.
        var seen = Set<String>()
        return out.filter { seen.insert($0.rule).inserted }
    }

    /// No duplicated chat items or rows ("the same reply twice").
    static func uniqueIDs(_ s: ReplayModel, _ at: String) -> [ReplayViolation] {
        var out: [ReplayViolation] = []
        let ids = s.items.map(\.id)
        if Set(ids).count != ids.count { out.append(.init(rule: "unique-items", detail: "duplicate item ids \(dupes(ids)) \(at)")) }
        let rows = s.rows.map(\.id)
        if Set(rows).count != rows.count { out.append(.init(rule: "unique-rows", detail: "duplicate row ids \(dupes(rows)) \(at)")) }
        return out
    }

    /// At most one working indicator: one running turn, one running work line.
    static func oneWorking(_ s: ReplayModel, _ at: String) -> [ReplayViolation] {
        var out: [ReplayViolation] = []
        if s.runningTurns.count > 1 {
            out.append(.init(rule: "one-running-turn", detail: "\(s.runningTurns.count) running turns \(s.runningTurns.map(\.id)) \(at)"))
        }
        if s.runningWorkRows.count > 1 {
            out.append(.init(rule: "one-running-work-row", detail: "running work rows \(s.runningWorkRows) \(at)"))
        }
        return out
    }

    /// A request the helper closed is never shown as waiting: not in the prompt bar at once,
    /// not in the chat after `grace` ("phantom approvals that already resolved").
    static func resolvedNotShown(_ s: ReplayModel, t: Int, _ at: String) -> [ReplayViolation] {
        var out: [ReplayViolation] = []
        for a in s.pending where s.closedAt[a.requestId] != nil {
            out.append(.init(rule: "closed-request-pending", detail: "\(a.requestId) closed but pending \(at)"))
        }
        for rid in s.openApprovalRequests {
            guard let closed = s.closedAt[rid], t - closed > grace else { continue }
            out.append(.init(rule: "closed-request-in-chat", detail: "\(rid) closed at \(closed)ms, chat still waits \(at)"))
        }
        return out
    }

    /// A pending request of a session whose adapter shows needs in its chat is in the chat
    /// after `grace` ("requests vanishing": the prompt bar hides a chat session's requests).
    static func requestsVisible(_ s: ReplayModel, t: Int, _ at: String) -> [ReplayViolation] {
        guard let d = s.descriptor, d.adapter != nil, s.feed.isLoaded, !s.needsReopen else { return [] }
        guard d.has(HarnessCapability.needsApprovals) || d.has(HarnessCapability.needsQuestions) else { return [] }
        let shown = Set(s.openApprovalRequests + ownedApprovals(s))
        return s.pending.compactMap { a in
            guard let opened = s.openedAt[a.requestId], t - opened > grace, !shown.contains(a.requestId) else { return nil }
            return .init(rule: "pending-request-not-in-chat",
                         detail: "\(a.requestId) (\(a.title)) pending since \(opened)ms, no open card in the chat \(at)")
        }
    }

    /// Approvals a question draws (owner set) while that question is open.
    private static func ownedApprovals(_ s: ReplayModel) -> [String] {
        let openQuestions = Set(s.openNeeds.map(\.id))
        return s.items.compactMap { item in
            guard case .approval(let a) = item.body, let owner = a.owner, openQuestions.contains(owner), a.resolved == nil else { return nil }
            return a.requestId
        }
    }

    /// Every frame the app acts on decodes into its models. A harness frame counts only when
    /// the adapter offers the chat: without it the app never opens the feed.
    static func decodes(_ replay: Replay) -> [ReplayViolation] {
        let chat = replay.states.contains { $0.descriptor?.has(HarnessCapability.chat) ?? false }
        return replay.fixture.undecodable.filter { chat || !$0.method.hasPrefix("harness") }.prefix(1).map {
            .init(rule: "undecodable-frame", detail: "\($0.method) does not decode: \($0.why.prefix(300))")
        }
    }

    /// A session this recording created shows nothing of a conversation before the app's
    /// first prompt or action ("a new session shows the last session's chat").
    static func foreignItems(_ replay: Replay) -> [ReplayViolation] {
        guard let created = replay.request("session.create") else { return [] }
        let first = [replay.request("harness.send"), replay.request("harness.action")].compactMap { $0 }.min()
            ?? replay.fixture.records.count
        guard first > created else { return [] }
        let state = replay.after(first - 1)
        let foreign = state.items.filter { item in
            switch item.body {
            case .user, .assistant, .turn, .tool: true
            default: false
            }
        }
        guard !foreign.isEmpty else { return [] }
        return [.init(rule: "items-before-first-send",
                      detail: "new session shows \(foreign.count) conversation items before the first prompt: \(foreign.map { "\($0.body.kind) \($0.id.prefix(12))" })")]
    }

    /// "Working" never stays up while nothing arrives for `stuck` ("working stuck while idle").
    static func stuckWorking(_ replay: Replay) -> [ReplayViolation] {
        var lastItem = 0
        for (index, record) in replay.fixture.records.enumerated() {
            if case .harness(let note) = record.step, !(note.items ?? []).isEmpty { lastItem = record.t }
            if case .request(let method, _) = record.step, method == "session.kill" { break }
            let state = replay.after(index)
            if state.feed.mission.summary.working, state.pending.isEmpty, record.t - lastItem > stuck {
                return [.init(rule: "working-stuck",
                              detail: "working for \((record.t - lastItem) / 1000)s with nothing from the agent (t=\(record.t)ms, last item at \(lastItem)ms)")]
            }
        }
        return []
    }

    /// Once the agent's turn ended (its turn item is no longer live) and nothing new started,
    /// the pill stops saying "working" within `turnEndGrace` ("working stuck", "interrupt
    /// state stuck").
    static let turnEndGrace = 5000

    static func workingAfterTurnEnd(_ replay: Replay) -> [ReplayViolation] {
        var endedAt: [String: Int] = [:]
        for (index, state) in replay.states.enumerated() {
            let t = replay.t(index)
            let main = state.main
            guard let last = main.lastIndex(where: { if case .turn = $0.body { true } else { false } }),
                  case .turn(let turn) = main[last].body, !turn.state.isLive else { continue }
            let id = main[last].id
            if endedAt[id] == nil { endedAt[id] = t }
            let newer = main.contains { item in
                guard item.seq > main[last].seq else { return false }
                switch item.body {
                case .user, .agent: return true
                case .notice(let notice): return notice.code == HarnessItem.Notice.continued
                case .tool(let tool): return tool.state.isLive
                default: return false
                }
            }
            guard let ended = endedAt[id], !newer, state.pending.isEmpty, state.feed.mission.summary.working,
                  t - ended > turnEndGrace else { continue }
            return [.init(rule: "working-after-turn-end",
                          detail: "the turn ended (\(turn.state.rawValue)) at \(ended)ms, the pill still says working at \(t)ms")]
        }
        return []
    }

    /// A prompt the app sent gets something back in the chat: a reply, a notice (error,
    /// interrupted) or a request ("the reply never showed").
    static func answered(_ replay: Replay) -> [ReplayViolation] {
        guard replay.request("harness.send") != nil,
              replay.final.descriptor?.has(HarnessCapability.chat) ?? false else { return [] }
        let main = replay.final.main
        guard let prompt = main.lastIndex(where: { if case .user(let u) = $0.body { !u.queued } else { false } }) else {
            return [.init(rule: "no-reply", detail: "the sent prompt never showed in the chat")]
        }
        let back = main[(prompt + 1)...].contains { item in
            switch item.body {
            case .assistant, .notice, .approval, .question: true
            default: false
            }
        }
        return back ? [] : [.init(rule: "no-reply", detail: "nothing came back after the prompt \(main[prompt].id) (no reply, notice or request)")]
    }

    /// The scenario ran to its end and the session settled: nothing works, nothing waits.
    static func settled(_ s: ReplayModel) -> [ReplayViolation] {
        var out: [ReplayViolation] = []
        if !s.workingSignals.isEmpty {
            out.append(.init(rule: "working-after-settle", detail: "still working at the end: \(s.workingSignals)"))
        }
        if !s.runningWorkRows.isEmpty {
            out.append(.init(rule: "work-row-running-after-settle", detail: "work rows still running: \(s.runningWorkRows)"))
        }
        if !s.pending.isEmpty {
            out.append(.init(rule: "pending-after-settle", detail: "requests pending at the end: \(s.pending.map(\.requestId))"))
        }
        if !s.openNeeds.isEmpty {
            out.append(.init(rule: "needs-open-after-settle", detail: "chat cards still waiting: \(s.openNeeds.map(\.id))"))
        }
        if let needs = s.mission?.summary.needsYou, needs != s.pending.count {
            out.append(.init(rule: "needs-count-mismatch", detail: "pill says \(needs) need you, \(s.pending.count) pending"))
        }
        return out
    }

    private static func dupes(_ ids: [String]) -> [String] {
        Dictionary(grouping: ids, by: { $0 }).filter { $0.value.count > 1 }.keys.sorted()
    }
}
