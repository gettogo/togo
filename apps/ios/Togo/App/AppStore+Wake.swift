import Foundation
import TogoKit

extension AppStore {
    /// Sends the machine's stored wake target and says what happened under its name. A
    /// machine that reports network wake as off is not sent anything.
    func wake(_ id: HostID) async {
        guard let machine = controlCenter.machines.first(where: { $0.id == id }), let target = machine.wake else { return }
        if target.enabled == false {
            wakeNotes[id] = WakeCopy.off(machine.name)
            return
        }
        do {
            try await sendWake(target)
            wakeNotes[id] = WakeCopy.sent(machine.name)
        } catch {
            wakeNotes[id] = WakeCopy.failed(machine.name)
        }
    }
}
