import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct MachineToolsTests {
    private let host = UUID()

    private func record(noHelper: Bool = false) -> HostRecord {
        HostRecord(
            id: host,
            profile: HostProfile(id: host, name: "Studio", endpoint: HostEndpoint(host: "studio.local", port: 22, user: "sam"), auth: .password),
            symbol: "desktop", noHelper: noHelper
        )
    }

    private func tools(
        _ helper: ScriptedHelper, record: HostRecord?, prompter: CountingPrompter? = nil
    ) -> LiveMachineTools {
        LiveMachineTools(
            channels: helper, records: { $0 == record?.id ? record : nil },
            prompter: { _ in prompter }
        )
    }

    @Test func liveToolsUsesHelperChannel() async throws {
        let helper = ScriptedHelper()
        helper.answer("host.info", #"{"interfaces":[{"name":"en0","mac":"02:00:00:00:00:01","ipv4":["192.168.1.20"]}],"wakeOnLan":true}"#)
        let tools = tools(helper, record: record())
        let info = try await tools.call(host) { try await $0.hostInfo() }
        #expect(info.interfaces.first?.mac == "02:00:00:00:00:01")
        #expect(helper.calls == 1)
        #expect(helper.requests.map(\.method) == ["host.info"])
    }

    @Test func liveToolsPassesTheAppPrompter() async throws {
        let helper = ScriptedHelper()
        let prompter = CountingPrompter()
        helper.answer("host.info", #"{"interfaces":[],"wakeOnLan":null}"#)
        let tools = tools(helper, record: record(), prompter: prompter)
        _ = try await tools.call(host) { try await $0.hostInfo() }
        #expect(helper.lastPrompterWasGiven)
    }

    @Test func unknownMachineFailsWithoutCalling() async {
        let helper = ScriptedHelper()
        let tools = tools(helper, record: record())
        await #expect(throws: LiveServiceError.self) { try await tools.call(UUID()) { try await $0.hostInfo() } }
        #expect(helper.calls == 0)
    }

    @Test func machineWithoutHelperFailsWithoutCalling() async {
        let helper = ScriptedHelper()
        let tools = tools(helper, record: record(noHelper: true))
        await #expect(throws: LiveServiceError.self) { try await tools.call(host) { try await $0.hostInfo() } }
        #expect(helper.calls == 0)
    }

    @Test func fakeToolsScriptReplies() async throws {
        let fake = FakeMachineTools()
        fake.defaultCapabilities = [Capability.notify]
        fake.answer("notify.get", #"{"idleSeconds":6,"muted":false,"remindMinutes":10}"#)
        let settings = try await fake.call(host) { try await $0.notifySettings() }
        #expect(settings == NotifySettings(idleSeconds: 6, muted: false, remindMinutes: 10))
        #expect(fake.requests.map(\.method) == ["notify.get"])
        #expect(fake.calledHosts == [host])
    }
}
