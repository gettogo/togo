import Foundation
import Observation
import TogoKit

enum Load<T: Equatable>: Equatable {
    case idle, loading, loaded(T), failed(String)
}

@MainActor @Observable final class NewSessionModel {
    enum Step: Hashable, Sendable { case machine, folder, agent }

    /// A machine the session can start on, with the reason it cannot when it is unavailable.
    struct Machine: Identifiable, Equatable {
        var id: HostID
        var name: String
        var symbol: HostSymbol
        var unavailable: String?
        var helperMissing: Bool
        var sessionCount: Int
    }

    /// The most sessions a machine holds: bibs run from 01 to 99.
    static let sessionLimit = 99

    private(set) var machines: [Machine]
    var step: Step {
        didSet {
            // Leaving the folder step drops any listing still on its way.
            if step != oldValue, step != .folder { cancelBrowse() }
        }
    }
    var hostID: HostID?
    var recent: Load<[String]> = .idle
    var browsing: Load<ListDirResult> = .idle
    var cwd: String?
    var agents: Load<[AgentInfo]> = .idle
    var agentID: String?
    var showHidden = false
    var name = ""
    var creating = false
    private(set) var createError: String?

    private var browsePath: String?
    private var recentToken = 0
    private var browseToken = 0
    private var agentsToken = 0
    private let services: any AppServices
    private let open: (HostID, String) -> Void

    init(
        services: any AppServices, hostID: HostID?, machines: [MachineSection] = [],
        open: @escaping (HostID, String) -> Void = { _, _ in }
    ) {
        self.services = services
        self.open = open
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
        let order: [Step] = [.machine, .folder, .agent]
        guard let start = order.firstIndex(of: firstStep), let end = order.firstIndex(of: step) else { return [] }
        return Array(order[start...end].dropFirst())
    }

    var machine: Machine? { machines.first { $0.id == hostID } }
    var machineName: String { machine?.name ?? NewSessionCopy.thisMachine }
    var helperMissing: Bool { machine?.helperMissing ?? false }

    /// Installed agents first, then the ones missing from the machine. Shell is always offered.
    var offeredAgents: [AgentInfo] {
        guard case .loaded(var list) = agents else { return [] }
        if !list.contains(where: { $0.id == "shell" }) {
            list.append(AgentInfo(id: "shell", name: "Shell", installed: true))
        }
        return list.filter(\.installed) + list.filter { !$0.installed }
    }

    var selectedAgent: AgentInfo? {
        offeredAgents.first { $0.id == agentID && $0.installed } ?? offeredAgents.first { $0.installed }
    }

    var canStart: Bool {
        guard !creating else { return false }
        return helperMissing || selectedAgent != nil
    }

    /// Loads what the first visible step needs. Safe to call again; it never replaces data
    /// that is already there.
    func start() async {
        guard hostID != nil, !helperMissing else { return }
        switch step {
        case .machine: break
        case .folder: if recent == .idle { await loadRecent() }
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
            if recentToken == token, self.hostID == hostID { recent = .loaded(dirs) }
        } catch {
            if recentToken == token, self.hostID == hostID { recent = .failed(message(for: error)) }
        }
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
        cwd = folder
        createError = nil
        leaveBrowsing()
        step = .agent
        if agents == .idle { await loadAgents() }
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
            let row = try await services.create(hostID: hostID, params: params())
            open(row.hostID, row.id)
            // Stays set: the sheet is about to close and must not accept a second start.
            return row
        } catch {
            creating = false
            createError = message(for: error)
            return nil
        }
    }

    /// The name goes to the helper as typed, so quotes and shell syntax are plain characters.
    private func params() -> CreateSessionParams {
        if helperMissing { return CreateSessionParams() }
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        return CreateSessionParams(agent: selectedAgent?.id, cwd: cwd, name: trimmed.isEmpty ? nil : trimmed)
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
            sessionCount: section.sessions.filter(\.managed).count
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
        }
    }
}
