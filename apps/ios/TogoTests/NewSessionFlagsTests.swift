import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct NewSessionFlagsTests {
    private let services = FakeAppServices()
    private let folder = "/Users/sam/src/app"
    private let kimi = AgentPreset(
        id: UUID(uuidString: "0E0C7C55-6A0B-4F5C-8B0B-6B3B2A1F0001")!,
        name: "Kimi", agent: AgentPreset.custom, program: "kimi", args: ["--yolo"]
    )
    private let claudeYolo = AgentPreset(
        id: UUID(uuidString: "0E0C7C55-6A0B-4F5C-8B0B-6B3B2A1F0002")!,
        name: "Claude, no prompts", agent: "claude", args: ["--dangerously-skip-permissions"]
    )

    private func machines(launch: Bool = true) -> [MachineSection] {
        var machines = Samples.controlCenter.machines
        if launch { machines[0].capabilities = [Capability.launch] }
        return machines
    }

    private func model(library: AgentLibrary, launch: Bool = true) -> NewSessionModel {
        NewSessionModel(services: services, hostID: nil, machines: machines(launch: launch), library: library)
    }

    private func ready(_ model: NewSessionModel) async {
        await model.choose(model.machines[0])
        await model.choose(folder: folder)
    }

    private var created: CreateSessionParams? {
        if case .create(_, let params)? = services.calls.last { return params }
        return nil
    }

    @Test func createSendsArgsAndProgram() async {
        let library = AgentLibrary.inMemory([kimi, claudeYolo])
        let model = model(library: library)
        await ready(model)

        model.choose(preset: kimi)
        #expect(model.flags == "--yolo")
        model.flags = #"--yolo --model "k2 turbo" '$HOME' ; rm -rf ~"#
        _ = await model.create()
        #expect(created == CreateSessionParams(
            agent: "custom", cwd: folder, program: "kimi",
            args: ["--yolo", "--model", "k2 turbo", "$HOME", ";", "rm", "-rf", "~"]
        ))
    }

    @Test func registryAgentSendsTypedFlagsAsArgv() async {
        let model = model(library: .inMemory())
        await ready(model)
        #expect(model.selectedAgent?.id == "claude")
        model.flags = "--dangerously-skip-permissions"
        _ = await model.create()
        #expect(created == CreateSessionParams(agent: "claude", cwd: folder, args: ["--dangerously-skip-permissions"]))
    }

    @Test func presetFlagsReachTheHelperUnchanged() async {
        let model = model(library: .inMemory([claudeYolo]))
        await ready(model)
        model.choose(preset: claudeYolo)
        _ = await model.create()
        #expect(created?.args == ["--dangerously-skip-permissions"])
        #expect(created?.agent == "claude")
        #expect(created?.program == nil)
    }

    @Test func badFlagsBlockStart() async {
        let model = model(library: .inMemory())
        await ready(model)
        model.flags = #"--model "opus"#
        #expect(model.flagsError == "A quote is not closed.")
        #expect(!model.canStart)
        model.flags = #"--model "opus""#
        #expect(model.flagsError == nil)
        #expect(model.canStart)
    }

    @Test func shellTakesNoFlags() async {
        let model = model(library: .inMemory())
        await ready(model)
        model.choose(agent: "shell")
        #expect(!model.showsFlags)
        model.flags = "--x"
        _ = await model.create()
        #expect(created?.args == nil)
    }

    @Test func rememberedPresetPreselected() async {
        let library = AgentLibrary.inMemory([kimi, claudeYolo])
        let first = model(library: library)
        await ready(first)
        first.choose(preset: kimi)
        first.rememberFolder = true
        _ = await first.create()
        #expect(await library.rememberedPreset(host: Samples.studio, cwd: folder) == kimi.id)

        let next = model(library: library)
        await ready(next)
        #expect(next.selectedPreset == kimi)
        #expect(next.isRemembered(kimi))
        #expect(!next.isRemembered(claudeYolo))
        #expect(next.flags == "--yolo")
        #expect(next.rememberFolder)

        // Another folder on the same machine keeps the registry default.
        let elsewhere = model(library: library)
        await elsewhere.choose(elsewhere.machines[0])
        await elsewhere.choose(folder: "/Users/sam/src/site")
        #expect(elsewhere.selectedPreset == nil)
        #expect(elsewhere.selectedAgent?.id == "claude")

        // Turning the toggle off forgets the folder.
        next.rememberFolder = false
        _ = await next.create()
        #expect(await library.rememberedPreset(host: Samples.studio, cwd: folder) == nil)
    }

    @Test func flagsDisabledWithoutCapability() async {
        let model = model(library: .inMemory([kimi]), launch: false)
        await ready(model)
        #expect(!model.flagsAvailable)
        #expect(model.flagsFooter == "Update the helper on Studio to use flags.")
        #expect(!model.canUse(kimi))
        model.flags = "--yolo"
        _ = await model.create()
        #expect(created == CreateSessionParams(agent: "claude", cwd: folder))
    }

    /// The agent editor offers a check only on machines that can answer it without fuss: online,
    /// with a helper that advertised launching agents.
    @Test func agentChecksOnlyOnOnlineLaunchMachines() {
        func section(_ reach: Reachability, caps: Set<String>, helper: HelperState = .present(version: "0.5.0", live: true))
            -> MachineSection {
            MachineSection(
                id: Samples.studio, name: "Studio", symbol: .desktop, reachability: reach, helper: helper,
                sessions: [], lastSeen: Samples.now, capabilities: caps
            )
        }
        #expect(SettingsMachine.checksAgents(section(.online, caps: [Capability.launch])))
        #expect(!SettingsMachine.checksAgents(section(.online, caps: [Capability.events])), "an older helper")
        #expect(!SettingsMachine.checksAgents(section(.offline(since: Samples.now, reason: "Asleep"), caps: [Capability.launch])))
        #expect(!SettingsMachine.checksAgents(section(.authNeeded, caps: [Capability.launch])), "it would ask to sign in")
        #expect(!SettingsMachine.checksAgents(section(.online, caps: [Capability.launch], helper: .missing)))
        #expect(!SettingsMachine.checksAgents(nil))
        let machines = [
            SettingsMachine(id: Samples.studio, name: "Studio", address: "a", symbol: .desktop, helper: .missing, checksAgents: true),
            SettingsMachine(id: Samples.build, name: "Build", address: "b", symbol: .server, helper: .missing),
        ]
        #expect(AgentEditor.checkable(machines).map(\.id) == [Samples.studio])
    }

    @Test func smartPunctuationTurnsBackIntoFlags() {
        #expect(FlagsField.plain("\u{2014}yolo \u{201C}a b\u{201D} \u{2018}c\u{2019}") == #"--yolo "a b" 'c'"#)
        #expect(FlagsField.plain("\u{2013}model opus \u{2212}\u{2212}yolo \u{2010}p") == "--model opus --yolo -p")
        #expect(FlagsField.plain("--a\n--b") == "--a --b")
        #expect(FlagsField.plain("--dangerously-skip-permissions") == "--dangerously-skip-permissions")
        // Inside a word the typed character stays: an apostrophe, or a dash in a prompt.
        #expect(FlagsField.plain("-p 'it\u{2019}s fine'") == "-p 'it\u{2019}s fine'")
        #expect(FlagsField.plain("-p 'a\u{2014}b'") == "-p 'a\u{2014}b'")
    }

    /// Command fields (terminal input, replies, the composer) straighten the same way but keep
    /// their line breaks: `npm test -- --watch` and `git commit -m "fix"` reach the session.
    @Test func commandInputStraightensAndKeepsLines() {
        #expect(CommandInput.straight("npm test \u{2014} \u{2014}watch") == "npm test -- --watch")
        #expect(CommandInput.straight("git commit -m \u{201C}fix\u{201D}") == #"git commit -m "fix""#)
        #expect(CommandInput.straight("line one\n\u{2014}two") == "line one\n--two")
        #expect(CommandInput.straight("don\u{2019}t") == "don\u{2019}t")
    }

    @Test func emptyQuotesBlockStart() async {
        let model = model(library: .inMemory())
        await ready(model)
        model.flags = "--model ''"
        #expect(model.flagsError == "Empty quotes make an empty flag. Remove them or put text inside.")
        #expect(!model.canStart)
        #expect(await model.create() == nil)
        #expect(created == nil)
        model.flags = #"--model """#
        #expect(!model.canStart)
    }

    @Test func invisibleCharactersBlockStart() async {
        let model = model(library: .inMemory())
        await ready(model)
        model.flags = "--name a\u{202E}b"
        #expect(model.flagsError == "Flags can't contain invisible characters.")
        #expect(!model.canStart)
    }

    @Test func editorRefusesPresetsThatCannotStart() {
        let draft = AgentPreset(id: kimi.id, name: " Kimi ", agent: AgentPreset.custom, program: "kimi")
        #expect(AgentEditor.preset(from: draft, flags: "--yolo") == kimi)
        #expect(AgentEditor.preset(from: draft, flags: "--yolo ''") == nil)
        #expect(AgentEditor.preset(from: draft, flags: "'--yolo") == nil)
        #expect(AgentEditor.preset(from: draft, flags: "\u{200B}") == nil)
        var unnamed = draft
        unnamed.name = "  "
        #expect(AgentEditor.preset(from: unnamed, flags: "") == nil)
        var noProgram = draft
        noProgram.program = ""
        #expect(AgentEditor.preset(from: noProgram, flags: "") == nil)
    }

    /// A second start while the first is still reading (a Shortcuts query on a cold launch)
    /// waits for that read instead of seeing no presets.
    @Test func secondStartWaitsForTheFirstLoad() async {
        let records = SyncedRecordStore<AgentPreset>(
            prefix: AgentPreset.storePrefix, local: MemoryBackend(), remote: nil, deviceID: "memory", clock: { Date() }
        )
        let launches = SyncedRecordStore<FolderLaunch>(
            prefix: FolderLaunchMemory.storePrefix, local: MemoryBackend(), remote: nil, deviceID: "memory", clock: { Date() }
        )
        await records.put(kimi.id.uuidString, kimi)
        let library = AgentLibrary(presets: records, launches: launches)
        let first = Task { await library.start() }
        await Task.yield()
        await library.start()
        #expect(library.presets.map(\.id) == [kimi.id])
        await first.value
    }

    @Test func deletedPresetDoesNotLendItsFlags() async {
        let library = AgentLibrary.inMemory([kimi])
        let model = model(library: library)
        await ready(model)
        model.choose(preset: kimi)
        #expect(model.canStart)
        await library.delete(kimi.id)
        #expect(model.selectedPreset == nil)
        #expect(model.selectedRegistryID == nil)
        #expect(!model.canStart)
        #expect(await model.create() == nil)
        #expect(created == nil)
        model.choose(agent: "claude")
        #expect(model.flags == "")
        #expect(model.canStart)
    }

    @Test func deletingAPresetForgetsItsFolders() async {
        let library = AgentLibrary.inMemory([kimi])
        await library.remember(kimi.id, host: Samples.studio, cwd: folder)
        await library.delete(kimi.id)
        #expect(await library.rememberedPreset(host: Samples.studio, cwd: folder) == nil)
    }
}
