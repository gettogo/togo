import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct OfflineMachineEditTests {
    @Test(arguments: [false, true])
    func aDisplayOnlyEditSavesWithoutSigningIn(symbolOnly: Bool) async throws {
        let appServices = FakeAppServices()
        let app = AppStore(services: appServices)
        let original = try #require(appServices.hostRecord(Samples.studio))
        let network = FakeOnboardingServices(outcomes: [.unreachable])
        let flow = AddMachineFlow.editing(original, store: app, services: network, tutorial: nil)
        if symbolOnly { flow.draft.symbol = .laptop } else { flow.draft.name = "Renamed offline" }
        flow.connect()
        await flow.runAttempt()
        #expect(network.connected.isEmpty)
        let saved = appServices.hosts.first { $0.id == original.id }
        if symbolOnly { #expect(saved?.symbol == .laptop) }
        else { #expect(saved?.name == "Renamed offline") }
    }

    @Test func aConnectionEditStillChecksTheMachine() async throws {
        let appServices = FakeAppServices()
        let app = AppStore(services: appServices)
        let original = try #require(appServices.hostRecord(Samples.studio))
        let network = FakeOnboardingServices(outcomes: [.unreachable])
        let flow = AddMachineFlow.editing(original, store: app, services: network, tutorial: nil)
        flow.draft.name = "Renamed"
        flow.draft.address = "other.example.test"
        flow.connect()
        await flow.runAttempt()
        #expect(network.connected.count == 1)
        #expect(appServices.hosts.first { $0.id == original.id }?.name != "Renamed")
    }
    @Test func unavailableCredentialDoesNotBlockDisplayOnlyChanges() async throws {
        let appServices = FakeAppServices()
        let app = AppStore(services: appServices)
        var original = try #require(appServices.hostRecord(Samples.studio))
        original.profile.auth = .key(id: "not-on-this-device")
        let network = FakeOnboardingServices(outcomes: [.unreachable])
        let flow = AddMachineFlow.editing(original, store: app, services: network, tutorial: nil)
        #expect(!flow.draft.isValid)
        flow.draft.name = "Renamed"
        flow.connect()
        #expect(flow.attempt == 1)
        #expect(!flow.showsErrors)
        #expect(!flow.validatesConnection)
        await flow.runAttempt()
        #expect(network.connected.isEmpty)
        #expect(appServices.hosts.first { $0.id == original.id }?.name == "Renamed")
    }

    @Test func failedLocalSaveDoesNotFinishOrTryTheNetwork() async throws {
        let appServices = FakeAppServices()
        appServices.beforeMetadataSave = { throw HostDisplayEdit.Failure.unavailable }
        let app = AppStore(services: appServices)
        let original = try #require(appServices.hostRecord(Samples.studio))
        app.sheet = .editMachine(original.id)
        let network = FakeOnboardingServices(outcomes: [.unreachable])
        let flow = AddMachineFlow.editing(original, store: app, services: network, tutorial: nil)
        flow.draft.name = "Renamed"
        flow.connect()
        await flow.runAttempt()
        #expect(network.connected.isEmpty)
        #expect(appServices.hosts.first { $0.id == original.id }?.name == original.profile.name)
        #expect(app.sheet == .editMachine(original.id))
        #expect(!flow.saving)
        if case .failed = flow.phase {} else { Issue.record("Local save failure was not shown") }
    }

    @Test func cancelledEditCannotDismissTheNextFormAfterALateSave() async throws {
        let appServices = FakeAppServices()
        let (gate, release) = AsyncStream<Void>.makeStream()
        var started = false
        appServices.beforeMetadataSave = {
            started = true
            for await _ in gate { break }
        }
        let app = AppStore(services: appServices)
        let original = try #require(appServices.hostRecord(Samples.studio))
        let flow = AddMachineFlow.editing(original, store: app, services: FakeOnboardingServices(), tutorial: nil)
        flow.draft.name = "Renamed"
        flow.connect()
        let save = Task { await flow.runAttempt() }
        defer { release.finish(); save.cancel() }
        let deadline = ContinuousClock.now + .seconds(3)
        while !started {
            guard ContinuousClock.now < deadline else { throw HarnessUnavailable() }
            await Task.yield()
        }
        flow.cancel()
        app.sheet = .newSession(original.id)
        release.yield(())
        await save.value
        #expect(app.sheet == .newSession(original.id))
        #expect(!flow.saving)
    }

}
