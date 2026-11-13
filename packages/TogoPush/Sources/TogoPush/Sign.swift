import Foundation

/// The exact bytes a device signs with its Ed25519 key. The helper builds the same strings,
/// so any change here is a protocol change.
public enum Signing {
    public static func liveContextBytes(hostID: String, deviceID: String, requestID: String, tokenHash: String, ts: Int64) -> Data {
        Data("togo-live-context/v1\n\(hostID)\n\(deviceID)\n\(requestID)\n\(tokenHash)\n\(ts)".utf8)
    }

    /// `togo-approval/v1\n<hostId>\n<requestId>\n<decision>\n<ts>`.
    public static func approvalBytes(hostID: String, requestID: String, decision: String, ts: Int64) -> Data {
        Data("togo-approval/v1\n\(hostID)\n\(requestID)\n\(decision)\n\(ts)".utf8)
    }

    /// `togo-register/v2\n<hostId>\n<deviceId>\n<apns>\n<live>\n<liveStart>\n<command>\n<ts>`; an
    /// absent token is an empty line and the command text choice is `true` or `false`. With
    /// `liveActivityEnded` it is v3 instead, the ended token on its own line after `<liveStart>`;
    /// without one it stays v2, which helpers that predate v3 accept.
    public static func registerBytes(
        hostID: String, deviceID: String, apnsToken: String?, liveActivityToken: String?,
        liveActivityStartToken: String?, liveActivityEnded: String? = nil, liveActivityCommand: Bool, ts: Int64
    ) -> Data {
        var lines = ["togo-register/v2", hostID, deviceID, apnsToken ?? "", liveActivityToken ?? "",
                     liveActivityStartToken ?? ""]
        if let liveActivityEnded {
            lines[0] = "togo-register/v3"
            lines.append(liveActivityEnded)
        }
        lines += [String(liveActivityCommand), String(ts)]
        return Data(lines.joined(separator: "\n").utf8)
    }
}
