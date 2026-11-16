import Testing
import TogoKit
@testable import Togo

@MainActor struct CallbackTargetTests {
    @Test func anUnrelatedLoopbackPortCannotBeForwarded() {
        let listener = ListeningPort(port: 3000, address: "127.0.0.1", pid: 42, command: "agent")
        #expect(throws: CallbackTargetError.unverified) {
            try LiveCallbackTunnel.verifiedAddress(sessionID: "session", port: 8888, ports: [listener])
        }
        #expect(throws: CallbackTargetError.unverified) {
            try LiveCallbackTunnel.verifiedAddress(sessionID: nil, port: 3000, ports: [listener])
        }
        #expect(throws: CallbackTargetError.unverified) {
            try LiveCallbackTunnel.verifiedAddress(sessionID: "session", port: 3000, ports: [])
        }
    }

    @Test func verifiedSessionListenersRetainTheirLoopbackFamily() throws {
        for (address, expected) in [("127.0.0.1", "127.0.0.1"), ("::1", "::1"), ("0.0.0.0", "127.0.0.1")] {
            let port = ListeningPort(port: 3000, address: address, pid: 42, command: "agent")
            #expect(try LiveCallbackTunnel.verifiedAddress(sessionID: "session", port: 3000, ports: [port]) == expected)
        }
    }
}
