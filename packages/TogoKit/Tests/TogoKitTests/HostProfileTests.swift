import Foundation
import Testing
import TogoKit

struct HostProfileTests {
    @Test func hostProfileCodableDefaults() throws {
        let json = """
        {
          "id": "6F1C2D4E-8A9B-4C3D-9E0F-112233445566",
          "name": "studio",
          "endpoint": {"host": "studio.local", "user": "sam"},
          "auth": {"password": {}}
        }
        """
        let profile = try JSONDecoder().decode(HostProfile.self, from: Data(json.utf8))
        #expect(profile.id == UUID(uuidString: "6F1C2D4E-8A9B-4C3D-9E0F-112233445566"))
        #expect(profile.endpoint == HostEndpoint(host: "studio.local", port: 22, user: "sam"))
        #expect(profile.jumps.isEmpty)
        #expect(profile.auth == .password)
        #expect(profile.transport == .auto)
        #expect(profile.moshServerPath == nil)
        #expect(profile.moshPorts == nil)
        #expect(profile.etAvailable == false)
    }

    @Test func hostProfileRoundTripsEveryField() throws {
        let profile = HostProfile(
            id: UUID(),
            name: "lab",
            endpoint: HostEndpoint(host: "10.0.0.5", port: 2222, user: "ops"),
            jumps: [HostEndpoint(host: "bastion", user: "jump")],
            auth: .key(id: "ed25519-main"),
            transport: .mosh,
            moshServerPath: "/opt/homebrew/bin/mosh-server",
            moshPorts: PortRange(lower: 60001, upper: 60010),
            etAvailable: true
        )
        let decoded = try JSONDecoder().decode(HostProfile.self, from: JSONEncoder().encode(profile))
        #expect(decoded == profile)
    }

    @Test func hostEndpointInitDefaultsPort() {
        #expect(HostEndpoint(host: "h", user: "u").port == 22)
    }
}
