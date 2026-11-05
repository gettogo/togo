import SwiftUI
import TogoKit

/// Opens a session over a transport the user picks for this one time. ET appears only on
/// machines where `etserver` was found, because picking it elsewhere could only fail.
struct ConnectWithMenu: View {
    let etAvailable: Bool
    let connect: (TransportPreference) -> Void

    var body: some View {
        Menu(ControlCenterCopy.connectWith, systemImage: "network") {
            ForEach(Self.transports(etAvailable: etAvailable), id: \.self) { transport in
                Button(ControlCenterCopy.transport(transport)) { connect(transport) }
            }
        }
    }

    static func transports(etAvailable: Bool) -> [TransportPreference] {
        TransportPreference.allCases.filter { $0 != .et || etAvailable }
    }
}
