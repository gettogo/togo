import Foundation

/// Every string the new session flow shows. Keys share the `new.` prefix in the catalog.
enum NewSessionCopy {
    static let cancel = String(localized: "new.cancel", defaultValue: "Cancel")
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
    static let otherHeading = String(localized: "new.folder.other", defaultValue: "Other folders")
    static let home = String(localized: "new.folder.home", defaultValue: "Home")
    static let browse = String(localized: "new.folder.browse", defaultValue: "Browse")
    static let browseLoading = String(localized: "new.folder.browseLoading", defaultValue: "Opening folder")
    static let browseEmpty = String(localized: "new.folder.browseEmpty", defaultValue: "No folders here")
    static let useFolder = String(localized: "new.folder.use", defaultValue: "Use this folder")
    static let showHidden = String(localized: "new.folder.showHidden", defaultValue: "Show hidden folders")
    static let up = String(localized: "new.folder.up", defaultValue: "Parent folder")
    static let retry = String(localized: "new.retry", defaultValue: "Try again")

    static let plainBody = String(localized: "new.plain.body", defaultValue: "Starts a tmux session in your home folder")
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

    static let badFolder = String(localized: "new.error.badFolder", defaultValue: "That folder no longer exists.")
    static func full(_ machine: String) -> String {
        String(localized: "new.error.full", defaultValue: "\(machine) has 99 sessions. Close one first.")
    }
    static let thisMachine = String(localized: "new.thisMachine", defaultValue: "This machine")
}
