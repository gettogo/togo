import Foundation

extension HelperError {
    /// The helper's structured conflict reason, in either client representation.
    /// Inspecting it leaves the original RPC object and its details intact.
    public var conflictReason: String? {
        switch self {
        case .conflict(let reason): reason
        case .rpc(let object) where object.code == RPCErrorCode.conflict: object.reason
        default: nil
        }
    }

    public var refusalReason: String? {
        switch self {
        case .refused(let reason): reason
        case .rpc(let object) where object.code == RPCErrorCode.refused: object.reason
        default: nil
        }
    }

    /// Native interaction is unavailable; the terminal is the supported route.
    /// Other refusals apply only to the operation that was attempted.
    public var isFallback: Bool {
        refusalReason == "fallback" || refusalReason == "no_adapter"
    }
}
