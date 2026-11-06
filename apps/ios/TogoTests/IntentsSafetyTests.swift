import Foundation
import Testing
@testable import Togo

/// Shortcuts, Siri and Spotlight can open the app and read a count. Nothing they run may
/// start a session, type into one or settle a request.
struct IntentsSafetyTests {
    private static let folder = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .appending(path: "Togo/Intents")

    private static func sources() throws -> [(name: String, text: String)] {
        try FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: nil)
            .filter { $0.pathExtension == "swift" }
            .map { ($0.lastPathComponent, try String(contentsOf: $0, encoding: .utf8)) }
    }

    @Test func noIntentAnswersRequests() throws {
        let files = try Self.sources()
        #expect(files.count >= 7)
        let banned = [
            // Requests and their answers.
            "answer", "approve", "deny", "decision", "claim(", "release(", ".mark(", ".remove(", "respond(",
            "submit(", "sendreply", "notificationaction",
            // Starting, ending or typing into a session.
            ".create(", "createsession", "relaunch", "sendtext", "send(text", "sendkeys", "launch(", "kill(",
            "exec(", "process(", "terminal", "compose", "attach",
            // Reaching a machine, a window's store or the new-session flow directly.
            "services", "machinetools", "helperclient", "helperchannels", "appstore", "store.", "newsessionmodel",
        ]
        for (name, text) in files {
            let lower = text.lowercased()
            for word in banned {
                #expect(!lower.contains(word), "\(name) mentions \(word)")
            }
        }
    }

    /// Each `perform()` may only hand the hub a link, read the hub's local data and return.
    @Test func performOnlyOpensLinksOrReads() throws {
        let allowed: Set<String> = ["open", "result", "link", "count", "requests", "clock", "message", "IntentDialog"]
        var bodies = 0
        for (name, text) in try Self.sources() {
            for body in Self.performBodies(text) {
                bodies += 1
                let calls = body.matches(of: /([A-Za-z_][A-Za-z0-9_]*)\s*\(/).map { String($0.output.1) }
                for call in calls {
                    #expect(allowed.contains(call), "\(name) perform calls \(call)")
                }
            }
        }
        #expect(bodies == 3)
    }

    /// A new kind of link has to be looked at here before an intent can send it.
    @Test func intentsSendOnlyKnownLinks() {
        let links: [DeepLink] = [.session(bib: "04", host: "Studio"), .newSession(host: "Studio")]
        for link in links {
            switch link {
            case .session, .newSession: break
            }
        }
    }

    private static func performBodies(_ text: String) -> [String] {
        var bodies: [String] = []
        var rest = text[...]
        while let start = rest.range(of: "func perform(") {
            guard let open = rest[start.upperBound...].firstIndex(of: "{") else { break }
            var depth = 0
            var index = open
            while index < rest.endIndex {
                if rest[index] == "{" { depth += 1 }
                if rest[index] == "}" {
                    depth -= 1
                    if depth == 0 { break }
                }
                index = rest.index(after: index)
            }
            bodies.append(String(rest[rest.index(after: open)..<min(index, rest.endIndex)]))
            rest = rest[min(index, rest.endIndex)...]
        }
        return bodies
    }
}
