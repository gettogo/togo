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

    /// The comment on a device key's `authorized_keys` line: `togo-iPad-1a2b3c4d`. Two devices of
    /// one person read differently on the machine, so removing one line never takes the other.
    static func deviceComment(for key: NIOSSHPublicKey) -> String {
        deviceComment(model: deviceModel(), fingerprint: fingerprint(for: key))
    }

    static func deviceComment(model: String, fingerprint: String) -> String {
        let digest = fingerprint.drop { $0 != ":" }.dropFirst()
        let source = digest.isEmpty ? Substring(fingerprint) : digest
        let short = String(source.filter { $0.isLetter || $0.isNumber }.prefix(8))
        return short.isEmpty ? "togo-\(model)" : "togo-\(model)-\(short)"
    }

    /// "iPad", "iPhone" or "device", from the hardware identifier (`iPad16,3`).
    static func deviceModel() -> String {
        var identifier = ProcessInfo.processInfo.environment["SIMULATOR_MODEL_IDENTIFIER"] ?? ""
        if identifier.isEmpty {
            var size = 0
            sysctlbyname("hw.machine", nil, &size, nil, 0)
            var buffer = [CChar](repeating: 0, count: max(size, 1))
            sysctlbyname("hw.machine", &buffer, &size, nil, 0)
            identifier = String(decoding: buffer.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }, as: UTF8.self)
        }
        return model(fromIdentifier: identifier)
    }

    static func model(fromIdentifier identifier: String) -> String {
        for name in ["iPad", "iPhone"] where identifier.hasPrefix(name) { return name }
        return "device"
    }
}
