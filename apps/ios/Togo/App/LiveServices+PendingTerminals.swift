import Foundation
import TogoKit
import UIKit

/// Terminals asked for before the stored machines are read. A tab restored at launch, or a row
/// listed from this device's own copy of the machines, can ask for its terminal while `start()`
/// is still deferred until after the first screen. Its machine is not unknown yet, only not
/// loaded: the tab shows connecting and attaches once the stores are read.
extension LiveServices {
    /// A handle that says connecting until the machines are loaded. `onMachineArrived` then
    /// replaces it with a real terminal; a machine still missing fails as unknown.
    func awaitingMachine(_ hostID: HostID) -> TerminalHandle {
        let (phases, sink) = AsyncStream.makeStream(of: LinkPhase.self)
        let key = UUID()
        if recordsLoaded {
            sink.yield(.failed(LiveServiceError.unknownMachine.localizedDescription))
        } else {
            sink.yield(.connecting)
            pendingTerminals[hostID, default: [:]][key] = sink
        }
        return TerminalHandle(engineView: UIViewController(), phases: phases) { [weak self] in
            self?.pendingTerminals[hostID]?[key] = nil
            sink.finish()
        }
    }

    /// The stored machines are read: waiting terminals attach to theirs, or fail when it is gone.
    func settlePendingTerminals() {
        recordsLoaded = true
        let waiting = pendingTerminals
        pendingTerminals = [:]
        for (hostID, sinks) in waiting {
            if records[hostID] != nil {
                onMachineArrived(hostID)
                sinks.values.forEach { $0.finish() }
            } else {
                for sink in sinks.values {
                    sink.yield(.failed(LiveServiceError.unknownMachine.localizedDescription))
                }
            }
        }
    }
}
