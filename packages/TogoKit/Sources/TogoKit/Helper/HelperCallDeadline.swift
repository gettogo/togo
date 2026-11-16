import Foundation

/// Transport deadlines include the helper's own work budget and time to return its reply.
/// Keep liveness checks short; source/history reads and multi-process git work need longer.
enum HelperCallDeadline {
    static func duration(for method: String) -> Duration {
        if method.hasPrefix("git.") { return .seconds(60) }
        if method.hasPrefix("harness.") || method == "fs.upload" { return .seconds(30) }
        return .seconds(10)
    }
}
