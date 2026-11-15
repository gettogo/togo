import Foundation
import TogoKit

extension CommandLineModel {
    var nativeCommandsAvailable: Bool {
        (routes.has(HarnessCapability.surface) || routes.has(HarnessCapability.terminalNative)) && routes.nativeCommand != nil
    }

    func opensOriginalScreen(_ command: HarnessCompletion, arguments: String = "") -> Bool {
        guard nativeCommandsAvailable, command.action == nil, command.opens == nil, let metadata = command.native else { return false }
        if routes.has(HarnessCapability.terminalNative), command.screen == nil, metadata.args != "required" { return true }
        let intent = metadata.intent(hasArguments: !arguments.isEmpty)
        return ["picker", "settings", "signin", "screen", "input", "text"].contains(intent)
    }

    /// Runs every command that reaches the typed-send path in the original
    /// native composer. API-native choices/screens already took their own path.
    func sendNativeCommand(_ text: String) async throws -> Bool {
        guard nativeCommandsAvailable, CommandLineLogic.commandWord(text) != nil,
              let send = routes.nativeCommand else { return false }
        let commands = await routingCommands()
        let command: HarnessCompletion?
        if case .command(let matched, _) = CommandLineLogic.typed(text, commands: commands) { command = matched }
        else { command = nil }
        try await send(text, command)
        return true
    }
}
