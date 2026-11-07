import Foundation
@testable import TogoKit

/// A recorded helper stream for one session (protocol/v1/replays/*.jsonl), as written by
/// `helper/cmd/togo-record` or by hand: a header line, then one line per frame the app sent
/// ("out") or received ("in"), each with milliseconds since the recording began.
struct ReplayFixture: Sendable {
    struct Header: Decodable, Sendable {
        var agent: String
        var scenario: String
        var session: String?
        var complete: Bool?
        var note: String?
        /// Agent config files the scenario changed (the recorder restored them).
        var configChanged: [String]?
        /// Hand-written fixtures say what they model.
        var description: String?
    }

    /// One frame, decoded into what the app does with it.
    enum Step: Sendable {
        /// events.subscribe answered: the snapshot the monitor starts from.
        case subscribed(EventSnapshot)
        case event(EventEnvelope)
        case described(HarnessDescriptor)
        /// harness.open answered.
        case opened(HarnessOpenResult)
        case harness(HarnessNotification)
        /// harness.send answered: the prompt the store shows before its notification.
        case sent(HarnessItem)
        /// A request the app made (approval.answer, harness.interrupt, harness.action…).
        case request(method: String, params: JSONValue?)
        /// Any other answer; `error` holds the error message when it failed.
        case answer(method: String, error: String?)
    }

    struct Record: Sendable {
        /// Milliseconds since the recording began.
        var t: Int
        var step: Step
    }

    var name: String
    var header: Header
    var records: [Record]
    /// Frames the app's models could not decode: (method, why). The app fails the call that
    /// carried them.
    var undecodable: [(method: String, why: String)] = []

    /// The repo's protocol/v1/replays folder; this file sits five levels below the root.
    static let directory: URL = {
        var url = URL(filePath: #filePath)
        for _ in 0..<6 { url.deleteLastPathComponent() }
        return url.appending(path: "protocol/v1/replays", directoryHint: .isDirectory)
    }()

    /// Every fixture's name (the file name without .jsonl), sorted.
    static var all: [String] {
        let files = (try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)) ?? []
        return files.filter { $0.pathExtension == "jsonl" }.map { $0.deletingPathExtension().lastPathComponent }.sorted()
    }

    static func load(_ name: String) throws -> ReplayFixture {
        let data = try Data(contentsOf: directory.appending(path: "\(name).jsonl"))
        return try parse(name: name, data)
    }

    static func parse(name: String, _ data: Data) throws -> ReplayFixture {
        let lines = data.split(separator: UInt8(ascii: "\n")).filter { !$0.isEmpty }
        guard let first = lines.first else { throw ReplayError.empty(name) }
        let decoder = JSONDecoder()
        let header = try decoder.decode(Header.self, from: Data(first))
        var records: [Record] = []
        var undecodable: [(method: String, why: String)] = []
        for (index, line) in lines.dropFirst().enumerated() {
            let raw: RawLine
            do {
                raw = try decoder.decode(RawLine.self, from: Data(line))
            } catch {
                throw ReplayError.badLine(name, index + 2, String(describing: error))
            }
            do {
                if let step = try decode(raw, decoder: decoder) { records.append(Record(t: raw.t, step: step)) }
            } catch {
                // The app would fail this call: kept as a failed answer, and reported.
                undecodable.append((raw.method, "line \(index + 2): \(error)"))
                records.append(Record(t: raw.t, step: .answer(method: raw.method, error: "undecodable")))
            }
        }
        var fixture = ReplayFixture(name: name, header: header, records: records)
        fixture.undecodable = undecodable
        return fixture
    }

    private struct RawLine: Decodable {
        var t: Int
        var dir: String
        var method: String
        var frame: JSONValue
    }

    private struct Frame<T: Decodable>: Decodable {
        var result: T?
        var params: T?
        var error: RPCErrorObject?
    }

    private struct SendResult: Decodable { var item: HarnessItem }

    private static func decode(_ raw: RawLine, decoder: JSONDecoder) throws -> Step? {
        let data = try JSONEncoder().encode(raw.frame)
        func frame<T: Decodable>(_: T.Type) throws -> Frame<T> { try decoder.decode(Frame<T>.self, from: data) }
        if raw.dir == "out" {
            guard case .object(let object) = raw.frame else { return nil }
            return .request(method: raw.method, params: object["params"])
        }
        let error = try? decoder.decode(Frame<JSONValue>.self, from: data).error
        if let error { return .answer(method: raw.method, error: error.message) }
        switch raw.method {
        case "events.subscribe": return try frame(EventSnapshot.self).result.map(Step.subscribed)
        case "event": return try frame(EventEnvelope.self).params.map(Step.event)
        case "harness.describe": return try frame(HarnessDescriptor.self).result.map(Step.described)
        case "harness.open": return try frame(HarnessOpenResult.self).result.map(Step.opened)
        case "harness": return try frame(HarnessNotification.self).params.map(Step.harness)
        case "harness.send": return try frame(SendResult.self).result.map { Step.sent($0.item) }
        default: return .answer(method: raw.method, error: nil)
        }
    }
}

enum ReplayError: Error, CustomStringConvertible {
    case empty(String)
    case badLine(String, Int, String)

    var description: String {
        switch self {
        case .empty(let name): "\(name): empty fixture"
        case .badLine(let name, let line, let why): "\(name):\(line): \(why)"
        }
    }
}
