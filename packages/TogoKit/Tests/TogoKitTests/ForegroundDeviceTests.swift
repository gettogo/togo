import Foundation
import Synchronization
import Testing
@testable import TogoKit

struct ForegroundDeviceTests {
    @Test func theAdditiveSubscriptionFieldSurvivesCoding() throws {
        let params = try JSONDecoder().decode(EventsSubscribeParams.self,
            from: Data("{\"foreground\":true,\"deviceId\":\"d_abcdefghijklmnopqrstuvwxyz\"}".utf8))
        let encoded = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(params)) as? [String: Any])
        #expect(encoded["deviceId"] as? String == "d_abcdefghijklmnopqrstuvwxyz")
    }

    @Test(arguments: [false, true], [false, true])
    func subscriptionsGateTheDeviceFieldWithoutChangingForeground(_ supports: Bool, _ hasID: Bool) async throws {
        let channel = PipeChannel()
        let id = hasID ? "d_abcdefghijklmnopqrstuvwxyz" : nil
        let source = HelperSource(host: "machine", clock: ManualClock(), deviceID: { id }) {
            HelperDial(channel: channel, hangUp: {})
        }
        async let accepted: Void = channel.acceptHello(capabilities: ["session.v1", "events.v1"] + (supports ? ["fgdevice.v1"] : []))
        _ = try await source.open()
        await accepted
        for foreground in [true, false] {
            await source.setForeground(foreground)
            let task = Task { try await source.snapshot() }
            let request = try #require(await channel.nextRequest())
            #expect(request.params["foreground"] == .bool(foreground))
            #expect(request.params["deviceId"] == (supports && hasID ? .string(id!) : nil))
            await channel.reply(to: request.id, result: #"{"seq":0,"sessions":[],"approvals":[]}"#)
            _ = try await task.value
        }
        await source.close()
    }

    @Test func protectedDataRecoveryReadsTheCurrentPairingIdentity() async throws {
        let id = Mutex<String?>(nil)
        let channel = PipeChannel()
        let source = HelperSource(host: "machine", clock: ManualClock(), deviceID: { id.withLock { $0 } }) {
            HelperDial(channel: channel, hangUp: {})
        }
        async let accepted: Void = channel.acceptHello(capabilities: ["session.v1", "events.v1", "fgdevice.v1"])
        _ = try await source.open()
        await accepted
        for expected in [nil, "d_abcdefghijklmnopqrstuvwxyz"] {
            id.withLock { $0 = expected }
            let task = Task { try await source.snapshot() }
            let request = try #require(await channel.nextRequest())
            #expect(request.params["foreground"] == .bool(true))
            #expect(request.params["deviceId"] == expected.map(JSONValue.string))
            await channel.reply(to: request.id, result: #"{"seq":0,"sessions":[],"approvals":[]}"#)
            _ = try await task.value
        }
        await source.close()
    }
}
