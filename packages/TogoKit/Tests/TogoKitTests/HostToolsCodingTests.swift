import Foundation
import Testing
@testable import TogoKit

struct HostToolsCodingTests {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try JSONDecoder().decode(type, from: Data(json.utf8))
    }

    private func json(_ value: some Encodable) throws -> JSONValue {
        try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(value))
    }

    private static let sessionBase = """
        "id":"s_abcdefghij","bib":"01","name":"Claude Code","agent":"claude","cwd":"/home/u",\
        "createdAt":"2026-11-03T12:00:00Z","activity":null,"attachedClients":0,"managed":true,\
        "muted":false,"meta":{"model":null,"contextPercent":null,"costUsd":null}
        """

    @Test func endedFailedStatusesDecode() throws {
        for (raw, status) in [("ended", SessionStatus.ended), ("failed", .failed)] {
            let session = try decode(Session.self, "{\(Self.sessionBase),\"status\":\"\(raw)\"}")
            #expect(session.status == status)
            #expect(SessionStatus.known.contains(status))
        }
        #expect(try decode(SessionStatus.self, #""paused""#) == .unknown("paused"))
    }

    @Test func sessionExitDecodes() throws {
        let body = #"{\#(Self.sessionBase),"status":"failed","command":["claude","--model","opus"],"#
            + #""exit":{"code":null,"reason":"usage_limit","detail":"Usage limit reached"}}"#
        let session = try decode(Session.self, body)
        #expect(session.command == ["claude", "--model", "opus"])
        #expect(session.exit == SessionExit(code: nil, reason: .usageLimit, detail: "Usage limit reached"))
        #expect(try json(session) == decode(JSONValue.self, body))

        let restart = try decode(SessionExit.self, #"{"code":1,"reason":"restart","detail":null}"#)
        #expect(restart.reason == .restart && restart.code == 1)
        #expect(try decode(SessionExit.self, #"{"code":0,"reason":"signal","detail":null}"#).reason == .unknown("signal"))
    }

    @Test func sessionWithoutLaunchKeysEncodesWithout() throws {
        let session = try decode(Session.self, "{\(Self.sessionBase),\"status\":\"idle\"}")
        #expect(session.command == nil && session.exit == nil)
        guard case .object(let fields) = try json(session) else { Issue.record("not an object"); return }
        #expect(fields["command"] == nil && fields["exit"] == nil)

        let nulls = try decode(Session.self, "{\(Self.sessionBase),\"status\":\"idle\",\"command\":null,\"exit\":null}")
        #expect(nulls == session)
        guard case .object(let kept) = try json(nulls) else { Issue.record("not an object"); return }
        #expect(kept["command"] == .null && kept["exit"] == .null)
    }

    @Test func createParamsOmitNilProgramAndArgs() throws {
        guard case .object(let plain) = try json(CreateSessionParams(agent: "claude", cwd: "~/p")) else {
            Issue.record("not an object"); return
        }
        #expect(plain["program"] == nil && plain["args"] == nil)
        let custom = try json(CreateSessionParams(agent: "custom", cwd: "~/p", program: "aider", args: ["--x"]))
        guard case .object(let fields) = custom else { Issue.record("not an object"); return }
        #expect(fields["program"] == .string("aider"))
        #expect(fields["args"] == .array([.string("--x")]))
    }

    @Test func awakeEventDecodes() throws {
        let body = """
            {"seq":9,"ts":"2026-11-03T12:00:02Z","kind":"awake","sessionId":null,"awake":\
            {"mode":"battery","held":true,"reason":"on_battery","power":"battery","batteryPercent":64,\
            "batteryFloor":20,"platformSupported":true,"needsAdmin":false}}
            """
        let envelope = try decode(EventEnvelope.self, body)
        let expected = AwakeStatus(
            mode: .battery, held: true, reason: .onBattery, power: "battery", batteryPercent: 64,
            batteryFloor: 20, platformSupported: true, needsAdmin: false
        )
        #expect(envelope.event == .awake(expected))
        #expect(try json(envelope) == decode(JSONValue.self, body))
    }

    @Test func awakeEnumsKeepUnknownValues() throws {
        #expect(AwakeReason.known.count == 10)
        #expect(try decode(AwakeReason.self, #""needs_admin_setup""#) == .needsAdminSetup)
        #expect(try decode(AwakeReason.self, #""battery_floor""#) == .batteryFloor)
        #expect(try decode(AwakeReason.self, #""later""#) == .unknown("later"))
        #expect(try decode(AwakeMode.self, #""sometimes""#) == .unknown("sometimes"))
    }

    @Test func refusedErrorReadsReason() async throws {
        let (channel, client) = try await readyClient(capabilities: [Capability.awake])
        let task = Task { try await client.setAwake(mode: .battery, batteryFloor: nil) }
        let request = try #require(await channel.nextRequest())
        #expect(request.method == "awake.set")
        #expect(request.params["mode"] == .string("battery") && request.params["batteryFloor"] == nil)
        await channel.fail(
            request.id, code: RPCErrorCode.refused, message: "refused",
            data: #"{"reason":"needs_admin_setup"}"#
        )
        await #expect(throws: HelperError.refused("needs_admin_setup")) { try await task.value }
        #expect(HelperError.refused("needs_admin_setup").localizedDescription.hasSuffix("."))
    }

    /// The helper names why it refused in `data.reason`; anything else there reads as none.
    @Test func rpcErrorReadsReason() {
        #expect(RPCErrorObject(code: -32006, message: "x", data: .object(["reason": .string("running")])).reason == "running")
        #expect(RPCErrorObject(code: -32006, message: "x", data: .object(["reason": .number(4)])).reason == nil)
        #expect(RPCErrorObject(code: -32006, message: "x", data: .string("running")).reason == nil)
        #expect(RPCErrorObject(code: -32006, message: "x").reason == nil)
    }

    @Test func conflictReasonsPassThrough() async throws {
        let (channel, client) = try await readyClient(capabilities: [Capability.reply, Capability.launch])
        let reply = Task { try await client.sendText(id: "s_abcdefghij", text: "go", submit: true) }
        let request = try #require(await channel.nextRequest())
        #expect(request.params["submit"] == .bool(true) && request.params["text"] == .string("go"))
        await channel.fail(request.id, code: RPCErrorCode.conflict, message: "conflict", data: #"{"reason":"needs_you"}"#)
        await #expect(throws: HelperError.conflict("needs_you")) { try await reply.value }

        let relaunch = Task { try await client.relaunch(id: "s_abcdefghij") }
        let second = try #require(await channel.nextRequest())
        await channel.fail(second.id, code: RPCErrorCode.conflict, message: "conflict", data: #"{"reason":"running"}"#)
        await #expect(throws: HelperError.conflict("running")) { try await relaunch.value }
    }

    @Test func missingCapabilityIsUnsupported() async throws {
        let (_, client) = try await readyClient(capabilities: [])
        await #expect(throws: HelperError.unsupported(capability: Capability.notify)) { try await client.notifySettings() }
        await #expect(throws: HelperError.unsupported(capability: Capability.hostInfo)) { try await client.hostInfo() }
    }

    @Test func clientMethodsSendAndDecode() async throws {
        let all = [
            Capability.launch, Capability.restore, Capability.notify, Capability.authkeys, Capability.hostInfo,
            Capability.awake,
        ]
        let (channel, client) = try await readyClient(capabilities: all)

        let check = Task { try await client.checkAgent(program: "aider") }
        var request = try #require(await channel.nextRequest())
        #expect(request.method == "agents.check" && request.params["program"] == .string("aider"))
        await channel.reply(to: request.id, result: #"{"installed":true,"path":"/bin/aider"}"#)
        #expect(try await check.value == AgentCheck(installed: true, path: "/bin/aider"))

        let notify = Task { try await client.setNotify(NotifyPatch(muted: true, remindMinutes: 0)) }
        request = try #require(await channel.nextRequest())
        #expect(request.method == "notify.set")
        #expect(request.params == ["muted": .bool(true), "remindMinutes": .number(0)])
        await channel.reply(to: request.id, result: #"{"idleSeconds":6,"muted":true,"remindMinutes":0}"#)
        #expect(try await notify.value == NotifySettings(idleSeconds: 6, muted: true, remindMinutes: 0))

        let removed = Task { try await client.removeAuthorizedKey(publicKey: "ssh-ed25519 AAAA") }
        request = try #require(await channel.nextRequest())
        #expect(request.method == "authkeys.remove")
        await channel.reply(to: request.id, result: #"{"removed":2}"#)
        #expect(try await removed.value == 2)

        let restore = Task { try await client.restore(id: "s_abcdefghij", resume: false) }
        request = try #require(await channel.nextRequest())
        #expect(request.method == "session.restore" && request.params["resume"] == .bool(false))
        await channel.reply(to: request.id, result: "{\(Self.sessionBase),\"status\":\"idle\"}")
        #expect(try await restore.value.id == "s_abcdefghij")

        let list = Task { try await client.restorable() }
        request = try #require(await channel.nextRequest())
        #expect(request.method == "session.restorable")
        await channel.reply(to: request.id, result: #"{"sessions":[]}"#)
        #expect(try await list.value.isEmpty)

        let dismiss = Task { try await client.dismissRestorable(id: "s_abcdefghij") }
        request = try #require(await channel.nextRequest())
        #expect(request.method == "session.dismiss")
        await channel.reply(to: request.id, result: "{}")
        try await dismiss.value

        let info = Task { try await client.hostInfo() }
        request = try #require(await channel.nextRequest())
        await channel.reply(to: request.id, result: #"{"interfaces":[],"wakeOnLan":null}"#)
        #expect(try await info.value == HostInfo(interfaces: [], wakeOnLan: nil))

        let status = Task { try await client.awakeStatus() }
        request = try #require(await channel.nextRequest())
        #expect(request.method == "awake.status")
        await channel.reply(
            to: request.id,
            result: #"{"mode":"off","held":false,"reason":"off","power":"ac","batteryFloor":20,"platformSupported":true,"needsAdmin":false}"#
        )
        #expect(try await status.value.mode == .off)
    }

    private func readyClient(capabilities: [String]) async throws -> (PipeChannel, HelperClient) {
        let channel = PipeChannel()
        let client = HelperClient(channel: channel, callTimeout: .seconds(5))
        async let accepted: Void = channel.acceptHello(capabilities: capabilities)
        _ = try await client.hello(client: HelperClient.testClient)
        await accepted
        return (channel, client)
    }
}

/// The error exchanges in the shared fixtures, replayed through a real `HelperClient`.
struct HostToolsFixtureReplayTests {
    private func response(_ name: String) throws -> String {
        let url = FixtureTests.fixturesDirectory.appending(path: name)
        guard case .object(let fields) = try JSONDecoder().decode(JSONValue.self, from: Data(contentsOf: url)),
              let response = fields["response"]
        else { throw CocoaError(.fileReadCorruptFile) }
        return String(decoding: try JSONEncoder().encode(response), as: UTF8.self)
    }

    private func replay(_ fixture: String, capability: String, _ call: @escaping @Sendable (HelperClient) async throws -> Void) async throws -> (any Error)? {
        let channel = PipeChannel()
        let client = HelperClient(channel: channel, callTimeout: .seconds(5))
        async let accepted: Void = channel.acceptHello(capabilities: [capability])
        _ = try await client.hello(client: HelperClient.testClient)
        await accepted
        let raw = try response(fixture)
        let task = Task { try await call(client) }
        let request = try #require(await channel.nextRequest())
        // The fixture's response carries its own id; answer under the real one.
        let body = raw.replacingOccurrences(of: #""id":\#(2)"#, with: #""id":\#(request.id)"#)
            .replacingOccurrences(of: #""id":\#(3)"#, with: #""id":\#(request.id)"#)
        await channel.sendJSON(body)
        do { try await task.value; return nil } catch { return error }
    }

    @Test func awakeNeedsAdminIsRefused() async throws {
        let error = try await replay("awake.set-needs-admin.json", capability: Capability.awake) {
            _ = try await $0.setAwake(mode: .battery, batteryFloor: nil)
        }
        #expect(error as? HelperError == .refused("needs_admin_setup"))
    }

    @Test func relaunchWhileRunningConflicts() async throws {
        let error = try await replay("session.relaunch-running.json", capability: Capability.launch) {
            _ = try await $0.relaunch(id: "s_abcdefghij")
        }
        #expect(error as? HelperError == .conflict("running"))
    }

    @Test func replyToShellConflicts() async throws {
        let error = try await replay("session.sendText-shell.json", capability: Capability.reply) {
            try await $0.sendText(id: "s_abcdefghij", text: "Run the tests again", submit: true)
        }
        #expect(error as? HelperError == .conflict("shell"))
    }

    @Test func symlinkedAuthorizedKeysConflicts() async throws {
        let error = try await replay("authkeys.remove-symlink.json", capability: Capability.authkeys) {
            _ = try await $0.removeAuthorizedKey(publicKey: "ssh-ed25519 AAAA")
        }
        #expect(error as? HelperError == .conflict("symlink"))
    }

    @Test func successfulFixtureResultsDecodeThroughTheClient() async throws {
        let error = try await replay("session.sendText.json", capability: Capability.reply) {
            try await $0.sendText(id: "s_abcdefghij", text: "Run the tests again", submit: true)
        }
        #expect(error == nil)
    }
}
