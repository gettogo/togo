import Foundation

/// The address of a prefilled bug report on the project's GitHub page. It carries the app
/// version, the iOS version, the device family and helper versions, and nothing else.
enum IssueLink {
    static let repository = "https://github.com/gettogo/togo"
    static let maxBody = 6000
    static let maxField = 32
    static let maxHelpers = 8

    /// The facts go in the issue form's `versions` field; the form ignores a free `body`.
    static func url(app: String, os: String, device: String, helpers: [String]) -> URL {
        let versions = cleanHelpers(helpers)
        let parts = [
            "App \(cleanApp(app) ?? "unknown")",
            "iOS \(cleanOS(os) ?? "unknown")",
            deviceFamily(device),
            "helper \(versions.isEmpty ? "none" : versions.joined(separator: ", "))",
        ]
        let text = String(parts.joined(separator: ", ").prefix(maxBody))
        let address = "\(repository)/issues/new?template=bug.yml&title=&versions=\(encode(text))"
        return URL(string: address) ?? URL(string: repository)!
    }

    /// The family a device reports, so the model never leaves the phone.
    static func family(isPad: Bool) -> String { isPad ? "iPad" : "iPhone" }

    /// Version numbers only: digits and dots, an optional "v", and at most a short tag such as
    /// "-rc.1". Each stops at the first character that is not a letter, digit, dot, dash, or
    /// (for the app) space or bracket, and is at most 32 long.
    static func cleanHelpers(_ helpers: [String]) -> [String] {
        Array(helpers.compactMap { clean($0, allowing: Self.versionCharacters, matching: Self.helperShape) }.prefix(maxHelpers))
    }

    private static let helperShape = "^v?[0-9]+(\\.[0-9]+){0,3}(-(rc|beta|alpha|dev|pre)(\\.?[0-9]+)?)?$"
    private static let appShape = "^[0-9]+(\\.[0-9]+){0,3}( \\([0-9]+\\))?$"
    private static let osShape = "^[0-9]+(\\.[0-9]+){0,3}$"

    private static func cleanApp(_ value: String) -> String? { clean(value, allowing: appCharacters, matching: appShape) }
    private static func cleanOS(_ value: String) -> String? { clean(value, allowing: appCharacters, matching: osShape) }

    private static let versionCharacters = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-")
    private static let appCharacters = versionCharacters.union(CharacterSet(charactersIn: " ()"))

    private static func clean(
        _ value: String, allowing allowed: CharacterSet, matching shape: String
    ) -> String? {
        var kept = ""
        for character in value.unicodeScalars {
            guard character.isASCII, allowed.contains(character) else { break }
            kept.unicodeScalars.append(character)
            if kept.count == maxField { break }
        }
        let trimmed = kept.trimmingCharacters(in: .whitespaces)
        return trimmed.range(of: shape, options: .regularExpression) == nil ? nil : trimmed
    }

    private static func deviceFamily(_ device: String) -> String {
        if device.hasPrefix("iPad") { return "iPad" }
        if device.hasPrefix("iPhone") { return "iPhone" }
        return "Unknown"
    }

    private static let unreserved = CharacterSet(
        charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
    )

    private static func encode(_ text: String) -> String {
        text.addingPercentEncoding(withAllowedCharacters: unreserved) ?? ""
    }
}
