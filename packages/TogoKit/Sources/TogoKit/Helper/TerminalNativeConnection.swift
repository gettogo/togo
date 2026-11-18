import Foundation

/// Menu ownership is bound to this connection. Commands, reads and actions
/// must not move to a request-scoped helper or reconnect behind the caller.
public protocol TerminalNativeConnection: Sendable {
    func harnessAction(_ params: HarnessActionParams) async throws -> HarnessActionResult
    func harnessScreen(id: String, screen: String, query: [String: String]?) async throws -> HarnessScreen
    func close() async
}

extension HelperClient: TerminalNativeConnection {}
