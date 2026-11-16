import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct ActionFailureTests {
    private func busy() throws -> HelperError {
        .rpc(try JSONDecoder().decode(RPCErrorObject.self, from: Data(
            #"{"code":-32006,"message":"conflict","data":{"reason":"busy"}}"#.utf8
        )))
    }

    @Test func busyIsNeverReportedAsAlreadyAnswered() throws {
        let expected = "The agent is busy. Try again after this reply."
        #expect(try busy().localizedDescription == expected)
        #expect(HelperError.conflict("busy").localizedDescription == expected)
    }

    @Test func aBusyPrimitiveKeepsTheReason() async throws {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        services.actionError = try busy()
        let store = HarnessSessionStore(subject: ChatTests.subject, services: services)
        let context = PrimitiveContext.store(store, openScreen: { _ in }, open: { _ in }, showTerminal: {})
        let result = await context.run("set_model", nil, nil)
        #expect(result?.result == .failed)
        #expect(result?.message == "The agent is busy. Try again after this reply.")
    }

}
