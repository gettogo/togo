import Foundation
import Testing

/// The checks `scripts/check-motion.sh` makes, so the test target runs them on the simulator,
/// which cannot start a shell. Both read the fixtures in `scripts/motion-lint/`; change the
/// rules in both places together.
enum MotionLint {
    private static let repo = URL(filePath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent()
        .deletingLastPathComponent().deletingLastPathComponent()

    static func allowListEntries() throws -> [String] {
        try lines(of: "scripts/motion-allowlist.txt")
    }

    /// The source lines of a fixture, without comments and blank lines.
    static func fixture(_ name: String) throws -> [String] {
        try lines(of: "scripts/motion-lint/\(name)")
    }

    static func violations() throws -> [String] {
        let root = repo.appending(path: "apps/ios/Togo")
        let files = try #require(FileManager.default.enumerator(at: root, includingPropertiesForKeys: nil))
        var found: [String] = []
        for case let url as URL in files where url.pathExtension == "swift" {
            let path = String(url.path.dropFirst(root.path.count + 1))
            if path.hasPrefix("Design/Motion") { continue }
            let lines = try String(contentsOf: url, encoding: .utf8).split(separator: "\n", omittingEmptySubsequences: false)
            for (index, line) in lines.enumerated() where flags(String(line)) {
                found.append("\(path):\(index + 1): \(line.trimmingCharacters(in: .whitespaces))")
            }
        }
        return found.sorted()
    }

    private static let banned = [
        "withAnimation", ".animation(", "Transaction(animation:", "UIView.animate", "UIView.transition(",
        "PropertyAnimator", "phaseAnimator", "keyframeAnimator", "PhaseAnimator", "KeyframeAnimator",
        "CABasicAnimation", "CASpringAnimation", "CAKeyframeAnimation", "CATransition", "CAAnimationGroup",
        ".spring(", ".easeIn", ".easeOut", ".linear(", ".bouncy", ".smooth", ".snappy", ".interactiveSpring",
        "UnitCurve.", "Spring(", ".speed(", "repeatForever", ".repeatCount(",
    ]

    static func flags(_ source: String) -> Bool {
        let line = source.trimmingCharacters(in: .whitespaces)
        if line.isEmpty || line.hasPrefix("//") || line.hasPrefix("#") { return false }
        // A fallback after a role call brings back a curve, or an animation where the role has none.
        if line.contains("Motion.animation("), line.contains("??") { return true }
        // Assignments take nil, a role, or nil-or-role.
        if let assignment = line.firstMatch(of: /\.animation[ \t]*=(?!=)/) {
            let rhs = line[assignment.range.upperBound...].trimmingCharacters(in: .whitespaces)
            let sanctioned = rhs.hasPrefix("nil") || rhs.hasPrefix("Motion.animation(")
                || rhs.firstMatch(of: /^[A-Za-z_.]+ \? nil : Motion\.animation\(/) != nil
            if !sanctioned { return true }
        }
        var probe = line
        for call in [
            "withAnimation(Motion.animation(", ".animation(Motion.animation(", "Motion.animation(",
            "Transaction(animation: nil)", ".animation(nil",
        ] {
            probe = probe.replacingOccurrences(of: call, with: "")
        }
        return banned.contains { probe.contains($0) }
    }

    private static func lines(of path: String) throws -> [String] {
        try String(contentsOf: repo.appending(path: path), encoding: .utf8)
            .split(separator: "\n").map(String.init)
            .filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty && !$0.hasPrefix("#") }
    }
}
