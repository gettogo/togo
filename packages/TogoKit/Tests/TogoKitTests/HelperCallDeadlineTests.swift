import Foundation
import Testing
@testable import TogoKit

struct HelperCallDeadlineTests {
    @Test func sourceCallMayOutlastTheOldTenSecondDeadline() async throws {
        let channel = PipeChannel()
        let client = HelperClient(channel: channel)
        async let accepted: Void = channel.acceptHello(capabilities: [Capability.harness])
        _ = try await client.hello(client: HelperClient.testClient)
        await accepted
        let complete = Task { try await client.harnessComplete(id: "s1", kind: .command, query: "/") }
        let request = try #require(await channel.nextRequest())
        // The helper's source timeout can be 15 seconds. Its valid reply must not lose to
        // the app's former 10-second deadline.
        try await Task.sleep(for: .seconds(11))
        await channel.reply(to: request.id, result: #"{"items":[]}"#)
        #expect(try await complete.value.isEmpty)
        await client.close()
    }

    @Test func operationBudgetsLeaveRoomForHelperWork() {
        #expect(HelperCallDeadline.duration(for: "harness.open") > .seconds(20))
        #expect(HelperCallDeadline.duration(for: "harness.action") > .seconds(15))
        #expect(HelperCallDeadline.duration(for: "git.status") > .seconds(20))
        #expect(HelperCallDeadline.duration(for: "events.keepalive") == .seconds(10))
    }

    @Test func explicitTimeoutStillOverridesLongOperationBudget() async throws {
        let channel = PipeChannel()
        let client = HelperClient(channel: channel, callTimeout: .milliseconds(40))
        async let accepted: Void = channel.acceptHello(capabilities: [Capability.harness])
        _ = try await client.hello(client: HelperClient.testClient)
        await accepted
        await #expect(throws: HelperError.timeout(method: "harness.complete")) {
            _ = try await client.harnessComplete(id: "s1", kind: .command, query: "/")
        }
        await client.close()
    }
}
