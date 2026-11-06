import Foundation

extension HelperClient {
    // MARK: - Launch and lifecycle

    /// Looks a program up on the host's login-shell PATH before it is offered as an agent.
    public func checkAgent(program: String) async throws -> AgentCheck {
        try await readyCall(.agentsCheck, AgentsCheckParams(program: program), needs: Capability.launch)
    }

    /// Runs an ended or failed session's command again in place. Throws `.conflict("running")`
    /// while the agent still runs.
    public func relaunch(id: String) async throws -> Session {
        try await mapped { try await readyCall(.sessionRelaunch, SessionIDParams(id: id), needs: Capability.launch) }
    }

    // MARK: - Restore

    /// Sessions a host restart ended, newest first.
    public func restorable() async throws -> [RestorableSession] {
        let list: RestorableList = try await readyCall(.sessionRestorable, EmptyParams(), needs: Capability.restore)
        return list.sessions
    }

    public func restore(id: String, resume: Bool) async throws -> Session {
        try await mapped {
            try await readyCall(.sessionRestore, RestoreParams(id: id, resume: resume), needs: Capability.restore)
        }
    }

    /// Deletes a restorable session's record. Nothing runs.
    public func dismissRestorable(id: String) async throws {
        let _: EmptyResult = try await mapped {
            try await readyCall(.sessionDismiss, SessionIDParams(id: id), needs: Capability.restore)
        }
    }

    // MARK: - Reply

    /// Types `text` into the agent waiting at its prompt; `submit` presses Enter after it.
    /// Throws `.conflict` with `shell`, `ended` or `needs_you` when nothing was typed.
    public func sendText(id: String, text: String, submit: Bool) async throws {
        let _: EmptyResult = try await mapped {
            try await readyCall(.sessionSendText, SendTextParams(id: id, text: text, submit: submit), needs: Capability.reply)
        }
    }

    // MARK: - Notification settings

    public func notifySettings() async throws -> NotifySettings {
        try await readyCall(.notifyGet, EmptyParams(), needs: Capability.notify)
    }

    public func setNotify(_ patch: NotifyPatch) async throws -> NotifySettings {
        try await readyCall(.notifySet, patch, needs: Capability.notify)
    }

    // MARK: - Authorized keys and host info

    /// Removes this device's key from the host's `authorized_keys` and returns how many lines went.
    public func removeAuthorizedKey(publicKey: String) async throws -> Int {
        let result: AuthkeysRemoveResult = try await mapped {
            try await readyCall(.authkeysRemove, AuthkeysRemoveParams(publicKey: publicKey), needs: Capability.authkeys)
        }
        return result.removed
    }

    public func hostInfo() async throws -> HostInfo {
        try await readyCall(.hostInfo, EmptyParams(), needs: Capability.hostInfo)
    }

    // MARK: - Keep awake

    public func awakeStatus() async throws -> AwakeStatus {
        try await readyCall(.awakeStatus, EmptyParams(), needs: Capability.awake)
    }

    /// Throws `.refused("needs_admin_setup")` for battery mode before its one-time setup.
    public func setAwake(mode: AwakeMode?, batteryFloor: Int?) async throws -> AwakeStatus {
        try await mapped {
            try await readyCall(.awakeSet, AwakeSetParams(mode: mode, batteryFloor: batteryFloor), needs: Capability.awake)
        }
    }

    /// Turns the helper's `-32006` and `-32008` errors into `.conflict` and `.refused`
    /// with the reason it names.
    private func mapped<R: Sendable>(_ body: () async throws -> R) async throws -> R {
        do {
            return try await body()
        } catch HelperError.rpc(let error) where error.code == RPCErrorCode.conflict {
            throw Self.conflict(error)
        } catch HelperError.rpc(let error) where error.code == RPCErrorCode.refused {
            throw HelperError.refused(error.reason ?? "")
        }
    }
}
