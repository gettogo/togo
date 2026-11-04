import Foundation
@preconcurrency import NIOSSH

/// The `authorized_keys` line for a key and the fingerprint `ssh-keygen -l` prints for it.
enum PublicKeyLine {
    static func line(for key: NIOSSHPublicKey, comment: String) -> String {
        let base = String(openSSHPublicKey: key).trimmingCharacters(in: .whitespaces)
        return comment.isEmpty ? base : base + " " + comment
    }

    static func fingerprint(for key: NIOSSHPublicKey) -> String {
        HostKeyTrust.fingerprint(blob: blob(of: key) ?? Data())
    }

    static func blob(of key: NIOSSHPublicKey) -> Data? {
        let parts = String(openSSHPublicKey: key).split(separator: " ")
        guard parts.count >= 2 else { return nil }
        return Data(base64Encoded: String(parts[1]))
    }

    static func keyType(of key: NIOSSHPublicKey) -> String {
        String(String(openSSHPublicKey: key).split(separator: " ").first ?? "")
    }

    static var deviceComment: String {
        let host = ProcessInfo.processInfo.hostName.split(separator: ".").first.map(String.init) ?? "device"
        return "togo@\(host)"
    }
}
