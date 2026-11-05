import CryptoKit
import Foundation

public enum HostID {
    /// `"h_"` + the first 26 characters of lowercase base32 (no padding) of SHA-256(secret).
    /// The relay recomputes it from the mailbox secret, so the id proves knowledge of the
    /// secret without revealing it.
    public static func derive(secret: Data) -> String {
        "h_" + Base32.lowercase(Data(SHA256.hash(data: secret))).prefix(26)
    }
}

enum Base32 {
    private static let alphabet = Array("abcdefghijklmnopqrstuvwxyz234567")

    /// RFC 4648 base32 in lowercase, without padding.
    static func lowercase(_ data: Data) -> String {
        var out = ""
        var buffer: UInt32 = 0
        var bits = 0
        for byte in data {
            buffer = buffer << 8 | UInt32(byte)
            bits += 8
            while bits >= 5 {
                bits -= 5
                out.append(alphabet[Int(buffer >> UInt32(bits) & 31)])
            }
        }
        if bits > 0 {
            out.append(alphabet[Int(buffer << UInt32(5 - bits) & 31)])
        }
        return out
    }
}
