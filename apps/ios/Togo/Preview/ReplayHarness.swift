#if DEBUG
import Foundation
import TogoKit

/// A recorded helper conversation, for UI tests: one protocol/v1 JSON-RPC message per line, in
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

    /// The first chapter after `released` that `trigger` releases.
    func chapter(for trigger: String, after released: Int) -> Int? {
        chapters.indices.first { $0 > released && chapters[$0].trigger == trigger }
    }

    static func load(_ name: String) -> ReplayScript? {
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
        if let data = script.opened, let opened = try? decoder.decode(HarnessOpenResult.self, from: data) {
            missionRev = opened.mission.rev
            continuation.yield(.opened(opened))
        }
        if released == 0 { play(0) }
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

    /// The control center answered a request: releases the recorded `approval.answer` chapter.
    func trigger(_ method: String) {
        guard let index = script.chapter(for: method, after: released) else { return }
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

    private func deliver(_ note: ReplayScript.Note) {
        switch note.method {
        case "harness":
            guard var notification = try? decoder.decode(HarnessNotification.self, from: note.params) else { return }
            if notification.mission != nil {
                missionRev += 1
                notification.mission?.rev = missionRev
            }
            for feed in feeds.values {
                notification.sessionId = feed.session
                feed.continuation.yield(.note(notification))
            }
        case "event":
            guard let event = try? JSONSerialization.jsonObject(with: note.params) as? [String: Any] else { return }
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
        default:
            break
        }
    }
}
#endif
