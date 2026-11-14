import Foundation

extension HelperClient {
    /// Opens the original runtime interface. Keep this HelperClient until close;
    /// a lease is never valid on a replacement or another device's connection.
    public func surfaceOpen(id: String, options: SurfaceOptions) async throws -> SurfaceLease {
        let result: SurfaceLease = try await readyCall(.surfaceOpen, SurfaceOpenParams(id: id, options: options), needs: Capability.surface)
        guard result.id == id, Self.surfaceHex(result.token, count: 64), Self.surfaceHex(result.generation, count: 32) else {
            throw SurfaceProtocolError.invalidSnapshot
        }
        return result
    }

    public func surfaceRead(
        _ lease: SurfaceLease, revision: UInt64 = 0, snapshot: String? = nil, offset: Int = 0
    ) async throws -> SurfaceReadResult {
        try await readyCall(.surfaceRead,
            SurfaceReadParams(id: lease.id, lease: lease.token, revision: revision, snapshot: snapshot, offset: offset),
            needs: Capability.surface)
    }

    /// Reads a complete immutable revision; nil means pending or unchanged. This
    /// has no ACK side effect. A visible renderer acknowledges its display later.
    public func surfaceSnapshot(_ lease: SurfaceLease, revision: UInt64 = 0, kinds: Set<String>) async throws -> SurfaceScene? {
        var assembler = SurfaceSnapshotAssembler(generation: lease.generation, kinds: kinds)
        var chunk = try await surfaceRead(lease, revision: revision)
        while true {
            try Task.checkCancellation()
            if let scene = try assembler.append(chunk) { return scene }
            if chunk.pending || chunk.unchanged { return nil }
            chunk = try await surfaceRead(lease, snapshot: chunk.snapshot, offset: chunk.nextOffset)
        }
    }

    /// Called only after the view displayed this exact instance and sequence.
    public func surfaceAck(_ lease: SurfaceLease, view: SurfaceView) async throws {
        let _: EmptyResult = try await readyCall(.surfaceAck,
            SurfaceAckParams(id: lease.id, lease: lease.token, surface: view.id, instance: view.instance, sequence: view.document.sequence),
            needs: Capability.surface)
    }

    /// No automatic retries: a disconnected reply has uncertain delivery. The
    /// producer's subsequent document confirms the effect, not `dispatched`.
    public func surfaceInput(
        _ lease: SurfaceLease, surface: String, instance: UInt64, event: JSONValue, expectedText: String? = nil
    ) async throws -> SurfaceDispatchResult {
        guard try JSONEncoder().encode(event).count <= 65_536 else { throw SurfaceProtocolError.invalidSnapshot }
        return try await readyCall(.surfaceInput,
            SurfaceInputParams(id: lease.id, lease: lease.token, surface: surface, instance: instance, event: event, expectedText: expectedText),
            needs: Capability.surface)
    }

    public func surfaceClose(_ lease: SurfaceLease) async throws {
        let _: EmptyResult = try await readyCall(.surfaceClose, SurfaceLeaseParams(id: lease.id, lease: lease.token), needs: Capability.surface)
    }

    private static func surfaceHex(_ value: String, count: Int) -> Bool {
        value.utf8.count == count && value.utf8.allSatisfy { (48...57).contains($0) || (97...102).contains($0) }
    }
}

private struct SurfaceOpenParams: Encodable, Sendable {
    var id: String
    var options: SurfaceOptions
}

private struct SurfaceLeaseParams: Encodable, Sendable {
    var id: String
    var lease: String
}

private struct SurfaceReadParams: Encodable, Sendable {
    var id: String
    var lease: String
    var revision: UInt64
    var snapshot: String?
    var offset: Int
}

private struct SurfaceAckParams: Encodable, Sendable {
    var id: String
    var lease: String
    var surface: String
    var instance: UInt64
    var sequence: Int64
}

private struct SurfaceInputParams: Encodable, Sendable {
    var id: String
    var lease: String
    var surface: String
    var instance: UInt64
    var event: JSONValue
    var expectedText: String?
}
