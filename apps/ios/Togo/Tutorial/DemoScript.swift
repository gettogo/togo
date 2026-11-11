import Foundation
import TogoKit

/// The demo machine's live session: a real recorded OpenCode turn (`demo-session.jsonl`) played
/// into the app's own chat by `ReplayHarnessServices`. Nothing runs until the person sends the
/// prompt waiting in the composer; each of the agent's three requests waits for an answer.
/// Watches what plays so the roster, the terminal and the review follow the chat.
@MainActor final class DemoScript {
    enum Phase: Equatable, Sendable {
        /// The prompt waits in the composer.
        case ready
        case working
        /// A request waits for an answer; what it asks.
        case waiting(String)
        case done
    }

    nonisolated static let resource = "demo-session"
    nonisolated static let sessionID = "demo-status"

    let replay: ReplayHarnessServices
    /// What the recorded person typed, waiting in the composer before anything runs.
    let prompt: String
    private(set) var phase: Phase = .ready
    private(set) var summary: MissionSummary?
    /// The paths the agent has written so far.
    private(set) var written: Set<String> = []
    /// The chat so far, latest version of each item, in order.
    private(set) var items: [HarnessItem] = []
    /// What the agent is doing now, for the roster's line.
    private(set) var activity: String?
    /// Called after every change, on the main actor.
    var onChange: [() -> Void] = []

    init(jsonl: String, longestGap: Double = 0.5) throws {
        replay = ReplayHarnessServices(script: try ReplayScript(jsonl: jsonl, longestGap: longestGap))
        prompt = Self.firstPrompt(jsonl) ?? ""
        replay.onPlayback = { [weak self] in self?.played($0) }
    }

    /// `longestGap` caps the wait between recorded steps, seconds.
    static func load(bundle: Bundle = .main, longestGap: Double = 0.5) -> DemoScript? {
        guard let url = bundle.url(forResource: resource, withExtension: "jsonl"),
              let text = try? String(contentsOf: url, encoding: .utf8) else { return nil }
        return try? DemoScript(jsonl: text, longestGap: longestGap)
    }

    /// The session's row in the demo machine's list, as the script has it now.
    func row(hostID: HostID, since: Date) -> SessionRow {
        let status: SessionStatus = switch phase {
        case .ready: .idle
        case .working: .working
        case .waiting: .needsYou
        case .done: .done
        }
        return SessionRow(
            id: Self.sessionID, hostID: hostID, bib: "01", name: "status", agent: "opencode", agentName: "OpenCode",
            status: status, activity: activity, cwd: "~/status", muted: false, managed: true, meta: SessionMeta(),
            statusSince: since, mission: summary
        )
    }

    // MARK: Playback

    private func played(_ step: ReplayHarnessServices.Playback) {
        switch step {
        case .released:
            phase = .working
        case .harness(let note):
            for item in note.items ?? [] { take(item) }
            for op in note.mission?.ops ?? [] { take(op) }
        case .finished:
            if !replay.canRelease("harness.send"), !replay.canRelease("harness.action:answer"), phase != .ready {
                if case .waiting = phase {} else { phase = .done }
            }
        case .event:
            return
        }
        onChange.forEach { $0() }
    }

    private func take(_ item: HarnessItem) {
        if let index = items.firstIndex(where: { $0.id == item.id }) {
            guard items[index].seq < item.seq else { return }
            items[index] = item
        } else {
            items.append(item)
        }
        switch item.body {
        case .tool(let tool):
            written.formUnion((tool.files ?? []).map(\.path))
            if phase == .working { activity = tool.title.isEmpty ? tool.name : tool.title }
        case .assistant(let text) where phase == .done || !text.partial:
            if let line = text.text.split(separator: "\n").first { activity = String(line) }
        default:
            break
        }
    }

    private func take(_ op: HarnessDelta.Op) {
        switch op.op {
        case .summary:
            guard let summary = op.summary else { return }
            self.summary = summary
            if summary.needsYou == 0, case .waiting = phase { phase = .working }
            if !summary.working, phase == .working, !replay.canRelease("harness.action:answer") { phase = .done }
        case .upsert where op.section == .needs:
            let ask = op.row?.subtitle ?? op.row?.title ?? ""
            phase = .waiting(ask)
            activity = ask
        case .replace where op.section == .changes:
            written.formUnion((op.rows ?? []).map(\.id))
        default:
            break
        }
    }

    /// The text of the recording's first `harness.send`.
    private static func firstPrompt(_ jsonl: String) -> String? {
        for line in jsonl.split(separator: "\n") where line.contains("\"harness.send\"") {
            guard let object = try? JSONSerialization.jsonObject(with: Data(line.utf8)) as? [String: Any],
                  let params = object["params"] as? [String: Any], let text = params["text"] as? String else { continue }
            return text
        }
        return nil
    }
}

/// Harness services for the demo machine: the scripted session plays `DemoScript`; any other
/// session has no chat. Once the recording has nothing left for a request (a second prompt, a
/// stop, a model change) it is refused with the demo's read-only message, never sent anywhere.
@MainActor final class DemoHarnessServices: HarnessServices {
    let script: DemoScript
    private let none = NoHarnessServices()
    /// Composer history in the demo's own defaults, never the person's.
    let composerMemory: CommandMemory

    init(script: DemoScript, defaults: UserDefaults) {
        self.script = script
        composerMemory = CommandMemory(defaults: defaults, prefix: "demo.composer")
    }

    private func scripted(_ s: HarnessSubject) -> Bool { s.sessionID == DemoScript.sessionID }

    func describe(_ s: HarnessSubject) async throws -> HarnessDescriptor {
        try await scripted(s) ? script.replay.describe(s) : none.describe(s)
    }

    func feed(_ s: HarnessSubject, after: Int) -> AsyncThrowingStream<HarnessFeed, any Error> {
        scripted(s) ? script.replay.feed(s, after: after) : none.feed(s, after: after)
    }

    func history(_ s: HarnessSubject, before: Int) async throws -> HarnessPage {
        try await scripted(s) ? script.replay.history(s, before: before) : none.history(s, before: before)
    }

    func screen(_ s: HarnessSubject, id: String, query: [String: String]?) async throws -> HarnessScreen {
        guard scripted(s) else { return try await none.screen(s, id: id, query: query) }
        return try await script.replay.screen(s, id: id, query: query)
    }

    func act(_ s: HarnessSubject, action: String, target: String?, args: [String: JSONValue]?) async throws -> HarnessActionResult {
        guard scripted(s), script.replay.canRelease("harness.action:\(action)") else { throw DemoServices.ReadOnly() }
        return try await script.replay.act(s, action: action, target: target, args: args)
    }

    func send(_ s: HarnessSubject, text: String, mode: HarnessSendParams.Mode, images: [String]) async throws -> HarnessItem {
        guard scripted(s), images.isEmpty, script.replay.canRelease("harness.send") else { throw DemoServices.ReadOnly() }
        return try await script.replay.send(s, text: text, mode: mode, images: images)
    }

    func upload(_ s: HarnessSubject, name: String, data: Data) async throws -> String { throw DemoServices.ReadOnly() }

    func image(_ s: HarnessSubject, path: String) async throws -> Data { throw HarnessUnavailable() }

    func interrupt(_ s: HarnessSubject) async throws -> HarnessActionResult { throw DemoServices.ReadOnly() }

    func complete(_ s: HarnessSubject, kind: HarnessCompleteParams.Kind, query: String) async throws -> [HarnessCompletion] {
        guard scripted(s) else { return [] }
        return try await script.replay.complete(s, kind: kind, query: query)
    }

    func locate(_ s: HarnessSubject, anchor: HarnessAnchor) async throws -> HarnessLocation {
        HarnessLocation(found: false, linesFromBottom: 0)
    }
}
