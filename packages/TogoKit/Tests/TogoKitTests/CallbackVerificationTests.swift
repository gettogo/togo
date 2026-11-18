import Foundation
import Testing
@testable import TogoKit

@MainActor struct CallbackVerificationTests {
    @Test func unverifiedTargetFailsBeforeOpeningSSHOrBindingAListener() async {
        let tunnel = RejectingCallbackTunnel()
        let forward = CallbackForward(tunnel: tunnel, hostID: UUID(), sessionID: "test", port: 18888)
        await #expect(throws: Refused.unverified) { try await forward.start() }
        #expect(tunnel.opened == 0)
    }
}

private enum Refused: Error, Equatable { case unverified }

@MainActor private final class RejectingCallbackTunnel: CallbackTunnel {
    var opened = 0
    func opener(hostID: UUID, fresh: Bool) async throws -> any TCPChannelOpener {
        opened += 1
        throw Refused.unverified
    }

    func loopbackAddress(hostID: UUID, sessionID: String?, port: Int) async throws -> String {
        throw Refused.unverified
    }
}
