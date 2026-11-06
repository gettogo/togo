import CryptoKit
import Foundation

/// The preset last started in one folder on one machine.
public struct FolderLaunch: Codable, Hashable, Sendable {
    public var preset: UUID

    public init(preset: UUID) {
        self.preset = preset
    }
}

/// Which preset to offer first for a folder, per machine. Records sync like presets.
///
/// A record is keyed by `<hostID>|<cwd>`. iCloud key-value keys are capped at 64 bytes and a
/// folder path is often longer, so the stored key is a digest of that string.
public final class FolderLaunchMemory: Sendable {
    public static let storePrefix = "folder."

    private let store: SyncedRecordStore<FolderLaunch>

    public init(store: SyncedRecordStore<FolderLaunch>) {
        self.store = store
    }

    public func preset(host: UUID, cwd: String) async -> UUID? {
        await store.all()[Self.recordID(host: host, cwd: cwd)]?.preset
    }

    /// Remembers `id` for the folder, or forgets the folder's preset when `id` is nil.
    public func remember(_ id: UUID?, host: UUID, cwd: String) async {
        let record = Self.recordID(host: host, cwd: cwd)
        if let id {
            await store.put(record, FolderLaunch(preset: id))
        } else {
            await store.delete(record)
        }
    }

    /// Forgets every folder that remembers `preset`, so a deleted preset leaves no records.
    public func forget(preset: UUID) async {
        for (record, launch) in await store.all() where launch.preset == preset {
            await store.delete(record)
        }
    }

    public func setSyncEnabled(_ on: Bool) async {
        await store.setSyncEnabled(on)
    }

    /// The logical key: the machine's id and the folder without a trailing slash.
    public static func key(host: UUID, cwd: String) -> String {
        var folder = cwd
        while folder.count > 1, folder.hasSuffix("/") { folder.removeLast() }
        return "\(host.uuidString)|\(folder)"
    }

    static func recordID(host: UUID, cwd: String) -> String {
        let digest = SHA256.hash(data: Data(key(host: host, cwd: cwd).utf8))
        return Data(digest).base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}
