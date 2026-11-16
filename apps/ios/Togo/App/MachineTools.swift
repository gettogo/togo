import SwiftUI
import TogoKit

/// How a feature reaches a machine's helper for a request of its own. Features write small
/// typed wrappers over `call` in their own files, so nothing here grows with them. What a
/// machine can do is read from its snapshot's capabilities, never from here.
@MainActor protocol MachineTools: AnyObject {
    func call<T: Sendable>(_ host: HostID, _ body: @Sendable (HelperClient) async throws -> T) async throws -> T
    /// Background reads must never create an authentication question.
    func quietCall<T: Sendable>(_ host: HostID, _ body: @Sendable (HelperClient) async throws -> T) async throws -> T
}

enum MachineToolsAccess: Error { case backgroundUnavailable }

extension MachineTools {
    /// Providers must explicitly support noninteractive access; never fall back to `call`.
    func quietCall<T: Sendable>(_ host: HostID, _ body: @Sendable (HelperClient) async throws -> T) async throws -> T {
        throw MachineToolsAccess.backgroundUnavailable
    }
}

/// The tools of an app with no machines to reach: previews and views built without a store.
@MainActor final class NoMachineTools: MachineTools {
    nonisolated init() {}

    func call<T: Sendable>(_ host: HostID, _ body: @Sendable (HelperClient) async throws -> T) async throws -> T {
        throw LiveServiceError.unknownMachine
    }
}

extension EnvironmentValues {
    @Entry var machineTools: any MachineTools = NoMachineTools()
}
