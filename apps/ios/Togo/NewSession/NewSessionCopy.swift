import Foundation

/// Every string the new session flow shows. Keys share the `new.` prefix in the catalog.
enum NewSessionCopy {
    static let machineTitle = String(localized: "new.machine.title", defaultValue: "Machine")
    static let machinePrompt = String(localized: "new.machine.prompt", defaultValue: "Where should it run?")
    static let folderTitle = String(localized: "new.folder.title", defaultValue: "Folder")
    static let agentTitle = String(localized: "new.agent.title", defaultValue: "Agent")
    static let plainTitle = String(localized: "new.plain.title", defaultValue: "New session")

    static let helperMissing = String(localized: "new.machine.helperMissing", defaultValue: "Helper not installed")
    static let connecting = String(localized: "new.machine.connecting", defaultValue: "Connecting")
    static let waking = String(localized: "new.machine.waking", defaultValue: "Waking up")
    static let authNeeded = String(localized: "new.machine.authNeeded", defaultValue: "Needs sign-in")
    static func keyMissing(device: String) -> String {
        String(localized: "new.machine.keyMissing", defaultValue: "Key not on this \(device)")
    }
    static func offline(_ reason: String) -> String {
        String(localized: "new.machine.offline", defaultValue: "Offline, \(reason.lowercased())")
    }

    static let recentHeading = String(localized: "new.folder.recent", defaultValue: "Recent folders")
    static let recentLoading = String(localized: "new.folder.recentLoading", defaultValue: "Loading recent folders")
    static let recentEmpty = String(localized: "new.folder.recentEmpty", defaultValue: "No recent folders yet")
    static let removeRecent = String(localized: "new.folder.removeRecent", defaultValue: "Remove from Recent")
    static let otherHeading = String(localized: "new.folder.other", defaultValue: "Other folders")
    static let home = String(localized: "new.folder.home", defaultValue: "Home")
    static let browse = String(localized: "new.folder.browse", defaultValue: "Browse")
    static let browseLoading = String(localized: "new.folder.browseLoading", defaultValue: "Opening folder")
    static let browseEmpty = String(localized: "new.folder.browseEmpty", defaultValue: "No folders here")
    static let useFolder = String(localized: "new.folder.use", defaultValue: "Use this folder")
    static let showHidden = String(localized: "new.folder.showHidden", defaultValue: "Show hidden folders")
    static let up = String(localized: "new.folder.up", defaultValue: "Parent folder")
    static let retry = String(localized: "new.retry", defaultValue: "Try again")

    static let plainBody = String(localized: "new.plain.body", defaultValue: "A shell in your home folder that keeps running.")
    static let plainStart = String(localized: "new.plain.start", defaultValue: "Start")

    static let agentLoading = String(localized: "new.agent.loading", defaultValue: "Looking for agents")
    static let namePlaceholder = String(localized: "new.agent.name", defaultValue: "Name (optional)")
    static func notInstalled(on machine: String) -> String {
        String(localized: "new.agent.notInstalled", defaultValue: "Not installed on \(machine)")
    }
    static func start(_ agent: String) -> String {
        String(localized: "new.agent.start", defaultValue: "Start \(agent)")
    }
    static let starting = String(localized: "new.agent.starting", defaultValue: "Starting")

    static let flags = String(localized: "new.agent.flags", defaultValue: "Flags")
    static func flagsNeedUpdate(_ machine: String) -> String {
        String(localized: "new.agent.flags.needsUpdate", defaultValue: "Update the helper on \(machine) to use flags.")
    }
    static let flagsUnbalanced = String(localized: "new.agent.flags.unbalanced", defaultValue: "A quote is not closed.")
    static let flagsControl = String(localized: "new.agent.flags.control", defaultValue: "Flags can't contain control characters.")
    static let flagsTooLong = String(localized: "new.agent.flags.tooLong", defaultValue: "These flags are too long.")
    static let flagsEmpty = String(
        localized: "new.agent.flags.empty", defaultValue: "Empty quotes make an empty flag. Remove them or put text inside."
    )
    static let flagsInvisible = String(
        localized: "new.agent.flags.invisible", defaultValue: "Flags can't contain invisible characters."
    )
    static let rememberFolder = String(localized: "new.agent.remember", defaultValue: "Remember for this folder")
    static let addFavorite = String(localized: "new.agent.favorite.add", defaultValue: "Add to favorites")
    static let removeFavorite = String(localized: "new.agent.favorite.remove", defaultValue: "Remove from favorites")
    static let notInstalledHeading = String(localized: "new.agent.missing", defaultValue: "Not installed")
    static func notInstalledCount(_ count: Int) -> String {
        String(localized: "new.agent.missing.count", defaultValue: "\(count) agents")
    }
    static let expanded = String(localized: "new.agent.missing.expanded", defaultValue: "Expanded")
    static let collapsed = String(localized: "new.agent.missing.collapsed", defaultValue: "Collapsed")
    static let lastUsedHere = String(localized: "new.agent.lastUsed", defaultValue: "Last used here")

    static let badFolder = String(localized: "new.error.badFolder", defaultValue: "That folder no longer exists.")
    static func full(_ machine: String) -> String {
        String(localized: "new.error.full", defaultValue: "\(machine) has 99 sessions. Close one first.")
    }
    static let thisMachine = String(localized: "new.thisMachine", defaultValue: "This machine")
}
