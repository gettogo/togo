import Foundation
import Observation
import OSLog
import SwiftUI

/// A device's installed app and dismissal are independent of its other devices and machines.
@MainActor @Observable final class AppUpdates {
    static let checkInterval: TimeInterval = 24 * 60 * 60
    static let retryInterval: TimeInterval = 15 * 60
    private static let key = "appUpdates.v1"
    private static let log = Logger(subsystem: "app.togo", category: "app-updates")

    private struct Saved: Codable {
        var country: String?
        var release: AppStoreRelease?
        var checkedAt: Date?
        var dismissed: String?
    }

    private var saved: Saved
    private var verifiedCountry: String?
    private var checking = false
    private var failedAt: Date?
    private let installed: String
    private let operatingSystem: String
    private let defaults: UserDefaults
    private let country: (() async throws -> String)?
    private let lookup: ((String) async throws -> AppStoreRelease?)?
    private let now: () -> Date

    init(installed: String = "", operatingSystem: String = "", defaults: UserDefaults = .standard,
         now: @escaping () -> Date = Date.init, country: (() async throws -> String)? = nil,
         lookup: ((String) async throws -> AppStoreRelease?)? = nil) {
        self.installed = installed
        self.operatingSystem = operatingSystem
        self.defaults = defaults
        self.now = now
        self.country = country
        self.lookup = lookup
        saved = defaults.data(forKey: Self.key).flatMap { try? JSONDecoder().decode(Saved.self, from: $0) } ?? Saved()
    }

    static func live() -> AppUpdates {
        let os = ProcessInfo.processInfo.operatingSystemVersion
        return AppUpdates(
            installed: Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "",
            operatingSystem: "\(os.majorVersion).\(os.minorVersion).\(os.patchVersion)",
            country: { try await AppStoreLookup.storefrontCountry() }, lookup: { try await AppStoreLookup.fetch(country: $0) }
        )
    }

    var availableVersion: String? {
        guard saved.country == verifiedCountry, let release = saved.release,
              release.isAvailable(installed: installed, operatingSystem: operatingSystem) else { return nil }
        if let dismissed = saved.dismissed.flatMap(AppReleaseVersion.init),
           let latest = AppReleaseVersion(release.version), latest <= dismissed { return nil }
        return release.version
    }

    func dismiss() {
        guard let version = availableVersion else { return }
        saved.dismissed = version
        save()
    }

    func checkIfDue() async {
        guard let country, let lookup, !checking, Self.due(failedAt, interval: Self.retryInterval, at: now()) else { return }
        checking = true
        defer { checking = false }
        do {
            let currentCountry = try await country()
            try Task.checkCancellation()
            verifiedCountry = currentCountry
            if saved.country != currentCountry {
                saved.country = currentCountry
                saved.release = nil
                saved.checkedAt = nil
                save()
            }
            guard Self.due(saved.checkedAt, interval: Self.checkInterval, at: now()) else { return }
            let release = try await lookup(currentCountry)
            try Task.checkCancellation()
            saved.release = release
            saved.checkedAt = now()
            failedAt = nil
            save()
        } catch {
            // URLSession can throw URLError.cancelled rather than CancellationError.
            guard !Task.isCancelled, !(error is CancellationError) else { return }
            failedAt = now()
            Self.log.notice("App Store update check failed; will retry on a later foreground entry.")
        }
    }

    private static func due(_ date: Date?, interval: TimeInterval, at now: Date) -> Bool {
        guard let date else { return true }
        let age = now.timeIntervalSince(date)
        return age < 0 || age >= interval
    }

    private func save() { defaults.set(try? JSONEncoder().encode(saved), forKey: Self.key) }
}

extension EnvironmentValues {
    @Entry var appUpdates: AppUpdates?
}
