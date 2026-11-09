import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct TutorialModelTests {
    /// A fresh, empty defaults domain per test, so runs never see each other's progress.
    private func defaults() -> UserDefaults {
        let name = "tutorial-tests-\(UUID().uuidString)"
        return UserDefaults(suiteName: name)!
    }

    @Test func progressResumesWhereLeft() {
        let store = defaults()
        let first = TutorialModel(defaults: store)
        #expect(first.showsOnLaunch)
        #expect(first.page == .welcome)
        first.next()
        first.next()
        #expect(first.page == .sessions)

        let relaunched = TutorialModel(defaults: store)
        #expect(relaunched.page == .sessions)
        #expect(relaunched.showsOnLaunch)
        relaunched.back()
        #expect(TutorialModel(defaults: store).page == .approvals)
    }

    @Test func skipMarksSeen() {
        let store = defaults()
        let model = TutorialModel(defaults: store)
        model.next()
        model.skip()
        #expect(!model.showsOnLaunch)
        #expect(!TutorialModel(defaults: store).showsOnLaunch)
    }

    @Test func finishingMarksSeen() {
        let store = defaults()
        let model = TutorialModel(defaults: store)
        model.go(to: .setup)
        model.finish()
        #expect(!TutorialModel(defaults: store).showsOnLaunch)
    }

    @Test func gettingStartedReopensAtStart() {
        let store = defaults()
        let model = TutorialModel(defaults: store)
        model.go(to: .sessions)
        model.skip()
        let generation = model.tipsGeneration

        model.reopen()
        #expect(model.page == .welcome)
        #expect(model.tipsGeneration == generation + 1)
        // Reopening from settings is a visit, not a reset: the next launch still skips the guide.
        let relaunched = TutorialModel(defaults: store)
        #expect(!relaunched.showsOnLaunch)
        #expect(relaunched.page == .welcome)
        #expect(relaunched.tipsGeneration == generation + 1)
    }

    @Test func gettingStartedGivesEveryTipANewID() {
        let model = TutorialModel(defaults: defaults())
        let before = TutorialTip.allCases.map { $0.tip(generation: model.tipsGeneration).id }
        model.reopen()
        let after = TutorialTip.allCases.map { $0.tip(generation: model.tipsGeneration).id }
        #expect(Set(before).isDisjoint(with: after))
        #expect(Set(after).count == TutorialTip.allCases.count)
    }

    /// Progress saved by the six-page guide this one replaced opens on a page that still
    /// exists, never past the end.
    @Test func progressFromTheOldGuideStillOpens() {
        let store = defaults()
        store.set(5, forKey: TutorialModel.Keys.page)
        #expect(TutorialModel(defaults: store).page == .welcome)
    }

    /// Only Mosh is optional: the helper is what gives chat, approvals and notifications.
    @Test func helperIsPartOfTheStandardSetup() {
        #expect(SetupItem.allCases.filter(\.isOptional) == [.mosh])
        #expect(SetupItem.allCases.firstIndex(of: .helper)! < SetupItem.allCases.firstIndex(of: .mosh)!)
    }

    /// The helper's steps install tmux first, then the helper, then start it at login.
    @Test func helperStepsInstallTmuxAndStartTheService() {
        for os in MachineOS.allCases {
            let commands = SetupItem.helper.steps(for: os).compactMap(\.command)
            #expect(commands.first?.contains("tmux") == true)
            #expect(commands.contains(HelperProbe.installCommand(os: os.unameName)))
            #expect(commands.last == "togo service install")
        }
    }

    @Test func nextStopsAtTheLastPage() {
        let model = TutorialModel(defaults: defaults())
        for _ in 0..<10 { model.next() }
        #expect(model.page == .setup)
        for _ in 0..<10 { model.back() }
        #expect(model.page == .welcome)
    }

    @Test func checklistTicksFromDetection() {
        let store = defaults()
        let model = TutorialModel(defaults: store)
        #expect(model.ticked.isEmpty)

        let report = HelperReport(helper: .present(version: "0.3.0"), tmux: "tmux 3.5a", mosh: false, os: "Darwin")
        model.record(SetupDetection(keyAccepted: true, report: report))
        #expect(model.ticked == [.remoteLogin, .reachable, .phoneKey, .helper])
        #expect(model.detected == [.remoteLogin, .reachable, .phoneKey, .helper])
        #expect(TutorialModel(defaults: store).ticked == [.remoteLogin, .reachable, .phoneKey, .helper])
    }

    @Test func refusedKeyStillShowsTheMachineIsReachable() {
        let model = TutorialModel(defaults: defaults())
        model.record(SetupDetection(keyAccepted: false, report: nil))
        #expect(model.ticked == [.remoteLogin, .reachable])
    }

    @Test func detectionNeverUnticksWhatTheUserTicked() {
        let model = TutorialModel(defaults: defaults())
        model.toggle(.mosh)
        model.record(SetupDetection(keyAccepted: true, report: HelperReport(helper: .missing, mosh: false)))
        #expect(model.ticked.contains(.mosh))
        #expect(!model.ticked.contains(.helper))
    }

    @Test func toggleTicksAndUnticks() {
        let store = defaults()
        let model = TutorialModel(defaults: store)
        model.toggle(.remoteLogin)
        #expect(TutorialModel(defaults: store).ticked == [.remoteLogin])
        model.toggle(.remoteLogin)
        #expect(TutorialModel(defaults: store).ticked.isEmpty)
    }

    @Test func demoNeverConnects() async throws {
        let demo = DemoMachine.store(now: Samples.now)
        let services = try #require(demo.services as? DemoServices)
        let machine = try #require(demo.controlCenter.machines.first)
        #expect(machine.name == "Demo")
        #expect(services.hosts.map(\.address) == [""])
        let row = try #require(machine.sessions.first)
        let card = try #require(demo.controlCenter.requests.first)

        await demo.answer(card.id, .approve)
        await demo.kill(row)
        await demo.rename(row, to: "renamed")
        await demo.setMute(row, true)
        let handle = services.terminal(hostID: machine.id, sessionID: row.id, transport: nil, userStarted: true)
        var phases: [LinkPhase] = []
        for await phase in handle.phases { phases.append(phase) }
        await #expect(throws: DemoServices.ReadOnly.self) {
            try await services.saveHost(HostRecord(
                id: machine.id, profile: HostProfile(
                    id: machine.id, name: "Demo", endpoint: HostEndpoint(host: "demo", port: 22, user: "demo"), auth: .password
                ), symbol: "laptop", helperPath: nil, noHelper: true
            ))
        }

        #expect(services.attempts == [.answer, .kill, .rename, .setMute, .terminal, .saveHost])
        #expect(!phases.contains { if case .live = $0 { true } else { false } })
        #expect(demo.controlCenter.requests.first?.phase == .open)
        #expect(demo.controlCenter.machines.first?.sessions.first?.name == row.name)
    }
}
