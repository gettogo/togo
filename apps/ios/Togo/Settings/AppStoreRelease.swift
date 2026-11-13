import Foundation
import StoreKit

/// App Store marketing versions, compared by number rather than by spelling.
struct AppReleaseVersion: Comparable, Equatable {
    private let parts: [Int]

    init?(_ text: String) {
        let words = text.split(separator: ".", omittingEmptySubsequences: false)
        guard text.utf8.count <= 32, (1...3).contains(words.count) else { return nil }
        var values: [Int] = []
        for word in words {
            guard !word.isEmpty, word.utf8.allSatisfy({ (48...57).contains($0) }), let value = Int(word) else { return nil }
            values.append(value)
        }
        parts = values + Array(repeating: 0, count: 3 - values.count)
    }

    static func < (lhs: Self, rhs: Self) -> Bool { lhs.parts.lexicographicallyPrecedes(rhs.parts) }
}

struct AppStoreRelease: Codable, Equatable {
    let version: String
    let minimumOsVersion: String

    func isAvailable(installed: String, operatingSystem: String) -> Bool {
        guard let current = AppReleaseVersion(installed), let latest = AppReleaseVersion(version),
              let required = AppReleaseVersion(minimumOsVersion), let running = AppReleaseVersion(operatingSystem) else { return false }
        return latest > current && running >= required
    }
}

/// Public, published releases only. No App Store Connect or TestFlight credentials.
enum AppStoreLookup {
    static let appID = 6_833_273_877
    static let bundleID = "app.togo.Togo"
    static let storeURL = URL(string: "https://apps.apple.com/app/togo-agent-remote/id6833273877")!
    static let timeout: TimeInterval = 15
    static let responseLimit = 256 * 1024

    enum Failure: Error { case storefront, response, listing }

    /// StoreKit uses alpha-3; lookup requires alpha-2. ICU normalizes the territory code.
    static func countryCode(_ storefrontCode: String) -> String? {
        guard storefrontCode.utf8.count == 3, storefrontCode.utf8.allSatisfy({ (65...90).contains($0) }) else { return nil }
        guard let code = Locale(identifier: "und_" + storefrontCode).region?.identifier,
              Locale.Region.isoRegions.contains(where: { $0.identifier == code }), code.utf8.count == 2 else { return nil }
        return code
    }

    static func storefrontCountry() async throws -> String {
        guard let storefront = await Storefront.current, let country = countryCode(storefront.countryCode) else {
            throw Failure.storefront
        }
        return country
    }

    static func request(country: String) -> URLRequest {
        var url = URLComponents(string: "https://itunes.apple.com/lookup")!
        url.queryItems = [URLQueryItem(name: "id", value: String(appID)), URLQueryItem(name: "country", value: country),
                          URLQueryItem(name: "entity", value: "software")]
        return URLRequest(url: url.url!, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: timeout)
    }

    static func release(data: Data, status: Int) throws -> AppStoreRelease? {
        guard status == 200, data.count <= responseLimit else { throw Failure.response }
        struct Item: Decodable {
            let trackId: Int
            let bundleId: String
            let version: String
            let minimumOsVersion: String
        }
        struct Result: Decodable { let results: [Item] }
        let result = try JSONDecoder().decode(Result.self, from: data)
        guard !result.results.isEmpty else { return nil }
        guard result.results.count == 1, let item = result.results.first,
              item.trackId == appID, item.bundleId == bundleID,
              AppReleaseVersion(item.version) != nil, AppReleaseVersion(item.minimumOsVersion) != nil else { throw Failure.listing }
        return AppStoreRelease(version: item.version, minimumOsVersion: item.minimumOsVersion)
    }

    static func fetch(country: String) async throws -> AppStoreRelease? {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForResource = timeout
        let session = URLSession(configuration: config)
        defer { session.finishTasksAndInvalidate() }
        let (data, response) = try await session.data(for: request(country: country))
        return try release(data: data, status: (response as? HTTPURLResponse)?.statusCode ?? 0)
    }
}
