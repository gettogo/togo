import Foundation
import TogoKit

extension CommandLineModel {
    var nativeCommandsAvailable: Bool {
        routes.has(HarnessCapability.surface) && routes.nativeCommand != nil
    }

    /// Runs every command that reaches the typed-send path in the original
    /// native composer. API-native choices/screens already took their own path.
    func sendNativeCommand(_ text: String) async throws -> Bool {
        guard nativeCommandsAvailable, CommandLineLogic.commandWord(text) != nil,
              let send = routes.nativeCommand else { return false }
        try await send(text)
        return true
    }
}
