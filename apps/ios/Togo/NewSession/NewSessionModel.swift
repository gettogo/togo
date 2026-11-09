import Foundation
import Observation
import TogoKit

enum Load<T: Equatable>: Equatable {
    case idle, loading, loaded(T), failed(String)
}

@MainActor @Observable final class NewSessionModel {
    /// `browse` is the folder browser, a page above the folder step that is still that step.
    enum Step: Hashable, Sendable { case machine, folder, browse, agent }

    /// A machine the session can start on, with the reason it cannot when it is unavailable.
    struct Machine: Identifiable, Equatable {
        var id: HostID
        var name: String
        var symbol: HostSymbol
        var unavailable: String?
        var helperMissing: Bool
        var sessionCount: Int
        /// What the machine's helper offers, such as `launch.v1` for flags and presets.
        var capabilities: Set<String> = []
    }

    /// The most sessions a machine holds: bibs run from 01 to 99.
    static let sessionLimit = 99

    private(set) var machines: [Machine]
    var step: Step {
        didSet {
            // Leaving the folder step drops any listing still on its way.
            if step != oldValue, step != .folder, step != .browse { cancelBrowse() }
        }
    }
    var hostID: HostID?
    var recent: Load<[String]> = .idle
    var browsing: Load<ListDirResult> = .idle
    var cwd: String?
    var agents: Load<[AgentInfo]> = .idle
    var agentID: String?
    /// A saved preset chosen instead of a registry agent.
    var presetID: UUID?
    /// Flags for this start, split into arguments by `ArgsParser`.
    var flags = ""
    /// Whether starting saves the chosen preset as this folder's.
    var rememberFolder = false
    /// The preset this folder last started with on this machine.
    var rememberedPreset: UUID?
    /// A preset asked for from outside the sheet, such as a shortcut. It wins over the
    /// folder's remembered one whenever the machine can start it, in any folder, until the
    /// user picks another machine.
    var requestedPresetID: UUID?
    var showHidden = false
    var name = ""
    var creating = false
    private(set) var createError: String?

    private var browsePath: String?
    private var recentToken = 0
    private var browseToken = 0
    private var agentsToken = 0
    private let services: any AppServices
    private let library: AgentLibrary
    /// The favorites as the flow opened. The list keeps this order while it is open, so a
    /// star toggled now changes only the star; the next new session lists it in its place.
    private let favoritesAtOpen: [String]
    private let open: (HostID, String, String?) -> Void

    init(
        services: any AppServices, hostID: HostID?, machines: [MachineSection] = [],
        library: AgentLibrary = .inMemory(),
        open: @escaping (HostID, String, String?) -> Void = { _, _, _ in }
    ) {
        self.services = services
        self.library = library
        self.open = open
        favoritesAtOpen = library.favorites
        let choices = machines.isEmpty ? Self.choices(from: services.hosts) : machines.map(Self.choice(from:))
        self.machines = choices
        let preselected = hostID ?? (choices.count == 1 ? choices[0].id : nil)
        self.hostID = preselected
        skipsMachineStep = preselected != nil
        step = preselected != nil ? .folder : .machine
    }

    /// True when the flow opens on the folder step because there was nothing to choose.
    let skipsMachineStep: Bool

    var firstStep: Step { skipsMachineStep ? .folder : .machine }

    /// The steps pushed on top of the first one to reach `step`.
    var pushedSteps: [Step] {
        // The browser opens above the folder step; choosing a folder there replaces it.
        let order: [Step] = step == .browse ? [.machine, .folder, .browse] : [.machine, .folder, .agent]
        guard let start = order.firstIndex(of: firstStep), let end = order.firstIndex(of: step) else { return [] }
        return Array(order[start...end].dropFirst())
    }

    var machine: Machine? { machines.first { $0.id == hostID } }
    var machineName: String { machine?.name ?? NewSessionCopy.thisMachine }
    var helperMissing: Bool { machine?.helperMissing ?? false }

    /// Installed agents first, then the ones missing from the machine, each part in
    /// `AgentOrder`. Shell is always offered.
    var offeredAgents: [AgentInfo] { installedAgents + missingAgents }

    /// The agents the machine can start: favorites, then the rest, each by name.
    var installedAgents: [AgentInfo] { installedSections.all }

    /// The installed agents split into the favorites and the rest, as the flow opened.
    var installedSections: AgentOrder.Sections<AgentInfo> {
        AgentOrder.sections(loadedAgents.filter(\.installed), favorites: favoritesAtOpen)
    }

    /// The agents the machine lacks, shown on request, by name.
    var missingAgents: [AgentInfo] {
        AgentOrder.byName(loadedAgents.filter { !$0.installed }, name: \.name)
    }

    private var loadedAgents: [AgentInfo] {
        guard case .loaded(var list) = agents else { return [] }
        if !list.contains(where: { $0.id == "shell" }) {
            list.append(AgentInfo(id: "shell", name: "Shell", installed: true))
        }
        return list
    }

    func isFavorite(_ agent: String) -> Bool { library.isFavorite(agent) }

    func toggleFavorite(_ agent: String) { library.toggleFavorite(agent) }

    var selectedAgent: AgentInfo? {
        offeredAgents.first { $0.id == agentID && $0.installed } ?? offeredAgents.first { $0.installed }
    }

    var presets: [AgentPreset] { library.presets }

    /// Flags and presets need a helper that takes a program and its arguments.
    var flagsAvailable: Bool { machine?.capabilities.contains(Capability.launch) ?? false }

    var flagsFooter: String? { flagsAvailable ? nil : NewSessionCopy.flagsNeedUpdate(machineName) }

    func canUse(_ preset: AgentPreset) -> Bool {
        guard flagsAvailable else { return false }
        return preset.isCustom || offeredAgents.contains { $0.id == preset.agent && $0.installed }
    }

    var selectedPreset: AgentPreset? {
        library.preset(presetID).flatMap { canUse($0) ? $0 : nil }
    }

    /// The registry agent that shows as chosen; nil while a preset is, or was until it went away.
    var selectedRegistryID: String? { presetID == nil ? selectedAgent?.id : nil }

    /// The chosen preset was deleted, perhaps on another device, while the sheet was open. Its
    /// flags must not start some other agent, so nothing starts until the user chooses again.
    var presetMissing: Bool { presetID != nil && selectedPreset == nil }

    func isRemembered(_ preset: AgentPreset) -> Bool { preset.id == rememberedPreset }

    /// A shell takes no arguments, so it has no flags field.
    var showsFlags: Bool {
        if selectedPreset != nil { return true }
        guard let agent = selectedAgent else { return false }
        return agent.id != "shell"
    }

    var flagsError: String? {
        guard flagsAvailable, showsFlags else { return nil }
        return FlagsField.problem(flags)
    }

    var startTitle: String {
        NewSessionCopy.start(selectedPreset?.name ?? selectedAgent?.name ?? "")
    }

    var canStart: Bool {
        guard !creating else { return false }
        if helperMissing { return true }
        guard !presetMissing else { return false }
        return (selectedPreset != nil || selectedAgent != nil) && flagsError == nil
    }

    func choose(agent id: String) {
        agentID = id
        presetID = nil
        flags = ""
        rememberFolder = false
    }

    /// Fills the flags with the preset's own; edits apply to this start only.
    func choose(preset: AgentPreset) {
        presetID = preset.id
        flags = ArgsParser.join(preset.args)
        rememberFolder = isRemembered(preset)
    }

    /// Loads what the first visible step needs. Safe to call again; it never replaces data
    /// that is already there.
    func start() async {
        guard hostID != nil, !helperMissing else { return }
        switch step {
        case .machine: break
        case .folder, .browse: if recent == .idle { await loadRecent() }
        case .agent: if agents == .idle { await loadAgents() }
        }
    }

    func choose(_ machine: Machine) async {
        if hostID != machine.id {
            hostID = machine.id
            recent = .idle
            browsing = .idle
            cwd = nil
            agents = .idle
            agentID = nil
            clearPreset()
            requestedPresetID = nil
        }
        createError = nil
        step = .folder
        guard !machine.helperMissing else { return }
        if recent == .idle { await loadRecent() }
    }

    func loadRecent() async {
        guard let hostID else { return }
        recentToken += 1
        let token = recentToken
        recent = .loading
        do {
            let dirs = try await services.recentDirs(hostID: hostID)
            if recentToken == token, self.hostID == hostID { recent = .loaded(RecentFolders.shown(dirs)) }
        } catch {
            if recentToken == token, self.hostID == hostID { recent = .failed(message(for: error)) }
        }
    }

    /// Takes `dir` off the recent folders: off this list at once (`removeRecent`), then off the
    /// machine's record. A helper that cannot forget one lists it again next time.
    func removeRecent(_ dir: String) {
        guard case .loaded(let dirs) = recent else { return }
        recent = .loaded(dirs.filter { $0 != dir })
    }

    func forgetRecent(_ dir: String) async {
        guard let hostID else { return }
        try? await services.forgetRecentDir(hostID: hostID, path: dir)
    }

    func browse(_ path: String) async {
        guard let hostID else { return }
        browseToken += 1
        let token = browseToken
        browsePath = path
        browsing = .loading
        do {
            let result = try await services.listDir(hostID: hostID, path: path)
            if browseToken == token, self.hostID == hostID { browsing = .loaded(result) }
        } catch {
            if browseToken == token, self.hostID == hostID { browsing = .failed(message(for: error)) }
        }
    }

    /// Tries the folder that failed again, not the home folder.
    func retryBrowse() async {
        await browse(browsePath ?? "~")
    }

    func leaveBrowsing() {
        cancelBrowse()
        browsing = .idle
    }

    /// Only the newest request may change `browsing`, so a late answer cannot undo a back step.
    private func cancelBrowse() {
        browseToken += 1
        if browsing == .loading { browsing = .idle }
    }

    func choose(folder: String) async {
        if cwd != folder { clearPreset() }
        cwd = folder
        createError = nil
        leaveBrowsing()
        step = .agent
        if agents == .idle {
            await loadAgents()
        } else if case .loaded = agents {
            await applyRemembered()
        }
    }

    private func clearPreset() {
        presetID = nil
        flags = ""
        rememberFolder = false
        rememberedPreset = nil
    }

    /// Chooses the preset this folder last started with, unless the user already chose one.
    private func applyRemembered() async {
        guard let hostID, let cwd else { return }
        let id = await library.rememberedPreset(host: hostID, cwd: cwd)
        guard self.hostID == hostID, self.cwd == cwd else { return }
        rememberedPreset = id
        let wanted = [requestedPresetID, id].lazy.compactMap { self.library.preset($0) }.first { self.canUse($0) }
        if presetID == nil, let preset = wanted { choose(preset: preset) }
    }

    func loadAgents() async {
        guard let hostID else { return }
        agentsToken += 1
        let token = agentsToken
        agents = .loading
        do {
            let list = try await services.agents(hostID: hostID)
            guard agentsToken == token, self.hostID == hostID else { return }
            agents = .loaded(list)
            agentID = agentID ?? selectedAgent?.id
            await applyRemembered()
        } catch {
            if agentsToken == token, self.hostID == hostID { agents = .failed(message(for: error)) }
        }
    }

    /// Starts the session and hands it to the workspace. Returns nil, with `createError` set,
    /// when the machine refuses.
    func create() async -> SessionRow? {
        guard let hostID, canStart else { return nil }
        creating = true
        createError = nil
        do {
            let started = selectedPreset?.agent ?? selectedAgent?.id
            let row = try await services.create(hostID: hostID, params: params())
            if let started, started != AgentPreset.custom { library.used(started) }
            await rememberChoice(hostID: hostID)
            open(row.hostID, row.id, row.agent)
            // Stays set: the sheet is about to close and must not accept a second start.
            return row
        } catch {
            creating = false
            createError = message(for: error)
            return nil
        }
    }

    private func rememberChoice(hostID: HostID) async {
        guard let preset = selectedPreset, let cwd else { return }
        if rememberFolder {
            await library.remember(preset.id, host: hostID, cwd: cwd)
        } else if rememberedPreset == preset.id {
            await library.remember(nil, host: hostID, cwd: cwd)
        }
    }

    /// The name goes to the helper as typed and each flag as its own argument, so quotes and
    /// shell syntax are plain characters.
    private func params() -> CreateSessionParams {
        if helperMissing { return CreateSessionParams() }
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let name = trimmed.isEmpty ? nil : trimmed
        let parsed = flagsAvailable && showsFlags ? (try? ArgsParser.split(flags)) ?? [] : []
        let args = parsed.isEmpty ? nil : parsed
        if let preset = selectedPreset {
            var params = preset.launch(cwd: cwd, name: name)
            params.args = args
            return params
        }
        return CreateSessionParams(agent: selectedAgent?.id, cwd: cwd, name: name, args: args)
    }

    private func message(for error: any Error) -> String {
        if case .rpc(let rpc)? = error as? HelperError {
            if rpc.code == RPCErrorCode.invalidPath { return NewSessionCopy.badFolder }
            if rpc.code == RPCErrorCode.invalidParams, (machine?.sessionCount ?? 0) >= Self.sessionLimit {
                return NewSessionCopy.full(machineName)
            }
        }
        return ServiceErrors.message(error)
    }

    private static func choice(from section: MachineSection) -> Machine {
        Machine(
            id: section.id, name: section.name, symbol: section.symbol,
            unavailable: unavailableReason(section.reachability),
            helperMissing: section.helper == .missing,
            sessionCount: section.sessions.filter(\.managed).count,
            capabilities: section.capabilities
        )
    }

    private static func choices(from hosts: [HostRecordView]) -> [Machine] {
        hosts.map {
            Machine(id: $0.id, name: $0.name, symbol: $0.symbol, unavailable: nil, helperMissing: false, sessionCount: 0)
        }
    }

    private static func unavailableReason(_ reachability: Reachability) -> String? {
        switch reachability {
        case .online: nil
        case .connecting: NewSessionCopy.connecting
        case .waking: NewSessionCopy.waking
        case .offline(_, let reason): NewSessionCopy.offline(reason)
        case .authNeeded: NewSessionCopy.authNeeded
        case .failed(let reason): reason
        case .keyMissing: NewSessionCopy.keyMissing(device: DeviceName.model)
        case .setUpHere: NewSessionCopy.setUpHere(device: DeviceName.model)
        case .hostKeyChanged: ControlCenterCopy.hostKeyChanged
        case .deviceOffline: ControlCenterCopy.deviceOffline(DeviceName.model)
        }
    }
}
