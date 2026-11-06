import Foundation
import TogoKit

/// `MachineTools` over the app's helper channels: it finds the machine's record and makes
/// the request on the machine's one kept connection. It adds nothing else.
@MainActor final class LiveMachineTools: MachineTools {
    private let channels: any HelperCalling
    private let records: (HostID) -> HostRecord?
    private let prompter: (HostRecord) -> (any ConnectionPrompter)?

    init(
        channels: any HelperCalling, records: @escaping (HostID) -> HostRecord?,
        prompter: @escaping (HostRecord) -> (any ConnectionPrompter)?
    ) {
        self.channels = channels
        self.records = records
        self.prompter = prompter
    }

    func call<T: Sendable>(_ host: HostID, _ body: @Sendable (HelperClient) async throws -> T) async throws -> T {
        guard let record = records(host) else { throw LiveServiceError.unknownMachine }
        guard !record.noHelper else { throw LiveServiceError.needsHelper }
        return try await channels.call(record, prompter: prompter(record), body)
    }
}
