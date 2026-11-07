import Foundation
import Testing
@testable import TogoKit

struct FixtureTests {
    /// Tests/TogoKitTests/FixtureTests.swift sits four directories below the repo root.
    static let fixturesDirectory: URL = {
        var url = URL(filePath: #filePath)
        for _ in 0..<5 { url.deleteLastPathComponent() }
        return url.appending(path: "protocol/v1/fixtures", directoryHint: .isDirectory)
    }()

    @Test func everyFixtureDecodes() throws {
        let files = try Self.fixtures(in: Self.fixturesDirectory)
        #expect(files.count >= 40)
        try checkFixtures(files)
    }

    /// Pending exchanges the app handles before any helper serves them still
    /// round-trip through the app's types.
    static let appPendingFixtures: Set<String> = [
        "approval.list-subject.json", "push.pair-watch.json",
        "harness.screen-secret.json",
    ]

    @Test func pendingFixturesValidate() throws {
        let pending = Self.fixturesDirectory.appending(path: "pending", directoryHint: .isDirectory)
        guard FileManager.default.fileExists(atPath: pending.path) else { return }
        let files = try Self.fixtures(in: pending).filter { Self.appPendingFixtures.contains($0.lastPathComponent) }
        #expect(files.count == Self.appPendingFixtures.count)
        try checkFixtures(files)
    }

    private static func fixtures(in directory: URL) throws -> [URL] {
        try FileManager.default
            .contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)
            .filter { $0.pathExtension == "json" }
            .sorted { $0.lastPathComponent < $1.lastPathComponent }
    }

    private func checkFixtures(_ files: [URL]) throws {
        for file in files {
            let name = file.lastPathComponent
            let fixture = try JSONDecoder().decode(JSONValue.self, from: Data(contentsOf: file))
            guard case .object(let fields) = fixture,
                  let request = fields["request"], let response = fields["response"]
            else {
                Issue.record("\(name): missing request or response")
                continue
            }
            if case .array(let setup) = fields["setup"] {
                for step in setup { try checkRequest(step, fixture: name) }
            }
            let method = try checkRequest(request, fixture: name)
            try checkResponse(response, method: method, fixture: name)
        }
    }

    @Test func sessionStatusUnknownKept() throws {
        let decoder = JSONDecoder()
        #expect(try decoder.decode(SessionStatus.self, from: Data(#""paused""#.utf8)) == .unknown("paused"))
        #expect(try decoder.decode(SessionStatus.self, from: Data(#""needs_you""#.utf8)) == .needsYou)
        #expect(String(decoding: try JSONEncoder().encode(SessionStatus.needsYou), as: UTF8.self) == #""needs_you""#)
        #expect(String(decoding: try JSONEncoder().encode(SessionStatus.unknown("paused")), as: UTF8.self) == #""paused""#)
    }

    @Test func createdAtAcceptsFractionalSecondsAndOffsets() throws {
        func createdAt(_ stamp: String) throws -> Date {
            let json = """
            {"id":"s_abcdefghij","bib":"01","name":"Shell","agent":"shell","cwd":"/tmp",
             "createdAt":"\(stamp)","status":"idle","activity":null,"attachedClients":0,"managed":true}
            """
            return try JSONDecoder().decode(Session.self, from: Data(json.utf8)).createdAt
        }
        let whole = try createdAt("2026-11-03T12:00:00Z")
        #expect(whole == Date(timeIntervalSince1970: 1_793_707_200))
        #expect(try createdAt("2026-11-03T12:00:00.250Z") == whole.addingTimeInterval(0.25))
        #expect(try createdAt("2026-11-03T14:00:00+02:00") == whole)
        #expect(throws: DecodingError.self) { try createdAt("yesterday") }
    }

    @Test func createdAtEncodesWholeSeconds() throws {
        let session = Session(
            id: "s_abcdefghij", bib: "01", name: "Shell", agent: "shell", cwd: "/tmp",
            createdAt: Date(timeIntervalSince1970: 1_793_707_200.75), status: .idle,
            activity: nil, attachedClients: 0, managed: true
        )
        let encoded = try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(session))
        guard case .object(let fields) = encoded else {
            Issue.record("session did not encode as an object")
            return
        }
        #expect(fields["createdAt"] == .string("2026-11-03T12:00:00Z"))
        #expect(fields["activity"] == .null)
    }

    @Test func incomingDistinguishesNullIDFromMissingID() throws {
        let parseError = #"{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse error"}}"#
        let notification = #"{"jsonrpc":"2.0","method":"event","params":{}}"#
        let decoder = JSONDecoder()
        let error = try decoder.decode(RPCIncoming.self, from: Data(parseError.utf8))
        #expect(error.id == .null)
        #expect(error.error?.code == RPCErrorCode.parse)
        let event = try decoder.decode(RPCIncoming.self, from: Data(notification.utf8))
        #expect(event.id == nil)
        #expect(event.method == "event")
    }

    @Test func requestEncodesParamsAndOmitsMissingID() throws {
        let request = RPCRequest(id: 7, method: TogoKit.Method.sessionList.rawValue, params: EmptyParams())
        #expect(try roundTripJSON(request) == .object([
            "jsonrpc": .string("2.0"), "id": .number(7), "method": .string("session.list"), "params": .object([:]),
        ]))
        let notification = RPCRequest(id: nil, method: "event", params: EmptyParams())
        guard case .object(let fields) = try roundTripJSON(notification) else {
            Issue.record("request did not encode as an object")
            return
        }
        #expect(fields["id"] == nil)
    }

    // MARK: - Helpers

    @discardableResult
    private func checkRequest(_ request: JSONValue, fixture: String) throws -> TogoKit.Method? {
        guard case .object(let fields) = request, case .string(let name) = fields["method"] else {
            Issue.record("\(fixture): request has no method")
            return nil
        }
        guard let method = TogoKit.Method(rawValue: name) else {
            Issue.record("\(fixture): unknown method \(name)")
            return nil
        }
        let params = fields["params"] ?? .object([:])
        let reencoded = try roundTrip(Self.types(for: method).params, params)
        #expect(reencoded == params, "\(fixture): \(name) params")
        return method
    }

    private func checkResponse(_ response: JSONValue, method: TogoKit.Method?, fixture: String) throws {
        guard let method, case .object(let fields) = response else { return }
        let incoming = try JSONDecoder().decode(RPCIncoming.self, from: JSONEncoder().encode(response))
        #expect(incoming.id != nil, "\(fixture): response id")

        if let result = fields["result"] {
            #expect(incoming.result == result, "\(fixture): untyped result")
            let reencoded = try roundTrip(Self.types(for: method).result, result)
            #expect(reencoded == result, "\(fixture): \(method.rawValue) result")
        } else if case .object(let errorFields) = fields["error"], let error = fields["error"] {
            let decoded = try JSONDecoder().decode(RPCErrorObject.self, from: JSONEncoder().encode(error))
            #expect(errorFields["code"] == .number(Double(decoded.code)), "\(fixture): error code")
            #expect(incoming.error == decoded, "\(fixture): incoming error")
            #expect(try roundTripJSON(decoded) == error, "\(fixture): error round trip")
        } else {
            Issue.record("\(fixture): response has neither result nor error")
        }
    }

    private static func types(for method: TogoKit.Method) -> (params: any Codable.Type, result: any Codable.Type) {
        switch method {
        case .hello: (HelloParams.self, HelloResult.self)
        case .sessionList: (EmptyParams.self, SessionList.self)
        case .sessionCreate: (CreateSessionParams.self, Session.self)
        case .sessionKill: (SessionIDParams.self, EmptyResult.self)
        case .sessionRename: (RenameParams.self, Session.self)
        case .sessionAttachCommand: (SessionIDParams.self, AttachCommandResult.self)
        case .sessionCapture: (CaptureParams.self, CaptureResult.self)
        case .sessionSetMute: (SetMuteParams.self, Session.self)
        case .agentsList: (EmptyParams.self, AgentList.self)
        case .fsRecentDirs: (EmptyParams.self, RecentDirs.self)
        case .fsListDir: (ListDirParams.self, ListDirResult.self)
        case .fsRead: (FsReadParams.self, FsReadResult.self)
        case .fsUpload: (UploadParams.self, UploadResult.self)
        case .fsImage: (FsImageParams.self, FsImageResult.self)
        case .eventsSubscribe: (EventsSubscribeParams.self, EventSnapshot.self)
        case .eventsKeepalive: (EmptyParams.self, EmptyResult.self)
        case .approvalList: (ApprovalListParams.self, ApprovalList.self)
        case .approvalAnswer: (ApprovalAnswerParams.self, ApprovalAnswerResult.self)
        case .gitStatus: (SessionIDParams.self, GitStatus.self)
        case .gitBranches: (SessionIDParams.self, GitBranches.self)
        case .portsList: (SessionIDParams.self, PortList.self)
        case .gitDiff: (GitDiffParams.self, GitDiff.self)
        case .gitStage, .gitUnstage, .gitDiscard: (GitPathsParams.self, GitStatus.self)
        case .gitCommit: (GitCommitParams.self, GitCommitResult.self)
        case .gitLog: (GitLogParams.self, GitLog.self)
        case .pushPair: (PushPairParams.self, PushPairResult.self)
        case .pushUnpair: (PushUnpairParams.self, EmptyResult.self)
        case .agentsCheck: (AgentsCheckParams.self, AgentCheck.self)
        case .sessionRelaunch: (SessionIDParams.self, Session.self)
        case .sessionRestorable: (EmptyParams.self, RestorableList.self)
        case .sessionRestore: (RestoreParams.self, Session.self)
        case .sessionDismiss: (SessionIDParams.self, EmptyResult.self)
        case .sessionSendText: (SendTextParams.self, EmptyResult.self)
        case .notifyGet: (EmptyParams.self, NotifySettings.self)
        case .notifySet: (NotifyPatch.self, NotifySettings.self)
        case .authkeysRemove: (AuthkeysRemoveParams.self, AuthkeysRemoveResult.self)
        case .hostInfo: (EmptyParams.self, HostInfo.self)
        case .awakeStatus: (EmptyParams.self, AwakeStatus.self)
        case .awakeSet: (AwakeSetParams.self, AwakeStatus.self)
        case .browserOpenOnMac: (BrowserOpenParams.self, EmptyResult.self)
        case .harnessDescribe: (SessionIDParams.self, HarnessDescriptor.self)
        case .harnessOpen: (HarnessOpenParams.self, HarnessOpenResult.self)
        case .harnessClose: (SessionIDParams.self, EmptyResult.self)
        case .harnessHistory: (HarnessHistoryParams.self, HarnessPage.self)
        case .harnessScreen: (HarnessScreenParams.self, HarnessScreen.self)
        case .harnessAction: (HarnessActionParams.self, HarnessActionResult.self)
        case .harnessSend: (HarnessSendParams.self, HarnessSendResult.self)
        case .harnessInterrupt: (SessionIDParams.self, HarnessActionResult.self)
        case .harnessComplete: (HarnessCompleteParams.self, HarnessCompleteResult.self)
        case .harnessLocate: (HarnessLocateParams.self, HarnessLocation.self)
        case .integrationsStatus: (EmptyParams.self, IntegrationsStatus.self)
        case .integrationsInstall, .integrationsUninstall: (IntegrationsChangeParams.self, IntegrationsChange.self)
        }
    }

    private func roundTrip<T: Codable>(_ type: T.Type, _ value: JSONValue) throws -> JSONValue {
        let typed = try JSONDecoder().decode(T.self, from: JSONEncoder().encode(value))
        return try roundTripJSON(typed)
    }

    private func roundTripJSON(_ value: some Encodable) throws -> JSONValue {
        try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(value))
    }
}

