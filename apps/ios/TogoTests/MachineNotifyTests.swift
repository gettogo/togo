import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct MachineNotifyTests {
    private let host = Samples.studio
    private let stored = #"{"idleSeconds":6,"muted":false,"remindMinutes":10}"#

    private func model(
        _ tools: FakeMachineTools, supported: Bool = true, debounce: Duration = .milliseconds(30),
        via: (any MachineTools)? = nil
    ) -> MachineNotifyModel {
        tools.defaultCapabilities = [Capability.notify]
        tools.answer("notify.get", stored)
        return MachineNotifyModel(host: host, name: "Studio", tools: via ?? tools, supported: supported, debounce: debounce)
    }

    private func sets(_ tools: FakeMachineTools) -> [FakeMachineTools.Request] {
        tools.requests.filter { $0.method == "notify.set" }
    }

    @Test func mutePatchSent() async {
        let tools = FakeMachineTools()
        let model = model(tools)
        await model.load()
        tools.answer("notify.set", #"{"idleSeconds":6,"muted":true,"remindMinutes":10}"#)
        model.setMuted(true)
        #expect(model.settings?.muted == true)
        await model.flush()
        #expect(sets(tools).map(\.params) == [["muted": .bool(true)]])
        #expect(model.settings?.muted == true)
        #expect(model.error == nil)
    }

    @Test func idleChoicesMapToSeconds() async {
        #expect(MachineNotifyModel.idleChoices.map(\.seconds) == [6, 15, 30, 60, 120])
        #expect(MachineNotifyModel.idleChoices.map(\.title) == ["6 s", "15 s", "30 s", "1 min", "2 min"])
        let tools = FakeMachineTools()
        let model = model(tools)
        await model.load()
        tools.answer("notify.set", #"{"idleSeconds":60,"muted":false,"remindMinutes":10}"#)
        model.setIdle(seconds: 60)
        await model.flush()
        #expect(sets(tools).map(\.params) == [["idleSeconds": .number(60)]])
    }

    @Test func remindOffSendsZero() async {
        #expect(MachineNotifyModel.remindChoices.map(\.minutes) == [0, 5, 10, 30])
        #expect(MachineNotifyModel.remindChoices.map(\.title) == ["Off", "5 min", "10 min", "30 min"])
        let tools = FakeMachineTools()
        let model = model(tools)
        await model.load()
        tools.answer("notify.set", #"{"idleSeconds":6,"muted":false,"remindMinutes":0}"#)
        model.setRemind(minutes: 0)
        await model.flush()
        #expect(sets(tools).map(\.params) == [["remindMinutes": .number(0)]])
    }

    @Test func noCapabilityShowsUpdateText() async {
        let section = MachineSection(
            id: host, name: "Studio", symbol: .desktop, reachability: .online, helper: .present(version: "1", live: true),
            sessions: [], lastSeen: nil, capabilities: [Capability.push]
        )
        let rows = NotificationValues.machines([section], paired: [host])
        #expect(rows.first?.supportsNotify == false)
        #expect(MachineNotifyCopy.update(name: "Studio") == "Update the helper on Studio to change these.")

        var withIt = section
        withIt.capabilities.insert(Capability.notify)
        #expect(NotificationValues.machines([withIt], paired: [host]).first?.supportsNotify == true)

        let tools = FakeMachineTools()
        let model = model(tools, supported: false)
        await model.load()
        await model.flush()
        #expect(tools.requests.isEmpty)
        #expect(model.settings == nil)
    }

    @Test func rapidChangesBecomeOnePatch() async {
        let tools = FakeMachineTools()
        let model = model(tools, debounce: .seconds(60))
        await model.load()
        tools.answer("notify.set", #"{"idleSeconds":120,"muted":false,"remindMinutes":30}"#)
        model.setIdle(seconds: 15)
        model.setIdle(seconds: 30)
        model.setIdle(seconds: 120)
        model.setRemind(minutes: 30)
        #expect(sets(tools).isEmpty)
        await model.flush()
        #expect(sets(tools).map(\.params) == [["idleSeconds": .number(120), "remindMinutes": .number(30)]])
        #expect(model.settings == NotifySettings(idleSeconds: 120, muted: false, remindMinutes: 30))
    }

    @Test func debounceSendsOnItsOwn() async {
        let tools = FakeMachineTools()
        let model = model(tools, debounce: .milliseconds(10))
        await model.load()
        tools.answer("notify.set", #"{"idleSeconds":6,"muted":true,"remindMinutes":10}"#)
        model.setMuted(true)
        for _ in 0..<200 where sets(tools).isEmpty { try? await Task.sleep(for: .milliseconds(25)) }
        await model.flush()
        #expect(sets(tools).count == 1)
    }

    @Test func aChangeAfterASendIsStillSent() async {
        let tools = FakeMachineTools()
        let model = model(tools)
        await model.load()
        tools.answer("notify.set", #"{"idleSeconds":6,"muted":true,"remindMinutes":10}"#)
        model.setMuted(true)
        async let first: Void = model.flush()
        model.setIdle(seconds: 60)
        async let second: Void = model.flush()
        _ = await (first, second)
        await model.flush()
        #expect(sets(tools).flatMap { $0.params.keys }.sorted() == ["idleSeconds", "muted"])
    }

    @Test func failedWriteKeepsTheNewerChangeAndSendsIt() async {
        let fake = FakeMachineTools()
        let gate = GateTools(fake)
        let model = model(fake, via: gate)
        await model.load()
        gate.beforeCall = { number in
            if number == 2 { fake.fail("notify.set", code: -32000) }
            if number == 3 { fake.answer("notify.set", #"{"idleSeconds":60,"muted":false,"remindMinutes":10}"#) }
        }
        gate.holdCall = 2
        model.setMuted(true)
        let flushing = Task { await model.flush() }
        while !gate.held { await Task.yield() }
        model.setIdle(seconds: 60)
        #expect(model.settings?.idleSeconds == 60)
        gate.release()
        await flushing.value
        await model.flush()
        #expect(sets(fake).map(\.params) == [["muted": .bool(true)], ["idleSeconds": .number(60)]])
        #expect(model.settings == NotifySettings(idleSeconds: 60, muted: false, remindMinutes: 10))
        #expect(model.error == "Couldn't save that change to Studio. Try again.")
    }

    @Test func failedLoadCanBeTriedAgain() async {
        let tools = FakeMachineTools()
        tools.defaultCapabilities = [Capability.notify]
        tools.fail("notify.get", code: -32000)
        let model = MachineNotifyModel(host: host, name: "Studio", tools: tools, supported: true)
        await model.load()
        #expect(model.settings == nil)
        #expect(model.error == "Couldn't read these settings from Studio.")
        tools.answer("notify.get", stored)
        await model.load()
        #expect(model.settings?.idleSeconds == 6)
        #expect(model.error == nil)
    }

    @Test func failedWriteRollsBackWithASentence() async {
        let tools = FakeMachineTools()
        let model = model(tools)
        await model.load()
        tools.fail("notify.set", code: -32000)
        model.setMuted(true)
        #expect(model.settings?.muted == true)
        await model.flush()
        #expect(model.settings == NotifySettings(idleSeconds: 6, muted: false, remindMinutes: 10))
        #expect(model.error == "Couldn't save that change to Studio. Try again.")
        tools.answer("notify.set", #"{"idleSeconds":6,"muted":true,"remindMinutes":10}"#)
        model.setMuted(true)
        await model.flush()
        #expect(model.error == nil)
    }

    @Test func muteFooterSaysRequestsToo() {
        #expect(MachineNotifyCopy.muteFooter(name: "Studio")
            == "Nothing from Studio, including approval requests, makes a sound or shows a banner. Requests still appear in Togo.")
        #expect(MachineNotifyCopy.title(name: "Studio") == "Notifications from Studio")
    }

    @Test func silentDoneIsKeptOnThisDevice() {
        let backend = MemoryBackend()
        var settings = AppSettings()
        settings.doneSilent = true
        DeviceSettings(backend: backend).save(settings)
        var other = AppSettings()
        DeviceSettings(backend: backend).apply(to: &other)
        #expect(other.doneSilent)
        let encoded = try? JSONEncoder().encode(settings)
        #expect(!(String(data: encoded ?? Data(), encoding: .utf8) ?? "").contains("doneSilent"))
    }
}

/// Passes calls to a fake helper and can hold one of them until released, so a test can change
/// a setting while a write is in flight.
@MainActor private final class GateTools: MachineTools {
    private let inner: FakeMachineTools
    private var calls = 0
    private var gate: CheckedContinuation<Void, Never>?
    var holdCall = 0
    var beforeCall: (Int) -> Void = { _ in }

    init(_ inner: FakeMachineTools) { self.inner = inner }

    var held: Bool { gate != nil }

    func release() {
        gate?.resume()
        gate = nil
    }

    func call<T: Sendable>(_ host: HostID, _ body: @Sendable (HelperClient) async throws -> T) async throws -> T {
        calls += 1
        beforeCall(calls)
        if calls == holdCall { await withCheckedContinuation { gate = $0 } }
        return try await inner.call(host, body)
    }
}
