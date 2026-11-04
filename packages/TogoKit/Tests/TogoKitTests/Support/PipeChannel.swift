import Foundation
import Synchronization
import TogoKit

/// An in-memory `ExecChannel` whose far end is driven by the test as a fake helper.
///
/// Frames the client writes are decoded and handed out through `nextRequest()`; the
/// test answers with `reply`, `fail` or raw bytes in any order it likes.
final class PipeChannel: ExecChannel {
    struct Request: Sendable {
        let id: Int
        let method: String
        let params: [String: JSONValue]
    }

    let incoming = ByteStream()
    private let decoder = Mutex(FrameDecoder())
    private let queue = Mutex(RequestQueue())

    var isClosed: Bool { queue.withLock { $0.finished } }

    func write(_ data: Data) async throws {
        let frames = try decoder.withLock { try $0.append(data) }
        for frame in frames {
            let message = try JSONDecoder().decode(WireRequest.self, from: frame)
            let request = Request(id: message.id, method: message.method, params: message.params ?? [:])
            let waiter: CheckedContinuation<Request?, Never>? = queue.withLock { queue in
                if let waiter = queue.waiter {
                    queue.waiter = nil
                    return waiter
                }
                queue.buffer.append(request)
                return nil
            }
            waiter?.resume(returning: request)
        }
    }

    func close() async {
        let waiter = queue.withLock { queue in
            queue.finished = true
            defer { queue.waiter = nil }
            return queue.waiter
        }
        incoming.finish()
        waiter?.resume(returning: nil)
    }

    /// The next request the client sent, or nil once the client closed the channel.
    /// Single consumer: the test body.
    func nextRequest() async -> Request? {
        await withCheckedContinuation { continuation in
            let ready: Request?? = queue.withLock { queue in
                if !queue.buffer.isEmpty { return .some(queue.buffer.removeFirst()) }
                if queue.finished { return .some(nil) }
                queue.waiter = continuation
                return .none
            }
            if case .some(let request) = ready { continuation.resume(returning: request) }
        }
    }

    func reply(to id: Int, result: String) async {
        await sendJSON(#"{"jsonrpc":"2.0","id":\#(id),"result":\#(result)}"#)
    }

    func fail(_ id: Int, code: Int, message: String) async {
        await sendJSON(#"{"jsonrpc":"2.0","id":\#(id),"error":{"code":\#(code),"message":"\#(message)"}}"#)
    }

    func sendJSON(_ json: String) async {
        await sendRaw(try! Frame.encode(Data(json.utf8)))
    }

    func sendRaw(_ bytes: Data) async {
        _ = await incoming.write(bytes)
    }

    /// Answers the next request, which must be `hello`, advertising `capabilities`.
    func acceptHello(capabilities: [String] = ["session.v1", "agents.v1", "fs.v1", "capture.v1"]) async {
        guard let request = await nextRequest(), request.method == "hello" else { return }
        let list = capabilities.map { "\"\($0)\"" }.joined(separator: ",")
        await reply(to: request.id, result: #"""
            {"protocol":1,"helper":{"version":"test","os":"darwin","arch":"arm64","hostname":"fake"},"capabilities":[\#(list)]}
            """#)
    }
}

private struct RequestQueue {
    var buffer: [PipeChannel.Request] = []
    var waiter: CheckedContinuation<PipeChannel.Request?, Never>?
    var finished = false
}

private struct WireRequest: Decodable {
    let id: Int
    let method: String
    let params: [String: JSONValue]?
}

extension HelperClient {
    static let testClient = ClientInfo(name: "togo-tests", version: "0")
}
