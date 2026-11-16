import Foundation
import TogoKit

extension CommandLineModel {
    /// Sets the mode through `ActionGate`: a mode change that restarts the agent asks while it
    /// works. The chip keeps the new mode until the session reports another.
    func setMode(_ id: String) async {
        guard id != mode, let choice = modes.first(where: { $0.id == id }) else { return }
        let previous = modes.first { $0.id == mode }
        let over = routes.mode()
        let revision = routes.modeRevision()
        let change = GatedChange(
            action: "set_mode", args: ["id": .string(id)], confirm: nil, setting: PrimitiveCopy.mode, value: choice.label,
            previous: previous.map { ($0.label, ["id": .string($0.id)]) }
        )
        await gate.change(change, context: gateContext) { [weak self] result in
            guard let self else { return }
            switch result?.result {
            case .failed?, .fallback?, nil:
                if result != nil { self.notice = result?.message ?? ComposerCopy.modeNotChanged }
            default:
                self.modeOverride = (id, over, revision)
                self.notice = nil
                self.paletteOpen = false
            }
        }
    }

    /// The mode after the current one, wrapping: Shift+Tab steps through the modes as it does
    /// at the desk. nil when the session has no modes to step through.
    var nextMode: String? {
        guard showsModeChip, modes.count > 1 else { return nil }
        let index = modes.firstIndex { $0.id == mode } ?? -1
        return modes[(index + 1) % modes.count].id
    }

    /// Shift+Tab in the field: the next mode, through the chip's own verified path.
    func cycleMode() async {
        guard let next = nextMode else { return }
        await setMode(next)
    }

}
