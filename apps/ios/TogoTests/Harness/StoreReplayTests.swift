import Foundation
import Testing
import TogoKit
@testable import Togo

/// The recorded helper streams (protocol/v1/replays) through the real `HarnessSessionStore`:
/// what the chat, the live working line and the mode chip end on. TogoKit's replay suite checks
/// the feed itself step by step (scripts/check-replay.sh); this checks the app layer on top of
/// it, so it needs an iOS test build (run with `scripts/check-replay.sh --app`).
@MainActor
struct StoreReplayTests {
    struct Recording {
        var name: String
        var complete: Bool
        var descriptor: HarnessDescriptor
        var open: HarnessOpenResult
        var notes: [HarnessNotification]
    }

    nonisolated static let directory: URL = {
        var url = URL(filePath: #filePath)
        for _ in 0..<5 { url.deleteLastPathComponent() }
        return url.appending(path: "protocol/v1/replays", directoryHint: .isDirectory)
    }()

    /// Recorded fixtures with one open: the hand-written ones reopen and are covered in TogoKit.
    nonisolated static var names: [String] {
        let files = (try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)) ?? []
        return files.map { $0.deletingPathExtension().lastPathComponent }
            .filter { !$0.hasPrefix("synthetic.") }.sorted()
    }

    static func load(_ name: String) throws -> Recording? {
        let text = try String(contentsOf: directory.appending(path: "\(name).jsonl"), encoding: .utf8)
        let lines = text.split(separator: "\n")
        guard let first = lines.first,
              let header = try JSONSerialization.jsonObject(with: Data(first.utf8)) as? [String: Any] else { return nil }
        var descriptor: HarnessDescriptor?
        var open: HarnessOpenResult?
        var notes: [HarnessNotification] = []
        let decoder = JSONDecoder()
        for line in lines.dropFirst() {
            guard let record = try JSONSerialization.jsonObject(with: Data(line.utf8)) as? [String: Any],
                  record["dir"] as? String == "in", let method = record["method"] as? String,
                  let frame = record["frame"] as? [String: Any] else { continue }
            func decode<T: Decodable>(_ key: String, _: T.Type) throws -> T? {
                guard let value = frame[key] else { return nil }
                return try decoder.decode(T.self, from: JSONSerialization.data(withJSONObject: value))
            }
            switch method {
            case "harness.describe": descriptor = try decode("result", HarnessDescriptor.self) ?? descriptor
            case "harness.open" where open == nil: open = try decode("result", HarnessOpenResult.self)
            case "harness" where open != nil: if let note = try decode("params", HarnessNotification.self) { notes.append(note) }
            default: break
            }
        }
        guard let descriptor, let open else { return nil }
        return Recording(name: name, complete: header["complete"] as? Bool ?? false, descriptor: descriptor, open: open, notes: notes)
    }

    @Test("Store ends settled", arguments: names)
    func storeEndsSettled(_ name: String) async throws {
        // Without the chat capability the app never opens the feed: nothing to replay here.
        guard let recording = try? Self.load(name), recording.descriptor.has(HarnessCapability.chat) else { return }
        let services = FakeHarnessServices(descriptor: recording.descriptor)
        services.feeds = [.init(elements: [.opened(recording.open)] + recording.notes.map(HarnessFeed.note), end: .hold)]
        let chat = HarnessSessionStore(subject: ChatTests.subject, services: services, retry: (.milliseconds(5), .milliseconds(20)))
        chat.launch()
        defer { chat.stop() }
        let lastSeq = ([recording.open.cursor] + recording.notes.flatMap { ($0.items ?? []).map(\.seq) }).max() ?? 0
        let lastRev = ([recording.open.mission.rev] + recording.notes.compactMap { $0.mission?.rev }).max() ?? 0
        try await until { chat.timeline.cursor >= lastSeq && (chat.feed.mission.mission?.rev ?? -1) >= lastRev }

        let ids = chat.rows.map(\.id)
        #expect(Set(ids).count == ids.count, "\(name): duplicate rows \(ids)")
        // The chip shows what the status line said last, else what describe said.
        let status = ModeMemory.status(chat.feed.mission.mission) ?? ""
        if !status.isEmpty { #expect(chat.currentMode == status, "\(name): chip \(chat.currentMode), status \(status)") }
        // The live line follows the helper: once it says idle (and nothing was sent from here),
        // the line is gone.
        if !chat.working {
            #expect(chat.liveWork == nil, "\(name): the live line shows on an idle session: \(String(describing: chat.liveWork))")
        }
        if recording.complete {
            #expect(!chat.working, "\(name): working at the end of a settled recording")
        }
    }
}

extension StoreReplayTests {
    private func until(_ condition: () -> Bool, timeout: Duration = .seconds(3)) async throws {
        let clock = ContinuousClock()
        let deadline = clock.now + timeout
        while !condition() {
            guard clock.now < deadline else {
                Issue.record("timed out")
                return
            }
            try await Task.sleep(for: .milliseconds(5))
        }
    }
}
