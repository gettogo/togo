import Foundation
import Testing
@testable import TogoKit

struct IntegrationsCodingTests {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try JSONDecoder().decode(type, from: Data(json.utf8))
    }

    private func json(_ value: some Encodable) throws -> JSONValue {
        try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(value))
    }

    @Test func reportDecodesEveryField() throws {
        let body = #"{"id":"claude","name":"Claude Code","installed":true,"setup":"hooks","state":"ready","#
            + #""hooks":"installed","statusLine":"chained","path":"/u/.claude/settings.json","#
            + #""backup":"/u/.claude/settings.json.togo-backup","lastHookAt":"2026-11-07T09:00:00Z","#
            + #""statusLineAt":"2026-11-07T09:00:30Z"}"#
        let report = try decode(IntegrationReport.self, body)
        #expect(report.state == .ready && report.setup == .hooks && report.statusLine == "chained")
        #expect(report.fix == nil)
        #expect(try json(report) == decode(JSONValue.self, body))
    }

    @Test func unknownWordsSurvive() throws {
        let report = try decode(
            IntegrationReport.self, #"{"id":"x","name":"X","installed":true,"setup":"plugin","state":"broken"}"#
        )
        #expect(report.setup == .unknown("plugin") && report.state == .unknown("broken"))
        #expect(try json(report) == decode(JSONValue.self, #"{"id":"x","name":"X","installed":true,"setup":"plugin","state":"broken"}"#))
    }

    @Test func changeParamsLeaveOutWhatIsUnset() throws {
        #expect(try json(IntegrationsChangeParams(agents: nil, block: nil)) == .object([:]))
        #expect(try json(IntegrationsChangeParams(agents: ["droid"], block: nil)) == decode(JSONValue.self, #"{"agents":["droid"]}"#))
    }

    @Test func methodNames() {
        #expect(Method.integrationsStatus.rawValue == "integrations.status")
        #expect(Method.integrationsInstall.rawValue == "integrations.install")
        #expect(Method.integrationsUninstall.rawValue == "integrations.uninstall")
        #expect(Capability.integrations == "integrations.v1")
    }
}
