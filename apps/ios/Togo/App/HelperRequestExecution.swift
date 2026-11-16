import Foundation
import TogoKit

/// A closed connection cannot tell whether its last request took effect. Reconnect for the
/// next call, but never replay a body that may have typed, answered or changed something.
enum HelperRequestExecution {
    static func once<Value: Sendable>(
        _ body: @Sendable () async throws -> Value, onClosed: @Sendable () async -> Void,
        onTimedOut: @Sendable (String) async -> Void = { _ in }
    ) async throws -> Value {
        do {
            return try await body()
        } catch HelperError.closed {
            await onClosed()
            throw HelperCallInterrupted()
        } catch HelperError.timeout(let method) {
            await onTimedOut(method)
            throw HelperCallInterrupted(reason: .timedOut)
        }
    }
}

struct HelperCallInterrupted: LocalizedError, Sendable {
    enum Reason: Sendable { case closed, timedOut }
    var reason: Reason = .closed

    var errorDescription: String? {
        if reason == .timedOut {
            return String(localized: "error.helper.resultTimedOut", defaultValue:
                "The result did not arrive in time. The request may have completed. Check before trying again.")
        }
        return String(localized: "error.helper.interrupted", defaultValue:
            "The connection closed before the result arrived. The request may have completed. Check before trying again.")
    }
}
