import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct AwakeTests {
    private let host = Samples.studio

    private func status(
        mode: AwakeMode = .plugged, held: Bool = false, reason: AwakeReason = .idle, detail: String? = nil,
        floor: Int = 20
    ) -> AwakeStatus {
        AwakeStatus(
            mode: mode, held: held, reason: reason, detail: detail, power: "ac", batteryPercent: 80,
            batteryFloor: floor, platformSupported: true, needsAdmin: false
        )
    }

    private func json(mode: String, held: Bool = false, reason: String = "idle") -> String {
        #"{"mode":"\#(mode)","held":\#(held),"reason":"\#(reason)","power":"ac","batteryFloor":20,"platformSupported":true,"needsAdmin":false}"#
    }

    private func model(_ tools: FakeMachineTools, start: AwakeStatus? = nil) -> MachineAwakeModel {
        tools.defaultCapabilities = [Capability.awake]
        return MachineAwakeModel(host: host, name: "Studio", tools: tools, status: start ?? status())
    }

    @Test func headingOnlyWhenHeld() {
        #expect(AwakeCopy.heading(status(held: true, reason: .active)) == "Held awake")
        #expect(AwakeCopy.heading(status(held: false, reason: .idle)) == nil)
        #expect(AwakeCopy.heading(status(held: false, reason: .onBattery)) == nil)
    }

    @Test func footersFollowTheReason() {
        #expect(AwakeCopy.reason(status(mode: .plugged, reason: .onBattery)) == "On battery a closed lid still sleeps.")
        #expect(
            AwakeCopy.reason(status(held: true, reason: .grace))
                == "Held for 2 minutes after the last session stops working."
        )
        #expect(
            AwakeCopy.reason(status(mode: .battery, reason: .batteryFloor, floor: 20))
                == "Paused until the battery is above 25%."
        )
        #expect(AwakeCopy.reason(status(reason: .thermal)) == "Paused while the machine is hot.")
        #expect(AwakeCopy.reason(status(reason: .unsupported)) == "This machine can't be kept awake by Togo.")
        #expect(AwakeCopy.reason(status(reason: .failed, detail: "pmset: denied")) == "Couldn't keep it awake: pmset: denied")
        #expect(AwakeCopy.reason(status(reason: .idle)) == "")
        #expect(AwakeCopy.reason(status(held: true, reason: .active)) == "")
    }

    @Test func failedDetailIsOneLinePlainText() {
        let text = AwakeCopy.reason(status(reason: .failed, detail: "disablesleep: **bold**\nsecond line"))
        #expect(text == "Couldn't keep it awake: disablesleep: **bold**")
    }

    @Test func pickerChoicesAreWorded() {
        #expect(AwakeCopy.choices.map(\.mode) == [.off, .plugged, .battery])
        #expect(AwakeCopy.choices.map(\.title) == ["Off", "When plugged in", "Also on battery"])
    }

    @Test func choosingAModeSendsItAndKeepsTheAnswer() async {
        let tools = FakeMachineTools()
        let model = model(tools)
        tools.answer("awake.set", json(mode: "off"))
        await model.choose(.off)
        #expect(tools.requests.filter { $0.method == "awake.set" }.map(\.params) == [["mode": .string("off")]])
        #expect(model.status?.mode == .off)
        #expect(model.error == nil)
        #expect(!model.needsBatterySetup)
    }

    @Test func refusedBatteryShowsTheSetupStepAndRevertsPicker() async {
        let tools = FakeMachineTools()
        let model = model(tools, start: status(mode: .plugged))
        tools.fail("awake.set", code: -32008, data: #"{"reason":"needs_admin_setup"}"#)
        await model.choose(.battery)
        #expect(model.needsBatterySetup)
        #expect(model.status?.mode == .plugged)
        #expect(model.error == nil)
    }

    /// The setup step stays under the choice until the next pick, which may succeed.
    @Test func theNextPickClearsTheSetupStep() async {
        let tools = FakeMachineTools()
        let model = model(tools, start: status(mode: .plugged))
        tools.fail("awake.set", code: -32008, data: #"{"reason":"needs_admin_setup"}"#)
        await model.choose(.battery)
        #expect(model.needsBatterySetup)
        tools.answer("awake.set", json(mode: "off"))
        await model.choose(.off)
        #expect(!model.needsBatterySetup)
    }

    @Test func showsTheChoiceAtOnceAndRevertsOnAnyFailure() async {
        let tools = FakeMachineTools()
        tools.defaultCapabilities = [Capability.awake]
        tools.fail("awake.set", code: -32603, message: "boom")
        let gated = GatedTools(tools)
        let slow = MachineAwakeModel(host: host, name: "Studio", tools: gated, status: status(mode: .plugged))
        let pending = Task { await slow.choose(.off) }
        while gated.entered == 0 { await Task.yield() }
        #expect(slow.status?.mode == .off)
        gated.open()
        await pending.value
        #expect(slow.status?.mode == .plugged)
        #expect(slow.error == "Couldn't change keep awake on Studio. Try again.")
        #expect(!slow.needsBatterySetup)
    }

    @Test func loadReadsTheCurrentStatus() async {
        let tools = FakeMachineTools()
        let model = model(tools, start: status(mode: .off))
        tools.answer("awake.status", json(mode: "plugged", held: true, reason: "active"))
        await model.load()
        #expect(model.status?.mode == .plugged)
        #expect(model.status?.held == true)
    }

    @Test func aNilSeedLoadsOnceTheStatusArrives() async {
        let tools = FakeMachineTools()
        tools.defaultCapabilities = [Capability.awake]
        let model = MachineAwakeModel(host: host, name: "Studio", tools: tools, status: nil)
        tools.answer("awake.status", json(mode: "plugged", held: true, reason: "active"))
        await model.load()
        #expect(model.status?.mode == .plugged)
        #expect(!model.loadFailed)
    }

    @Test func aFailedFirstReadCanBeRetried() async {
        let tools = FakeMachineTools()
        tools.defaultCapabilities = [Capability.awake]
        let model = MachineAwakeModel(host: host, name: "Studio", tools: tools, status: nil)
        tools.fail("awake.status", code: -32603, message: "boom")
        await model.load()
        #expect(model.status == nil)
        #expect(model.loadFailed)
        tools.answer("awake.status", json(mode: "off"))
        await model.load()
        #expect(model.status?.mode == .off)
        #expect(!model.loadFailed)
    }

    @Test func adoptFollowsLiveUpdates() {
        let model = MachineAwakeModel(host: host, name: "Studio", tools: FakeMachineTools(), status: nil)
        model.adopt(status(held: true, reason: .grace))
        #expect(model.status?.reason == .grace)
        model.adopt(status(mode: .battery, reason: .batteryFloor))
        #expect(model.status?.reason == .batteryFloor)
        model.adopt(nil)
        #expect(model.status?.reason == .batteryFloor)
    }

    @Test func adoptWaitsOutAPickInFlight() async {
        let tools = FakeMachineTools()
        tools.defaultCapabilities = [Capability.awake]
        tools.answer("awake.set", json(mode: "off"))
        let gated = GatedTools(tools)
        let model = MachineAwakeModel(host: host, name: "Studio", tools: gated, status: status(mode: .plugged))
        let pending = Task { await model.choose(.off) }
        while gated.entered == 0 { await Task.yield() }
        model.adopt(status(mode: .plugged, held: true, reason: .active))
        #expect(model.status?.mode == .off)
        gated.open()
        await pending.value
        #expect(model.status?.mode == .off)
    }

    @Test func aSlowLoadDoesNotUndoAPick() async {
        let tools = FakeMachineTools()
        tools.defaultCapabilities = [Capability.awake]
        tools.answer("awake.status", json(mode: "plugged"))
        tools.answer("awake.set", json(mode: "off"))
        let gated = GatedTools(tools)
        let model = MachineAwakeModel(host: host, name: "Studio", tools: gated, status: status(mode: .plugged))
        let loading = Task { await model.load() }
        while gated.entered < 1 { await Task.yield() }
        let picking = Task { await model.choose(.off) }
        while gated.entered < 2 { await Task.yield() }
        gated.open()
        await loading.value
        await picking.value
        #expect(model.status?.mode == .off)
    }

    @Test func gatedOnTheAwakeCapability() {
        let section = MachineSection(
            id: host, name: "Studio", symbol: .desktop, reachability: .online, helper: .present(version: "1", live: true),
            sessions: [], lastSeen: nil, capabilities: [Capability.push]
        )
        #expect(NotificationValues.machines([section], paired: [host]).first?.supportsAwake == false)
        var withIt = section
        withIt.capabilities.insert(Capability.awake)
        #expect(NotificationValues.machines([withIt], paired: [host]).first?.supportsAwake == true)
    }

    @Test func snapshotCarriesTheStatusToTheSection() {
        var mapper = SnapshotMapper()
        var snapshot = HostSnapshot(hostID: host, reach: .online, capabilities: [Capability.awake])
        snapshot.awake = status(held: true, reason: .active)
        #expect(mapper.map(snapshot, noHelper: false, now: Samples.now).awake == snapshot.awake)
    }

    @Test func theHeadingAccessorySitsAfterAnyReachabilityWords() {
        let held = status(held: true, reason: .active)
        #expect(AwakeCopy.caption(reach: nil, awake: held) == "Held awake")
        #expect(AwakeCopy.caption(reach: "Offline", awake: held) == "Offline")
        #expect(AwakeCopy.caption(reach: nil, awake: status()) == nil)
        #expect(AwakeCopy.caption(reach: nil, awake: nil) == nil)
    }
}

/// Holds every call until the test opens the gate, so a test can look at the state in between.
@MainActor private final class GatedTools: MachineTools {
    private let inner: FakeMachineTools
    private(set) var entered = 0
    private var isOpen = false

    init(_ inner: FakeMachineTools) { self.inner = inner }

    func open() { isOpen = true }

    func call<T: Sendable>(_ host: HostID, _ body: @Sendable (HelperClient) async throws -> T) async throws -> T {
        entered += 1
        while !isOpen { await Task.yield() }
        return try await inner.call(host, body)
    }
}
