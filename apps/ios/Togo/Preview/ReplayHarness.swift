import Foundation
import TogoKit

/// A recorded helper conversation, for UI tests and the demo machine: one protocol/v1 JSON-RPC message per line, in
/// the order it crossed the wire (`-ui-replay <name>` loads `<name>.jsonl` from the app's
/// resources).
///
/// - A request (`id` and `method`) is what the app asked; its response (`id` and `result` or
///   `error`) follows it somewhere later.
/// - A notification (`method`, no `id`) is what the helper said on its own: `harness` (a
///   `HarnessNotification`) or `event` (an `Event`; `approval.open` and `approval.closed` drive
///   the control center's requests).
/// - An optional `"t"` beside `jsonrpc` is milliseconds since the recording began; without it
///   the notification's own `ts` paces the replay. Blank lines and lines starting `//` are skipped.
///
/// Replay: `harness.open` answers with the recorded result, then the notifications up to the
/// first request that changes the session. `harness.send`, `harness.action` (matched by
/// `action`), `harness.interrupt` and `approval.answer` each release the next recorded chapter of
/// that kind: its response, then its notifications at the recorded pace (each gap clamped to
/// 50–500 ms). Reads (`harness.describe`, `harness.screen` by screen id, `harness.history`,
/// `harness.complete`, `harness.locate`) answer with the latest recorded response at or before
/// the replay's position. Mission deltas are renumbered from the open result's revision, so a
/// flow may skip chapters.
struct ReplayScript: Sendable {
    struct Note: Sendable {
        var method: String
        var params: Data
        /// Wait before it, seconds.
        var delay: Double
    }

    struct Chapter: Sendable {
        /// What releases it: `harness.send`, `harness.action:<action>`, `harness.interrupt`,
        /// `approval.answer`; nil for the opening chapter.
        var trigger: String?
        var response: Data?
        var line: Int
        var notes: [Note] = []
    }

    static let gating: Set<String> = ["harness.send", "harness.action", "harness.interrupt", "approval.answer"]

    private(set) var opened: Data?
    private(set) var chapters: [Chapter] = [Chapter(trigger: nil, line: 0)]
    /// Read answers by key, with the line their request was on.
    private(set) var reads: [String: [(line: Int, result: Data)]] = [:]

    enum ParseError: Error, Equatable { case badLine(Int) }

    /// `longestGap` caps the wait between notifications, seconds (`-ui-replay-pace <s>` sets it
    /// for a launch; the website's recording plays at 0.25).
    init(jsonl: String, longestGap: Double = 0.5) throws {
        var requests: [String: (method: String, key: String, chapter: Int?, line: Int)] = [:]
        var lastStamp: Double?
        for (index, raw) in jsonl.split(separator: "\n", omittingEmptySubsequences: false).enumerated() {
            let text = raw.trimmingCharacters(in: .whitespaces)
            guard !text.isEmpty, !text.hasPrefix("//") else { continue }
            guard let object = try? JSONSerialization.jsonObject(with: Data(text.utf8)) as? [String: Any] else {
                throw ParseError.badLine(index + 1)
            }
            let id = object["id"].map { "\($0)" }
            let method = object["method"] as? String
            let params = object["params"] as? [String: Any] ?? [:]
            if let id, let method {
                // A request.
                let key = Self.key(method, params)
                var chapter: Int?
                if Self.gating.contains(method) {
                    chapters.append(Chapter(trigger: key, line: index))
                    chapter = chapters.count - 1
                }
                requests[id] = (method, key, chapter, index)
            } else if let id, let request = requests[id] {
                // A response.
                guard let result = object["result"], let data = try? JSONSerialization.data(withJSONObject: result) else { continue }
                if request.method == "harness.open", opened == nil {
                    opened = data
                } else if let chapter = request.chapter {
                    chapters[chapter].response = data
                } else {
                    reads[request.key, default: []].append((request.line, data))
                }
            } else if let method {
                // A notification.
                let stamp = (object["t"] as? Double).map { $0 / 1000 } ?? Self.stamp(params["ts"])
                var delay = 0.15
                if let stamp, let lastStamp { delay = min(max(stamp - lastStamp, 0.05), longestGap) }
                if let stamp { lastStamp = stamp }
                guard let data = try? JSONSerialization.data(withJSONObject: params) else { continue }
                chapters[chapters.count - 1].notes.append(Note(method: method, params: data, delay: delay))
            }
        }
    }

    static func key(_ method: String, _ params: [String: Any]) -> String {
        switch method {
        case "harness.action": "harness.action:\(params["action"] as? String ?? "")"
        case "harness.screen": "harness.screen:\(params["screen"] as? String ?? "")"
        case "harness.complete": "harness.complete:\(params["kind"] as? String ?? "")"
        default: method
        }
    }

    private static func stamp(_ value: Any?) -> Double? {
        guard let text = value as? String else { return nil }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: text) { return date.timeIntervalSince1970 }
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.date(from: text)?.timeIntervalSince1970
    }

    /// The latest recorded answer to `key` at or before `line`, else the first one.
    func read(_ key: String, atOrBefore line: Int) -> Data? {
        guard let answers = reads[key] else { return nil }
        return answers.last { $0.line <= line }?.result ?? answers.first?.result
    }

    /// The recorded session's agent, as the app names agents ("opencode", "claude"): the adapter
    /// `harness.describe` reported, without its protocol generation ("opencode2").
    var agent: Agent? {
        guard let data = read("harness.describe", atOrBefore: .max),
              let adapter = (try? JSONDecoder().decode(HarnessDescriptor.self, from: data))?.adapter else { return nil }
        let id = String(adapter.id.reversed().drop(while: \.isNumber).reversed())
        return Agent(id: id.isEmpty ? adapter.id : id, name: adapter.name)
    }

    struct Agent: Equatable, Sendable {
        var id: String
        var name: String
    }

    /// The first chapter after `released` that `trigger` releases.
    func chapter(for trigger: String, after released: Int) -> Int? {
        chapters.indices.first { $0 > released && chapters[$0].trigger == trigger }
    }

    /// Recordings the app ships once under another name: the website's `replay-status` is the
    /// demo machine's session.
    static let aliases = ["replay-status": "demo-session"]

    static func load(_ name: String) -> ReplayScript? {
        let name = aliases[name] ?? name
        guard let url = Bundle.main.url(forResource: name, withExtension: "jsonl"),
              let text = try? String(contentsOf: url, encoding: .utf8) else { return nil }
        let args = ProcessInfo.processInfo.arguments
        let pace = args.firstIndex(of: "-ui-replay-pace").flatMap { args.indices.contains($0 + 1) ? Double(args[$0 + 1]) : nil }
        return try? ReplayScript(jsonl: text, longestGap: pace ?? 0.5)
    }
}

/// Harness services that play a `ReplayScript` back, in place of a machine's helper. Approval
/// events reach the control center through `onApproval`; answering a request there releases its
/// chapter through `trigger`.
@MainActor final class ReplayHarnessServices: HarnessServices {
    enum ApprovalChange {
        case open(Approval)
        case closed(requestID: String)
    }

    let script: ReplayScript
    /// The control center's side of approval events, with the session they belong to.
    var onApproval: ((ApprovalChange, String) -> Void)?
    /// What plays, for `LiveDemo`: a chapter released by its trigger, each `event` notification's
    /// params, and the end of a chapter's notifications.
    var onPlayback: ((Playback) -> Void)?

    enum Playback {
        case released(String)
        case event([String: Any])
        /// A harness notification, as the feeds got it.
        case harness(HarnessNotification)
        case finished(String?)
    }
    private var released = 0
    private var position = 0
    private var feeds: [UUID: (session: String, continuation: AsyncThrowingStream<HarnessFeed, any Error>.Continuation)] = [:]
    private var session = ""
    /// The mission's revision as the app holds it: a test that skips a chapter (approves without
    /// sending first) still gets deltas that follow on, so the store never reopens.
    private var missionRev = 0
    private let decoder = JSONDecoder()
    /// What has played so far, so a feed opened again (a tab reopened, the app back from the
    /// background) starts where the session is, as a helper's `harness.open` would.
    private var played: [String: HarnessItem] = [:]
    private var mission = MissionState()
    private var hasPlayed = false
    /// Permission questions offered as approvals, by item id, as the helper offers them.
    private var offered: [String: String] = [:]
    private var openingPlayed = false

    init(script: ReplayScript) {
        self.script = script
    }

    func describe(_ s: HarnessSubject) async throws -> HarnessDescriptor {
        try read("harness.describe")
    }

    func feed(_ s: HarnessSubject, after: Int) -> AsyncThrowingStream<HarnessFeed, any Error> {
        session = s.sessionID
        let id = UUID()
        let (stream, continuation) = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        feeds[id] = (s.sessionID, continuation)
        continuation.onTermination = { [weak self] _ in Task { @MainActor in self?.feeds[id] = nil } }
        if let data = script.opened, var opened = try? decoder.decode(HarnessOpenResult.self, from: data) {
            if hasPlayed {
                opened.items = played.values.sorted { $0.seq < $1.seq }
                opened.cursor = max(opened.cursor, opened.items.last?.seq ?? 0)
                if var current = mission.mission {
                    current.rev = missionRev
                    opened.mission = current
                }
            } else {
                missionRev = opened.mission.rev
                mission.load(opened.mission)
            }
            continuation.yield(.opened(opened))
        }
        // The opening chapter plays once, for the first feed.
        if !openingPlayed {
            openingPlayed = true
            play(0)
        }
        return stream
    }

    func history(_ s: HarnessSubject, before: Int) async throws -> HarnessPage {
        (try? read("harness.history")) ?? HarnessPage(items: [], more: false)
    }

    func screen(_ s: HarnessSubject, id: String, query: [String: String]?) async throws -> HarnessScreen {
        do { return try read("harness.screen:\(id)") } catch { throw HelperError.refused("fallback") }
    }

    func act(_ s: HarnessSubject, action: String, target: String?, args: [String: JSONValue]?) async throws -> HarnessActionResult {
        (try? release("harness.action:\(action)")) ?? HarnessActionResult(result: .applied)
    }

    func send(_ s: HarnessSubject, text: String, mode: HarnessSendParams.Mode, images: [String]) async throws -> HarnessItem {
        if let result: HarnessSendResult = try? release("harness.send") { return result.item }
        return HarnessItem(id: "replay-\(UUID().uuidString)", seq: 10_000, at: WireDate(Date()),
                           body: .user(.init(text: text, queued: mode == .queue, source: "phone", images: [])))
    }

    func upload(_ s: HarnessSubject, name: String, data: Data) async throws -> String { "/tmp/uploads/\(name)" }

    func image(_ s: HarnessSubject, path: String) async throws -> Data { throw HarnessUnavailable() }

    func interrupt(_ s: HarnessSubject) async throws -> HarnessActionResult {
        (try? release("harness.interrupt")) ?? HarnessActionResult(result: .applied)
    }

    func complete(_ s: HarnessSubject, kind: HarnessCompleteParams.Kind, query: String) async throws -> [HarnessCompletion] {
        let items = (try? read("harness.complete:\(kind.rawValue)") as HarnessCompleteResult)?.items ?? []
        return CommandLineLogic.filter(items, query: query)
    }

    func locate(_ s: HarnessSubject, anchor: HarnessAnchor) async throws -> HarnessLocation {
        (try? read("harness.locate")) ?? HarnessLocation(found: false, linesFromBottom: 0)
    }

    /// Whether a recorded chapter still waits for `trigger`, such as `harness.send`.
    func canRelease(_ trigger: String) -> Bool {
        script.chapter(for: trigger, after: released) != nil
    }

    /// The control center answered a request: releases the recorded `approval.answer` chapter.
    /// A recording whose requests were questions (OpenCode's permissions) has no
    /// `approval.answer` chapters: an approval answered from the phone releases the question's
    /// answer instead, as the helper answers through the adapter.
    func trigger(_ method: String) {
        let index = script.chapter(for: method, after: released)
            ?? (method == "approval.answer" ? script.chapter(for: "harness.action:answer", after: released) : nil)
        guard let index else { return }
        released = index
        position = script.chapters[index].line
        onPlayback?(.released(method))
        play(index)
    }

    // MARK: Playback

    private func read<T: Decodable>(_ key: String) throws -> T {
        guard let data = script.read(key, atOrBefore: position) else { throw HarnessUnavailable() }
        return try decoder.decode(T.self, from: data)
    }

    private func release<T: Decodable>(_ trigger: String) throws -> T {
        guard let index = script.chapter(for: trigger, after: released) else { throw HarnessUnavailable() }
        released = index
        position = script.chapters[index].line
        onPlayback?(.released(trigger))
        play(index)
        guard let data = script.chapters[index].response else { throw HarnessUnavailable() }
        return try decoder.decode(T.self, from: data)
    }

    private func play(_ index: Int) {
        let notes = script.chapters[index].notes
        let trigger = script.chapters[index].trigger
        Task { [weak self] in
            for note in notes {
                try? await Task.sleep(for: .seconds(note.delay))
                self?.deliver(note)
            }
            self?.onPlayback?(.finished(trigger))
        }
    }

    private func remember(_ notification: HarnessNotification) {
        hasPlayed = true
        for item in notification.items ?? [] where (played[item.id]?.seq ?? .min) < item.seq {
            played[item.id] = item
        }
        if let delta = notification.mission {
            var applied = delta
            applied.rev = (mission.mission?.rev ?? 0) + 1
            _ = mission.apply(applied)
        }
    }

    private func deliver(_ note: ReplayScript.Note) {
        switch note.method {
        case "harness":
            guard var notification = try? decoder.decode(HarnessNotification.self, from: note.params) else { return }
            if notification.mission != nil {
                missionRev += 1
                notification.mission?.rev = missionRev
            }
            remember(notification)
            for feed in feeds.values {
                notification.sessionId = feed.session
                feed.continuation.yield(.note(notification))
            }
            onPlayback?(.harness(notification))
            for item in notification.items ?? [] { offer(item) }
        case "event":
            guard let event = try? JSONSerialization.jsonObject(with: note.params) as? [String: Any] else { return }
            handle(event)
        default:
            break
        }
    }

    /// The helper offers a permission an adapter answers through its API (OpenCode's "Allow
    /// once / Always allow / Deny" question) as an approval titled with its prompt, Allow once
    /// and Deny only, so the Lock Screen, the Watch and the sessions list can answer it; it
    /// closes once the question is answered. Recordings made before the helper did so have
    /// only the question, so the replay offers it the same way.
    private func offer(_ item: HarnessItem) {
        guard case .question(let body) = item.body, body.questions.count == 1, let question = body.questions.first,
              let approve = question.options.first(where: { $0.id == "once" }),
              let deny = question.options.first(where: { $0.id == "reject" }) else { return }
        if body.answered?.isEmpty == false {
            guard let request = offered.removeValue(forKey: item.id) else { return }
            handle(["kind": "approval.closed", "requestId": request])
            return
        }
        guard offered[item.id] == nil else { return }
        let request = "rq_" + item.id.prefix(while: { $0 != "~" })
        offered[item.id] = request
        let approval = Approval(
            requestId: request, sessionId: session, bib: nil, agent: script.agent?.id ?? "shell", source: .ask,
            title: question.prompt, detail: nil, snippet: "",
            options: [ApprovalOption(n: 1, label: approve.label, role: .approve), ApprovalOption(n: 2, label: deny.label, role: .deny)],
            allowText: false, createdAt: Date(), expiresAt: nil
        )
        guard let data = try? JSONEncoder().encode(approval),
              let object = try? JSONSerialization.jsonObject(with: data) else { return }
        handle(["kind": "approval.open", "approval": object])
    }

    private func handle(_ event: [String: Any]) {
        onPlayback?(.event(event))
        switch event["kind"] as? String {
        case "approval.open":
            guard let body = event["approval"],
                  let data = try? JSONSerialization.data(withJSONObject: body),
                  let approval = try? decoder.decode(Approval.self, from: data) else { return }
            onApproval?(.open(approval), session)
        case "approval.closed":
            guard let id = event["requestId"] as? String else { return }
            onApproval?(.closed(requestID: id), session)
        default:
            break
        }
    }
}
