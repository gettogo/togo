import Foundation
import Observation
import TogoKit

/// Something the standard setup needs on the machine. Only Mosh is optional: without the helper
/// Togo is a plain terminal, with no chat, approvals or notifications.
enum SetupItem: String, CaseIterable, Codable, Sendable {
    case remoteLogin, reachable, phoneKey, helper, mosh

    var isOptional: Bool {
        self == .mosh
    }
}

/// What connecting to a machine proved. Any answer at all means SSH is on and the phone can
/// reach it; `keyAccepted` means the sign-in with this phone's key worked.
struct SetupDetection: Equatable, Sendable {
    var keyAccepted: Bool
    var report: HelperReport?
}

/// Where the user is in the guide and what they have ticked. Everything is saved as it changes,
/// so a guide left half way opens on the same page next launch.
@MainActor @Observable final class TutorialModel {
    private(set) var page: TutorialPage
    /// True once the guide was finished or skipped; it then opens only from Settings.
    private(set) var seen: Bool
    private(set) var ticked: Set<SetupItem>
    /// Items found on a machine, as opposed to ticked by hand.
    private(set) var detected: Set<SetupItem>
    /// Tips are keyed by this number, so raising it shows every tip again.
    private(set) var tipsGeneration: Int

    @ObservationIgnored private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        page = TutorialPage(rawValue: defaults.integer(forKey: Keys.page)) ?? .welcome
        seen = defaults.bool(forKey: Keys.seen)
        ticked = Self.items(defaults.stringArray(forKey: Keys.ticked))
        detected = Self.items(defaults.stringArray(forKey: Keys.detected))
        tipsGeneration = defaults.integer(forKey: Keys.tipsGeneration)
    }

    var showsOnLaunch: Bool {
        !seen
    }

    /// Whether the guide opens by itself now. `seen` lives on this device only, so a device's
    /// first launch can find machines already here from iCloud, and the welcome that offers the
    /// guide is skipped or gone in a moment. The guide then opens once the launch has played and
    /// no sheet is up. Without machines the welcome offers it, and nothing opens by itself; nor
    /// after the user added a machine from the welcome (`startedFromWelcome`), having passed
    /// the guide by.
    func opensOnLaunch(hasMachines: Bool, startedFromWelcome: Bool, launchPlaying: Bool, sheetShown: Bool) -> Bool {
        showsOnLaunch && hasMachines && !startedFromWelcome && !launchPlaying && !sheetShown
    }

    func go(to page: TutorialPage) {
        self.page = page
        defaults.set(page.rawValue, forKey: Keys.page)
    }

    func next() {
        TutorialPage(rawValue: page.rawValue + 1).map(go)
    }

    func back() {
        TutorialPage(rawValue: page.rawValue - 1).map(go)
    }

    func skip() {
        markSeen()
    }

    func finish() {
        markSeen()
    }

    /// The "Getting started" row: the guide from its first page, and every tip once more.
    func reopen() {
        go(to: .welcome)
        tipsGeneration += 1
        defaults.set(tipsGeneration, forKey: Keys.tipsGeneration)
    }

    func toggle(_ item: SetupItem) {
        if ticked.contains(item) {
            ticked.remove(item)
        } else {
            ticked.insert(item)
        }
        save(ticked, Keys.ticked)
    }

    /// Ticks what a connection found. It only ever adds: a machine without Mosh says nothing
    /// about another machine the user set up by hand.
    func record(_ detection: SetupDetection) {
        var found: Set<SetupItem> = [.remoteLogin, .reachable]
        if detection.keyAccepted {
            found.insert(.phoneKey)
        }
        if let report = detection.report {
            if case .present = report.helper { found.insert(.helper) }
            if report.mosh { found.insert(.mosh) }
        }
        detected.formUnion(found)
        ticked.formUnion(found)
        save(detected, Keys.detected)
        save(ticked, Keys.ticked)
    }

    private func markSeen() {
        seen = true
        defaults.set(true, forKey: Keys.seen)
    }

    private func save(_ items: Set<SetupItem>, _ key: String) {
        defaults.set(items.map(\.rawValue).sorted(), forKey: key)
    }

    private static func items(_ raw: [String]?) -> Set<SetupItem> {
        Set((raw ?? []).compactMap(SetupItem.init(rawValue:)))
    }

    enum Keys {
        static let page = "tutorial.page"
        static let seen = "tutorial.seen"
        static let ticked = "tutorial.ticked"
        static let detected = "tutorial.detected"
        static let tipsGeneration = "tutorial.tipsGeneration"
    }
}
