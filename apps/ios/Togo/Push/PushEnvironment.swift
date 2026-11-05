import Foundation
import TogoKit

/// Where this build's pushes come from and which relay carries them.
enum PushEnvironment {
    /// Debug builds are signed for the APNs sandbox, release builds for production.
    static var apns: PushPairParams.Environment {
        #if DEBUG
        .sandbox
        #else
        .production
        #endif
    }

    /// The hosted relay, used unless the user set their own. It comes from the build setting
    /// `TOGO_RELAY_HOST` (see Config/Relay.example.xcconfig), so a build without one has none.
    static let defaultRelay = hostedRelay(Bundle.main.object(forInfoDictionaryKey: "TogoRelayHost") as? String)

    /// The https address for a configured relay host, or nil when the build set none.
    static func hostedRelay(_ host: String?) -> URL? {
        guard let host = host?.trimmingCharacters(in: .whitespaces), !host.isEmpty, !host.contains("$(") else {
            return nil
        }
        return customRelay("https://" + host)
    }

    /// The relay to pair with: the user's own when it is a valid https address, else the hosted
    /// one, or nil when there is neither.
    static func relay(_ settings: AppSettings, hosted: URL? = defaultRelay) -> URL? {
        settings.relayURL.flatMap { customRelay($0.absoluteString) } ?? hosted
    }

    /// A relay address the user typed, or nil unless it is https with a host and nothing after
    /// the path. Pushes carry sealed payloads, but the relay still sees which machines and
    /// devices talk, so plain http is never accepted.
    static func customRelay(_ text: String) -> URL? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let parts = URLComponents(string: trimmed),
              parts.scheme?.lowercased() == "https",
              let host = parts.host, !host.isEmpty,
              parts.user == nil, parts.password == nil, parts.query == nil, parts.fragment == nil,
              let url = parts.url
        else { return nil }
        return url
    }
}
