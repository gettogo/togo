#if DEBUG
import Foundation
import Synchronization
import TogoKit

/// `MachineTools` for previews and tests: every call runs against a scripted helper that
/// answers each method from `answer` and `fail`, and the requests it saw are recorded.
@MainActor final class FakeMachineTools: MachineTools {
    struct Request: Sendable, Equatable {
        let method: String
        let params: [String: JSONValue]
    }

    /// What each machine advertises; a machine not listed advertises `defaultCapabilities`.
    var capabilitySets: [HostID: Set<String>] = [:]
    var defaultCapabilities: Set<String> = []
    private(set) var calledHosts: [HostID] = []
    private let script = FakeHelperScript()

    var requests: [Request] { script.requests }

    func answer(_ method: String, _ result: String) {
        script.set(method, #""result":\#(result)"#)
    }

    func fail(_ method: String, code: Int, message: String = "refused", data: String? = nil) {
        let extra = data.map { #","data":\#($0)"# } ?? ""
        script.set(method, #""error":{"code":\#(code),"message":"\#(message)"\#(extra)}"#)
    }

    func capabilities(_ host: HostID) -> Set<String> {
        capabilitySets[host] ?? defaultCapabilities
    }

    func call<T: Sendable>(_ host: HostID, _ body: @Sendable (HelperClient) async throws -> T) async throws -> T {
        calledHosts.append(host)
        let client = HelperClient(channel: FakeHelperChannel(script: script, capabilities: capabilities(host)))
        defer { Task { await client.close() } }
        _ = try await client.hello(client: ClientInfo(name: "togo-preview", version: "0"))
        return try await body(client)
    }
}

final class FakeHelperScript: Sendable {
    private let state = Mutex<(answers: [String: String], requests: [FakeMachineTools.Request])>(([:], []))

    var requests: [FakeMachineTools.Request] { state.withLock { $0.requests } }

    func set(_ method: String, _ body: String) {
        state.withLock { $0.answers[method] = body }
    }

    func reply(id: Int, method: String, params: [String: JSONValue], capabilities: Set<String>) -> String {
        if method == "hello" {
            let list = capabilities.sorted().map { "\"\($0)\"" }.joined(separator: ",")
            return #"{"jsonrpc":"2.0","id":\#(id),"result":{"protocol":1,"helper":{"version":"preview","os":"darwin","arch":"arm64","hostname":"preview"},"capabilities":[\#(list)]}}"#
        }
        let body = state.withLock { state in
            state.requests.append(FakeMachineTools.Request(method: method, params: params))
            return state.answers[method] ?? #""result":{}"#
        }
        return #"{"jsonrpc":"2.0","id":\#(id),\#(body)}"#
    }
}

/// Answers every frame the client writes with the script's reply for its method.
final class FakeHelperChannel: ExecChannel {
    let incoming = ByteStream()
    private let decoder = Mutex(FrameDecoder())
    private let script: FakeHelperScript
    private let capabilities: Set<String>

    init(script: FakeHelperScript, capabilities: Set<String>) {
        self.script = script
        self.capabilities = capabilities
    }

    private struct Wire: Decodable {
        let id: Int
        let method: String
        let params: [String: JSONValue]?
    }

    func write(_ data: Data) async throws {
        for frame in try decoder.withLock({ try $0.append(data) }) {
            let request = try JSONDecoder().decode(Wire.self, from: frame)
            let reply = script.reply(
                id: request.id, method: request.method, params: request.params ?? [:], capabilities: capabilities
            )
            _ = await incoming.write(try Frame.encode(Data(reply.utf8)))
        }
    }

    func close() async {
        incoming.finish()
    }
}
#endif
