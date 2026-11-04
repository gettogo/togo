#if DEBUG
import Foundation
import Observation
import TogoKit

/// Host profiles for the debug screens, kept in user defaults. Profiles hold no secrets.
@MainActor @Observable final class DebugHostStore {
    private static let defaultsKey = "debug.hosts"
    private(set) var profiles: [HostProfile] = []

    init() {
        if let data = UserDefaults.standard.data(forKey: Self.defaultsKey),
           let saved = try? JSONDecoder().decode([HostProfile].self, from: data) {
            profiles = saved
        }
    }

    func save(_ profile: HostProfile) {
        if let index = profiles.firstIndex(where: { $0.id == profile.id }) {
            profiles[index] = profile
        } else {
            profiles.append(profile)
        }
        persist()
    }

    func delete(at offsets: IndexSet) {
        profiles.remove(atOffsets: offsets)
        persist()
    }

    private func persist() {
        if let data = try? JSONEncoder().encode(profiles) {
            UserDefaults.standard.set(data, forKey: Self.defaultsKey)
        }
    }
}
#endif
