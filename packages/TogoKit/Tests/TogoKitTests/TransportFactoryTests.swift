import Foundation
import Testing
@testable import TogoKit

struct TransportFactoryTests {
    @Test func etOfferedOnlyWithEtserver() {
        let factory = ETFactory.factory
        #expect(factory.kind == .et)
        let withServer = ConnectorTests.profile(etAvailable: true)
        let without = ConnectorTests.profile(etAvailable: false)
        #expect(factory.unavailableReason(withServer) == nil)
        #expect(factory.unavailableReason(without) == "etserver not found on this machine")
    }

    @Test func etNotOfferedThroughJumpHost() {
        let profile = ConnectorTests.profile(jumps: [HostEndpoint(host: "bastion", user: "u")], etAvailable: true)
        #expect(ETFactory.factory.unavailableReason(profile) == "ET needs a direct route; this machine uses a jump host")
    }
}
