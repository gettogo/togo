import Foundation

/// Joins exactly one immutable snapshot, with bounded storage and no action or ACK.
public struct SurfaceSnapshotAssembler: Sendable {
    public static let maxBytes = 8 * 1024 * 1024 + 65_536
    private let generation: String
    private let kinds: Set<String>
    private var snapshot: String?
    private var revision: UInt64 = 0
    private var total = 0
    private var buffer = Data()

    public init(generation: String, kinds: Set<String>) {
        self.generation = generation
        self.kinds = kinds
    }

    public mutating func append(_ chunk: SurfaceReadResult) throws -> SurfaceScene? {
        guard chunk.generation == generation, chunk.offset >= 0,
              chunk.total >= 0, chunk.total <= Self.maxBytes,
              chunk.data.count <= 65_536 else { throw SurfaceProtocolError.invalidSnapshot }
        if chunk.pending || chunk.unchanged {
            guard snapshot == nil, chunk.data.isEmpty, chunk.total == 0 else { throw SurfaceProtocolError.invalidSnapshot }
            return nil
        }
        guard let id = chunk.snapshot, id.utf8.count == 64,
              id.utf8.allSatisfy({ (48...57).contains($0) || (97...102).contains($0) }),
              !chunk.data.isEmpty, chunk.nextOffset <= chunk.total,
              chunk.nextOffset == chunk.offset + chunk.data.count else { throw SurfaceProtocolError.invalidSnapshot }
        if snapshot == nil {
            guard chunk.offset == 0 else { throw SurfaceProtocolError.invalidSnapshot }
            snapshot = id
            revision = chunk.revision
            total = chunk.total
            buffer.reserveCapacity(total)
        }
        guard id == snapshot, revision == chunk.revision, total == chunk.total,
              chunk.offset == buffer.count else { throw SurfaceProtocolError.invalidSnapshot }
        buffer.append(chunk.data)
        guard buffer.count == total else { return nil }
        let scene = try SurfaceScene.decode(buffer, kinds: kinds)
        guard scene.revision == revision else { throw SurfaceProtocolError.invalidSnapshot }
        return scene
    }
}
