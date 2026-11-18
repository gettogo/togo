import Foundation
import TogoKit
import TogoTerminal
import UIKit

extension NativeCommandContext {
    /// Source link actions act on this device. In particular, sign-in uses
    /// the existing authentication browser and SSH callback forward.
    func localLinkAction(_ node: SurfaceNode) throws -> HarnessActionResult? {
        guard let href = SurfaceValues.string(node.properties["href"]) else { return nil }
        guard let url = URL(string: href), LinkPolicy.allows(url) else {
            throw ChatSurfaceInteraction.Failure(message: PrimitiveCopy.failed)
        }
        switch NativeFormScreen.click(node) {
        case "copy":
            UIPasteboard.general.string = href
            return .init(result: .applied, message: PrimitiveCopy.copied)
        case "open":
            let origin = LinkCenter.Origin(hostID: store.subject.hostID, sessionID: store.subject.sessionID)
            if model.workflow == "login" { LinkCenter.shared.signIn(url, from: origin) }
            else { LinkCenter.shared.open(url, from: origin) }
            // Opening a browser does not mean authentication succeeded.
            return .init(result: .typed)
        default: return nil
        }
    }
}
