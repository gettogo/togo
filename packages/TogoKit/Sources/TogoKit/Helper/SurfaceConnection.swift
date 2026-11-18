import Foundation

/// One connection owns a native renderer from open through release. Kept apart
/// from request-scoped helper routing so a reconnect cannot transfer its lease.
public protocol SurfaceConnection: Sendable {
    func surfaceOpen(id: String, options: SurfaceOptions) async throws -> SurfaceLease
    func surfaceSnapshot(_ lease: SurfaceLease, revision: UInt64, kinds: Set<String>) async throws -> SurfaceScene?
    func surfaceInput(_ lease: SurfaceLease, surface: String, instance: UInt64, event: JSONValue, expectedText: String?) async throws -> SurfaceDispatchResult
    func surfaceAck(_ lease: SurfaceLease, view: SurfaceView) async throws
    func surfaceClose(_ lease: SurfaceLease) async throws
    func close() async
}

extension HelperClient: SurfaceConnection {}
