#if DEBUG
import Foundation
import TogoKit

/// A strict fixture preserves the request/response pairs and intervening events. A tape
/// for this runner has one outstanding request at a time. Concurrent wire scenarios belong
/// at the transport seam until this runner can preserve their causal ordering.
struct StrictReplayTape: Sendable {
    enum Failure: Error, Equatable {
        case invalidFrame(line: Int)
        case missingReply(line: Int)
        case unexpectedCall(method: String, expectedLine: Int?)
        case unconsumedCall(line: Int)
        case invalidReply(line: Int)
        case invalidEvent(line: Int)
    }

    enum Reply: Sendable {
        case value(JSONValue)
        case error(RPCErrorObject)
    }

    struct Event: Sendable {
        let method: String
        let params: [String: JSONValue]
        let line: Int
    }

    struct Exchange: Sendable {
        let method: String
        let params: [String: JSONValue]
        let line: Int
        var reply: Reply?
        var beforeReply: [Event] = []
        var afterReply: [Event] = []
    }

    let exchanges: [Exchange]

    init(jsonl: String) throws {
        let decoder = JSONDecoder()
        var exchanges: [Exchange] = []
        var outstanding: JSONValue?
        for (index, raw) in jsonl.split(separator: "\n", omittingEmptySubsequences: false).enumerated() {
            let line = index + 1
            let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
            if text.isEmpty || text.hasPrefix("//") { continue }
            guard let frame = try? decoder.decode([String: JSONValue].self, from: Data(text.utf8)),
                  frame["jsonrpc"] == .string("2.0") else { throw Failure.invalidFrame(line: line) }
            let id = frame["id"]
            if case .string(let method) = frame["method"] {
                guard frame["result"] == nil, frame["error"] == nil,
                      case .object(let params) = frame["params"] else { throw Failure.invalidFrame(line: line) }
                if let id {
                    switch id {
                    case .number, .string: break
                    default: throw Failure.invalidFrame(line: line)
                    }
                    guard outstanding == nil else { throw Failure.invalidFrame(line: line) }
                    outstanding = id
                    exchanges.append(Exchange(method: method, params: params, line: line))
                } else {
                    let event = Event(method: method, params: params, line: line)
                    guard let last = exchanges.indices.last else { throw Failure.invalidEvent(line: line) }
                    if outstanding == nil { exchanges[last].afterReply.append(event) }
                    else { exchanges[last].beforeReply.append(event) }
                }
            } else {
                guard let id, id == outstanding, let last = exchanges.indices.last,
                      frame["method"] == nil else { throw Failure.invalidFrame(line: line) }
                switch (frame["result"], frame["error"]) {
                case (let result?, nil): exchanges[last].reply = .value(result)
                case (nil, let error?):
                    guard let data = try? JSONEncoder().encode(error),
                          let payload = try? decoder.decode(RPCErrorObject.self, from: data) else {
                        throw Failure.invalidFrame(line: line)
                    }
                    exchanges[last].reply = .error(payload)
                default: throw Failure.invalidFrame(line: line)
                }
                outstanding = nil
            }
        }
        if outstanding != nil, let last = exchanges.last { throw Failure.missingReply(line: last.line) }
        self.exchanges = exchanges
    }
}

/// Test assertions can detect divergence even when a caller has caught an error and fallen
/// back. Diagnostics report the expected line and the app's method, never parameter values.
@MainActor final class StrictReplayRun {
    typealias Failure = StrictReplayTape.Failure

    private let tape: StrictReplayTape
    private var cursor = 0
    private var failure: Failure?

    init(tape: StrictReplayTape) { self.tape = tape }

    func take(_ method: String, params: some Encodable) throws -> StrictReplayTape.Exchange {
        if let failure { throw failure }
        let expected = next
        guard let expected, expected.method == method,
              let data = try? JSONEncoder().encode(params),
              let actual = try? JSONDecoder().decode([String: JSONValue].self, from: data),
              expected.params == actual else {
            throw record(.unexpectedCall(method: method, expectedLine: expected?.line))
        }
        cursor += 1
        return expected
    }

    func value<Value: Decodable>(_ exchange: StrictReplayTape.Exchange) throws -> Value {
        switch exchange.reply {
        case .error(let error): throw HelperError.rpc(error)
        case .value(let value):
            if let data = try? JSONEncoder().encode(value), let decoded = try? JSONDecoder().decode(Value.self, from: data) {
                return decoded
            }
            throw record(.invalidReply(line: exchange.line))
        case nil: throw record(.missingReply(line: exchange.line))
        }
    }

    @discardableResult func record(_ error: Failure) -> Failure {
        if failure == nil { failure = error }
        return failure ?? error
    }

    func verifyConsumed() throws {
        if let failure { throw failure }
        if let next { throw Failure.unconsumedCall(line: next.line) }
    }

    private var next: StrictReplayTape.Exchange? {
        tape.exchanges.indices.contains(cursor) ? tape.exchanges[cursor] : nil
    }
}
#endif
