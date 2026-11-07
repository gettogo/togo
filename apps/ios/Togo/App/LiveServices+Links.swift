import Foundation
import TogoKit

/// Pages agents ask their machine to open, put to the user on this device.
extension LiveServices {
    func askToOpen(from snapshot: HostSnapshot) {
        for request in snapshot.browserOpens {
            let agentID = request.sessionID.flatMap { id in snapshot.sessions.first { $0.id == id }?.agent }
            let agent = agentID.map { mapper.agentNames[snapshot.hostID]?[$0] ?? $0 }
            LinkCenter.shared.agentAsked(request, hostID: snapshot.hostID, agent: agent)
        }
    }
}
