import Foundation
import Testing
import TogoKit
import TogoPush
@testable import Togo

/// Fixed snapshot data for the intents: Studio online with 01 and 04, Build box asleep with 02.
@MainActor enum IntentSamples {
    static var machines: [IntentSnapshot.Machine] {
        Samples.controlCenter.machines.map { IntentSnapshot.Machine(id: $0.id, name: $0.name) }
    }

    static func view(_ section: MachineSection) -> HostViewSnapshot {
        HostViewSnapshot(
            hostID: section.id, reachability: section.reachability, helper: section.helper,
            sessions: section.sessions, requests: [], at: Samples.now
        )
    }

    static var snapshot: IntentSnapshot {
        Samples.controlCenter.machines.reduce(IntentSnapshot().with(machines: machines)) { $0.applying(view($1)) }
    }
}

@MainActor final class SentLinks {
    var sent: [DeepLink] = []
}

@MainActor @Suite(.serialized) struct IntentsTests {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private let preset = AgentPreset(
        id: UUID(uuidString: "0E0C7C55-6A0B-4F5C-8B0B-6B3B2A1F0009")!,
        name: "Claude, no prompts", agent: "claude", args: ["--dangerously-skip-permissions"]
    )

    /// A hub that reads fixed data and records the links intents hand the app.
    private func hub(requests: [PendingRequest] = [], presets: [AgentPreset] = []) -> SentLinks {
        let snapshot = IntentSamples.snapshot
        let now = self.now
        let hub = IntentHub(snapshot: { snapshot }, requests: { requests }, presets: { presets }, clock: { now })
        let links = SentLinks()
        hub.connect { links.sent.append($0) }
        IntentHub.current = hub
        return links
    }

    private func request(_ id: String, age: TimeInterval, answered: PendingRequest.Answered? = nil) -> PendingRequest {
        PendingRequest(
            requestId: id, hostId: "h1", sessionId: "s1", bib: "04", agent: "Claude Code", host: "Studio",
            title: "Run npm test", subject: nil, options: [], receivedAt: now.addingTimeInterval(-age), answered: answered
        )
    }

    @Test func waitingCountFromSnapshot() async throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        let store = PendingStore(directory: directory, clock: { [now] in now })
        store.upsert(request("open-1", age: 5))
        store.upsert(request("open-2", age: 590))
        store.upsert(request("answered", age: 10, answered: .approved))
        store.upsert(request("expired", age: 700))

        #expect(WaitingCountIntent.count(store.all(), now: now) == 2)
        #expect(WaitingCountIntent.count([], now: now) == 0)
        #expect(WaitingCountIntent.message(2) == "2 requests are waiting.")
        #expect(WaitingCountIntent.message(1) == "1 request is waiting.")
        #expect(WaitingCountIntent.message(0) == "Nothing is waiting.")

        let links = hub(requests: store.all())
        _ = try await WaitingCountIntent().perform()
        #expect(links.sent.isEmpty)
        #expect(WaitingCountIntent.openAppWhenRun == false)
    }

    @Test func openSessionBuildsDeepLink() async throws {
        let session = try #require(IntentSamples.snapshot.sessions.first { $0.bib == "02" })
        let entity = SessionEntity(session)
        #expect(entity.machine == "Build box")
        #expect(entity.deepLink == .session(bib: "02", host: "Build box"))

        let links = hub()
        let intent = OpenSessionIntent()
        intent.session = entity
        _ = try await intent.perform()
        #expect(links.sent == [.session(bib: "02", host: "Build box")])
        #expect(OpenSessionIntent.openAppWhenRun)
    }

    @Test func linkWaitsForTheApp() {
        let hub = IntentHub(snapshot: { IntentSnapshot() }, requests: { [] }, presets: { [] }, clock: { .now })
        hub.open(.newSession(host: "Studio"))
        let links = SentLinks()
        hub.connect { links.sent.append($0) }
        #expect(links.sent == [.newSession(host: "Studio")])
        hub.open(.newSession(host: nil))
        #expect(links.sent.count == 2)
    }

    @Test func newSessionPrefillsFlow() async throws {
        let links = hub(presets: [preset])
        let intent = NewSessionIntent()
        intent.machine = MachineEntity(id: Samples.studio, name: "Studio")
        intent.folder = "  /Users/sam/src/app \n"
        intent.agent = AgentPresetEntity(preset)
        _ = try await intent.perform()
        let link = DeepLink.newSession(host: "Studio", folder: "/Users/sam/src/app", preset: preset.id)
        #expect(links.sent == [link])
        #expect(NewSessionIntent.openAppWhenRun)

        // The app opens the sheet on that machine and the flow lands on the agent step.
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.open(link)
        #expect(store.sheet == .newSession(Samples.studio))
        #expect(store.takeNewSessionPrefill(for: Samples.build) == nil)
        let prefill = try #require(store.takeNewSessionPrefill(for: Samples.studio))
        #expect(prefill == NewSessionPrefill(hostID: Samples.studio, folder: "/Users/sam/src/app", presetID: preset.id))
        #expect(store.takeNewSessionPrefill(for: Samples.studio) == nil)

        var machines = Samples.controlCenter.machines
        machines[0].capabilities = [Capability.launch]
        let model = NewSessionModel(
            services: FakeAppServices(), hostID: Samples.studio, machines: machines, library: .inMemory([preset])
        ).prefilled(prefill)
        #expect(model.step == .agent)
        #expect(model.cwd == "/Users/sam/src/app")
        #expect(model.pushedSteps == [.agent])
        await model.start()
        #expect(model.selectedPreset?.id == preset.id)
        #expect(model.flags == "--dangerously-skip-permissions")

        // Without a folder the flow asks for one, then picks the agent.
        let noFolder = NewSessionModel(
            services: FakeAppServices(), hostID: Samples.studio, machines: machines, library: .inMemory([preset])
        ).prefilled(NewSessionPrefill(hostID: Samples.studio, folder: nil, presetID: preset.id))
        #expect(noFolder.step == .folder)
        await noFolder.choose(folder: "/Users/sam/src/site")
        #expect(noFolder.selectedPreset?.id == preset.id)

        // Another machine drops the shortcut's agent.
        await noFolder.choose(noFolder.machines[1])
        #expect(noFolder.requestedPresetID == nil)
    }

    /// The sheet that is up already closes and opens again from the shortcut; the prefill
    /// waits for the sheet that reads it, however late the old one goes.
    @Test func prefillSurvivesReopen() async throws {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.sheet = .newSession(Samples.studio)
        store.open(.newSession(host: "Studio", folder: "/Users/sam/src/api", preset: nil))
        #expect(store.sheet == nil)
        // The closing sheet is built once more on its way out; it is not the one asked for.
        #expect(store.takeNewSessionPrefill(for: Samples.build) == nil)
        try await Task.sleep(for: AppStore.coverDismissal + .milliseconds(300))
        #expect(store.sheet == .newSession(Samples.studio))
        #expect(store.takeNewSessionPrefill(for: Samples.studio)?.folder == "/Users/sam/src/api")

        // From one machine's sheet to another's: the old sheet cannot take the new prefill.
        store.sheet = .newSession(Samples.build)
        store.open(.newSession(host: "Studio", folder: "/Users/sam/src/site", preset: nil))
        #expect(store.sheet == .newSession(Samples.studio))
        #expect(store.takeNewSessionPrefill(for: Samples.build) == nil)
        #expect(store.takeNewSessionPrefill(for: Samples.studio)?.folder == "/Users/sam/src/site")

        // A plain link leaves an open sheet alone.
        store.open(.newSession(host: "Studio"))
        #expect(store.sheet == .newSession(Samples.studio))
        #expect(store.takeNewSessionPrefill(for: Samples.studio) == nil)
    }

    @Test func newSessionLinkCarriesCheckedPrefill() {
        let id = preset.id.uuidString
        func link(_ text: String) -> DeepLink? { URL(string: text).flatMap(DeepLink.init(url:)) }
        #expect(link("togo://new?host=Studio&folder=%2FUsers%2Fsam&agent=\(id)")
            == .newSession(host: "Studio", folder: "/Users/sam", preset: preset.id))
        #expect(link("togo://new?host=Studio&folder=a%00b&agent=nope") == .newSession(host: "Studio"))
        #expect(link("togo://new?folder=\(String(repeating: "a", count: 1100))") == .newSession(host: nil))
        #expect(link("togo://new?folder=%20%20") == .newSession(host: nil))
    }

    @Test func entitiesQueryByName() async throws {
        _ = hub(presets: [preset])

        let byBib = try await SessionEntityQuery().entities(matching: "04")
        #expect(byBib.map(\.bib) == ["04"])
        let byName = try await SessionEntityQuery().entities(matching: "RELEASE")
        #expect(byName.map(\.machine) == ["Build box"])
        let byMachine = try await SessionEntityQuery().entities(matching: "studio")
        #expect(Set(byMachine.map(\.bib)) == ["01", "04"])
        let suggested = try await SessionEntityQuery().suggestedEntities()
        #expect(suggested.count == 3)
        let ids = [byName[0].id, "missing"]
        #expect(try await SessionEntityQuery().entities(for: ids).map(\.id) == [byName[0].id])

        let machines = try await MachineEntityQuery().entities(matching: "build")
        #expect(machines.map(\.name) == ["Build box"])
        #expect(try await MachineEntityQuery().suggestedEntities().map(\.name) == ["Studio", "Build box"])
        #expect(try await MachineEntityQuery().entities(for: [Samples.studio]).map(\.name) == ["Studio"])

        #expect(try await AgentPresetEntityQuery().entities(matching: "no prompts").map(\.id) == [preset.id])
        #expect(try await AgentPresetEntityQuery().entities(for: [preset.id]).map(\.name) == [preset.name])
    }
}
