import Foundation

extension HelperClient {
    public func createSession(_ params: CreateSessionParams) async throws -> Session {
        try await readyCall(.sessionCreate, params)
    }

    public func killSession(id: String) async throws {
        let _: EmptyResult = try await readyCall(.sessionKill, SessionIDParams(id: id))
    }

    public func renameSession(id: String, name: String) async throws -> Session {
        try await readyCall(.sessionRename, RenameParams(id: id, name: name))
    }

}
