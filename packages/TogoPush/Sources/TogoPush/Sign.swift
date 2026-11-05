import Foundation

/// The exact bytes a device signs with its Ed25519 key. The helper builds the same strings,
/// so any change here is a protocol change.
public enum Signing {
    /// `togo-approval/v1\n<hostId>\n<requestId>\n<decision>\n<ts>`.
    public static func approvalBytes(hostID: String, requestID: String, decision: String, ts: Int64) -> Data {
        Data("togo-approval/v1\n\(hostID)\n\(requestID)\n\(decision)\n\(ts)".utf8)
    }

    /// `togo-register/v2\n<hostId>\n<deviceId>\n<apns>\n<live>\n<liveStart>\n<command>\n<ts>`; an
    /// absent token is an empty line and the command text choice is `true` or `false`.
    public static func registerBytes(
        hostID: String, deviceID: String, apnsToken: String?, liveActivityToken: String?,
        liveActivityStartToken: String?, liveActivityCommand: Bool, ts: Int64
    ) -> Data {
        let lines = ["togo-register/v2", hostID, deviceID, apnsToken ?? "", liveActivityToken ?? "",
                     liveActivityStartToken ?? "", String(liveActivityCommand), String(ts)]
        return Data(lines.joined(separator: "\n").utf8)
    }
}
