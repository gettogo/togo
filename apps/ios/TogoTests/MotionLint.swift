import Foundation
import Testing

/// The checks `scripts/check-motion.sh` makes, so the test target runs them on the simulator,
/// which cannot start a shell. Both read the fixtures in `scripts/motion-lint/`; change the
/// rules in both places together.
enum MotionLint {
    private static let repo = URL(filePath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent()
        .deletingLastPathComponent().deletingLastPathComponent()

    /// A file of the repo, whole.
    static func source(_ path: String) throws -> String {
        try String(contentsOf: repo.appending(path: path), encoding: .utf8)
    }

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

    // MARK: - Motion principles v2 (§6)

    /// One v2 offence: the 1-based line and the rule it breaks.
    struct Offence: Equatable, CustomStringConvertible {
        var line: Int
        var rule: String
        var description: String { "\(line): \(rule)" }
    }

    /// The v2 rules of `scripts/check-motion.sh` over one file's source: no-rise,
    /// no-roll-in-timeline, periodic-now, motion-purpose, availability-animates. `path` decides
    /// the PromptBar and Tutorial/ exemptions from no-rise.
    static func v2Offences(in source: String, path: String) -> [Offence] {
        let lines = source.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        let exempt = path.contains("PromptBar") || path.hasPrefix("Tutorial/") || path.contains("/Tutorial/")
        var found: [Offence] = []
        func report(_ index: Int, _ rule: String) {
            let offence = Offence(line: index + 1, rule: rule)
            if !found.contains(offence) { found.append(offence) }
        }
        func isCode(_ line: String) -> Bool {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            return !trimmed.isEmpty && !trimmed.hasPrefix("//") && !trimmed.hasPrefix("#")
        }
        func braces(_ line: String) -> Int {
            line.reduce(0) { $0 + ($1 == "{" ? 1 : $1 == "}" ? -1 : 0) }
        }
        let rolls = /numericText|rollingNumber|countChange/
        var inTransition = false
        var depth = 0
        for (index, line) in lines.enumerated() where isCode(line) {
            if line.firstMatch(of: /:\s*([A-Za-z]+\s*&\s*)?Transition\s*\{/) != nil {
                inTransition = true
                depth = 0
            }
            if !exempt {
                if line.firstMatch(of: /revealRise|swapShift|revealOnAppear/) != nil {
                    report(index, "no-rise")
                } else if line.contains(".offset(y:"),
                          inTransition || line.firstMatch(of: /[Tt]ransition|asymmetric|combined\(with:/) != nil {
                    report(index, "no-rise")
                }
            }
            if inTransition {
                depth += braces(line)
                if depth <= 0 { inTransition = false }
            }
            if line.contains("TimelineView(") {
                if line.firstMatch(of: rolls) != nil { report(index, "no-roll-in-timeline") }
                var balance = braces(line)
                var next = index
                while balance > 0, next + 1 < lines.count, next < index + 150 {
                    next += 1
                    if isCode(lines[next]), lines[next].firstMatch(of: rolls) != nil { report(next, "no-roll-in-timeline") }
                    balance += braces(lines[next])
                }
            }
            if line.firstMatch(of: /\.periodic\(from:\s*(\.now|Date\(\))/) != nil { report(index, "periodic-now") }
            if line.firstMatch(of: /\.transition\(|withAnimation\(|\.motion\(|matchedGeometryEffect\(/) != nil,
               line.firstMatch(of: /func (motion|transition)\(/) == nil {
                let purposed = lines[max(0, index - 3)...index].contains { $0.contains("// motion:") }
                if !purposed { report(index, "motion-purpose") }
            }
            if line.firstMatch(of: /\.motion\(|\.animation\(/) != nil,
               line.firstMatch(of: /value:[^)]*(shownLayer|chatAvailable|hasMission)/) != nil {
                report(index, "availability-animates")
            }
        }
        return found
    }

    /// A v2 fixture whole, with its comments, and the rules its `// expect <rule>` lines name.
    static func v2Fixture(_ name: String) throws -> (source: String, expected: [String]) {
        let source = try self.source("scripts/motion-lint/\(name)")
        let expected = source.split(separator: "\n").compactMap { line -> String? in
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            guard trimmed.hasPrefix("// expect ") else { return nil }
            return trimmed.dropFirst("// expect ".count).split(separator: " ").first.map(String.init)
        }
        return (source, expected)
    }
}
