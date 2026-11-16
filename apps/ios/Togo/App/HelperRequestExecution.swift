import Foundation
import TogoKit

/// A closed connection cannot tell whether its last request took effect. Reconnect for the
/// next call, but never replay a body that may have typed, answered or changed something.
enum HelperRequestExecution {
    static func once<Value: Sendable>(
        _ body: @Sendable () async throws -> Value, onClosed: @Sendable () async -> Void
    ) async throws -> Value {
        do {
            return try await body()
        } catch HelperError.closed {
            await onClosed()
            throw HelperCallInterrupted()
        }
    }
}

struct HelperCallInterrupted: LocalizedError, Sendable {
    var errorDescription: String? {
        String(localized: "error.helper.interrupted", defaultValue:
            "The connection closed before the result arrived. The request may have completed. Check before trying again.")
    }
}
