import CryptoKit
import Foundation

public enum HostKeyTrust {
    /// The known_hosts spelling, so one key store entry covers every profile for a host.
    public static func hostID(_ endpoint: HostEndpoint) -> String {
        "[\(endpoint.host.lowercased())]:\(endpoint.port)"
    }

    /// Matches `ssh-keygen -l -E sha256`: SHA-256 of the wire-format key blob, base64 without padding.
    public static func fingerprint(blob: Data) -> String {
        let digest = Data(SHA256.hash(data: blob)).base64EncodedString()
        return "SHA256:" + digest.replacingOccurrences(of: "=", with: "")
    }
}

public protocol HostKeyStore: Sendable {
    /// Throws when the trusted keys cannot be read: an unreadable store must block the
    /// connection, never look like "no keys yet" and turn a changed key into a trust prompt.
    func fingerprints(for hostID: String) async throws -> [String]
    func trust(_ fingerprint: String, for hostID: String) async throws
    func forget(_ hostID: String) async throws
}

public enum HostKeyStoreError: Error, Equatable, LocalizedError {
    /// The trusted host key file exists but cannot be read or parsed.
    case unreadable(String)

    public var errorDescription: String? {
        String(localized: "The list of trusted machine keys could not be read, so Togo will not connect until it is fixed.", bundle: .module)
    }
}

/// Trusted host keys as a JSON map of host id to fingerprints.
///
/// Every write replaces the file through a rename, so a crash leaves either the old map
/// or the new one, never a torn file. The actor serialises read-modify-write cycles.
public actor FileHostKeyStore: HostKeyStore {
    private let url: URL
    private var cache: [String: [String]]?

    public init(url: URL) {
        self.url = url
    }

    public func fingerprints(for hostID: String) async throws -> [String] {
        try load()[hostID] ?? []
    }

    public func trust(_ fingerprint: String, for hostID: String) async throws {
        var map = try load()
        var list = map[hostID] ?? []
        guard !list.contains(fingerprint) else { return }
        list.append(fingerprint)
        map[hostID] = list
        try save(map)
    }

    public func forget(_ hostID: String) async throws {
        var map = try load()
        guard map.removeValue(forKey: hostID) != nil else { return }
        try save(map)
    }

    /// Only a missing file means "no trusted hosts". Anything else throws, after keeping a
    /// `.corrupt` copy, so no later write can replace the file with a smaller map.
    private func load() throws -> [String: [String]] {
        if let cache { return cache }
        let data: Data
        do {
            data = try Data(contentsOf: url)
        } catch CocoaError.fileReadNoSuchFile {
            cache = [:]
            return [:]
        } catch {
            throw HostKeyStoreError.unreadable(url.path)
        }
        do {
            let map = try JSONDecoder().decode([String: [String]].self, from: data)
            cache = map
            return map
        } catch {
            let copy = url.appendingPathExtension("corrupt")
            if !FileManager.default.fileExists(atPath: copy.path) {
                try? FileManager.default.copyItem(at: url, to: copy)
            }
            throw HostKeyStoreError.unreadable(url.path)
        }
    }

    private func save(_ map: [String: [String]]) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .prettyPrinted]
        let data = try encoder.encode(map)
        let directory = url.deletingLastPathComponent()
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        // Created 0600 from the start: writing then chmod-ing would leave a readable window.
        let temp = directory.appendingPathComponent(".\(url.lastPathComponent).\(UUID().uuidString)")
        guard FileManager.default.createFile(atPath: temp.path, contents: data, attributes: [.posixPermissions: 0o600]) else {
            throw CocoaError(.fileWriteUnknown, userInfo: [NSFilePathErrorKey: temp.path])
        }
        guard rename(temp.path, url.path) == 0 else {
            throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
        }
        cache = map
    }
}
