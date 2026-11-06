import Foundation
import TogoKit

/// What `LiveServices` needs from a machine's monitor; `HostMonitor` in the app.
protocol HostWatch: Sendable {
    /// Single consumer: `LiveServices`.
    var snapshots: AsyncStream<HostSnapshot> { get }
    func start() async
    func stop() async
    func setForeground(_ on: Bool) async
    func refresh() async
    func perform(_ op: SessionOp) async throws -> Session?
}

extension HostMonitor: HostWatch {}

/// Helper requests outside the monitor's connection; `HelperChannels` in the app. A request
/// with a `prompter` may ask the user for a password or code if its channel has to sign in.
protocol HelperCalling: Sendable {
    func call<T: Sendable>(
        _ record: HostRecord, prompter: (any ConnectionPrompter)?, _ body: @Sendable (HelperClient) async throws -> T
    ) async throws -> T
    func drop(_ id: UUID) async
    func dropAll() async
}

extension HelperChannels: HelperCalling {}

enum LiveServiceError: Error, LocalizedError {
    case unknownMachine
    case needsHelper

    var errorDescription: String? {
        switch self {
        case .unknownMachine: String(localized: "live.unknownMachine", defaultValue: "This machine is no longer in Togo.")
        case .needsHelper: String(localized: "live.needsHelper", defaultValue: "This needs the Togo helper on the machine.")
        }
    }
}
