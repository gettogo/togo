#if canImport(ActivityKit) && os(iOS)
import Foundation
import Testing
@testable import TogoPush

struct LiveModelsTests {
    private func decode(_ json: String) throws -> TogoActivityAttributes.ContentState {
        try JSONDecoder().decode(TogoActivityAttributes.ContentState.self, from: Data(json.utf8))
    }

    @Test func contentStateDecodesReason() throws {
        let state = try decode("""
        {"bib":"01","agent":"claude","status":"failed","waiting":0,"since":1793707200,\
        "requestId":null,"command":null,"reason":"usage_limit"}
        """)
        #expect(state.status == "failed")
        #expect(state.reason == "usage_limit")
    }

    @Test func contentStateWithoutReasonStillDecodes() throws {
        let state = try decode("""
        {"bib":"01","agent":"claude","status":"working","waiting":0,"since":1793707200,\
        "requestId":null,"command":null}
        """)
        #expect(state.status == "working")
        #expect(state.reason == nil)
    }

    @Test func initDefaultsReasonToNil() {
        let state = TogoActivityAttributes.ContentState(
            bib: nil, agent: "shell", status: "done", waiting: 0, since: 1, requestId: nil, command: nil)
        #expect(state.reason == nil)
    }
}
#endif
