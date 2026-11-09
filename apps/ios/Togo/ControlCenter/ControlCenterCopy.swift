import Foundation
import TogoKit

/// Every sentence the control center shows, keyed under `cc.` in the string catalog.
enum ControlCenterCopy {
    static let newSession = String(localized: "cc.newSession", defaultValue: "New session")
    static let settings = String(localized: "cc.settings", defaultValue: "Settings")
    static let waitingOnYou = String(localized: "cc.waitingOnYou", defaultValue: "Waiting on you")
    static let filter = String(localized: "cc.filter", defaultValue: "Show")
    static let searchPrompt = String(localized: "cc.searchPrompt", defaultValue: "Sessions, folders, agents")

    static func filterName(_ filter: SessionFilter) -> String {
        switch filter {
        case .all: String(localized: "cc.filter.all", defaultValue: "All")
        case .needsYou: String(localized: "cc.filter.needsYou", defaultValue: "Needs you")
        case .working: String(localized: "cc.filter.working", defaultValue: "Working")
        case .done: String(localized: "cc.filter.done", defaultValue: "Done")
        }
    }

    static func filterEmpty(_ filter: SessionFilter) -> String {
        switch filter {
        case .all, .needsYou: String(localized: "cc.filterEmpty.needsYou", defaultValue: "Nothing needs you right now")
        case .working: String(localized: "cc.filterEmpty.working", defaultValue: "No sessions are working")
        case .done: String(localized: "cc.filterEmpty.done", defaultValue: "No finished sessions")
        }
    }

    static func noMatches(_ query: String) -> String {
        String(localized: "cc.noMatches", defaultValue: "No sessions match \u{201C}\(query)\u{201D}")
    }

    // MARK: First run

    static let firstRunTitle = String(localized: "cc.firstRun.title", defaultValue: "Add a machine")
    static let firstRunMessage = String(
        localized: "cc.firstRun.message", defaultValue: "Togo connects over SSH to your Mac or Linux machine."
    )
    static let addMachine = String(localized: "cc.firstRun.add", defaultValue: "Add machine")
    static let withoutHelper = String(localized: "cc.firstRun.withoutHelper", defaultValue: "Connect without the helper")

    // MARK: Machines

    static func offlineSeen(_ ago: String) -> String {
        String(localized: "cc.machine.offlineSeen", defaultValue: "Offline, seen \(ago)")
    }

    static let offline = String(localized: "cc.machine.offline", defaultValue: "Offline")
    static let waking = String(localized: "cc.machine.waking", defaultValue: "Waking")
    static let connecting = String(localized: "cc.machine.connecting", defaultValue: "Connecting")
    static let noHelper = String(localized: "cc.machine.noHelper", defaultValue: "No helper")
    static let plainSSH = String(
        localized: "cc.machine.plainSSH", defaultValue: "No helper. Install it to see what agents are doing."
    )
    static let install = String(localized: "cc.machine.install", defaultValue: "Install")
    static let tryAgain = String(localized: "cc.machine.tryAgain", defaultValue: "Try again")
    static let signInAgain = String(localized: "cc.machine.signInAgain", defaultValue: "Sign in again")
    static let edit = String(localized: "cc.machine.edit", defaultValue: "Edit")
    static let update = String(localized: "cc.machine.update", defaultValue: "Update")

    /// A connected helper older than this app needs; Update walks through installing the new one.
    static func helperOutdated(_ version: String) -> String {
        version.isEmpty
            ? String(localized: "cc.machine.helperOutdatedUnknown", defaultValue: "The helper needs an update.")
            : String(localized: "cc.machine.helperOutdated", defaultValue: "Helper \(version) needs an update.")
    }

    /// The helper is ahead of this app. Only updating the app helps, so there is no button.
    static func helperNewer(_ machine: String) -> String {
        String(localized: "cc.machine.helperNewer", defaultValue: "The helper on \(machine) is newer than this app. Update Togo from the App Store.")
    }

    /// Keys never sync, so a machine added on another device can name one this device lacks.
    static func keyMissing(device: String) -> String {
        String(localized: "cc.machine.keyMissing", defaultValue: "Key not on this \(device)")
    }

    /// A machine added on another device, whose key list does not have this device's key yet.
    /// Nothing failed, so it never says "sign in again".
    static func setUpHere(_ machine: String, device: String) -> String {
        String(
            localized: "cc.machine.setUpHere",
            defaultValue: "\(machine) was added on another device. Add this \(device)'s key to use it here."
        )
    }

    static let setUp = String(localized: "cc.machine.setUp", defaultValue: "Set up")

    /// The machine showed another key. Reviewing it in Edit machine is the only way on: there
    /// the old key can be forgotten if the machine really was reinstalled.
    static let hostKeyChanged = String(
        localized: "cc.machine.hostKeyChanged",
        defaultValue: "This machine's key changed. If nobody reinstalled it, someone may be intercepting the connection."
    )
    static let reviewKey = String(localized: "cc.machine.reviewKey", defaultValue: "Review key")
    static let keyChangedCaption = String(localized: "cc.machine.keyChangedCaption", defaultValue: "Key changed")

    /// The phone itself has no network, so no machine is tried.
    static func deviceOffline(_ device: String) -> String {
        String(localized: "cc.machine.deviceOffline", defaultValue: "This \(device) is offline. Togo reconnects when the network is back.")
    }

    static let noNetwork = String(localized: "cc.machine.noNetwork", defaultValue: "No network")

    static func signedOut(_ machine: String) -> String {
        String(localized: "cc.machine.signedOut", defaultValue: "\(machine) needs you to sign in again.")
    }

    static func minutesAgo(_ minutes: Int) -> String {
        String(localized: "cc.ago.minutes", defaultValue: "\(minutes) min ago")
    }

    static func hoursAgo(_ hours: Int) -> String {
        String(localized: "cc.ago.hours", defaultValue: "\(hours) h ago")
    }

    static let justNow = String(localized: "cc.ago.now", defaultValue: "just now")

    // MARK: Rows

    static let muted = String(localized: "cc.row.muted", defaultValue: "Muted")
    static let open = String(localized: "cc.action.open", defaultValue: "Open")
    static let connectWith = String(localized: "cc.action.connectWith", defaultValue: "Connect with")
    static let rename = String(localized: "cc.action.rename", defaultValue: "Rename")
    static let mute = String(localized: "cc.action.mute", defaultValue: "Mute")
    static let unmute = String(localized: "cc.action.unmute", defaultValue: "Unmute")
    static let copyPath = String(localized: "cc.action.copyPath", defaultValue: "Copy path")
    static let kill = String(localized: "cc.action.kill", defaultValue: "End session")
    static let cancel = String(localized: "cc.action.cancel", defaultValue: "Cancel")
    static let name = String(localized: "cc.rename.field", defaultValue: "Name")
    static let startedOutside = String(localized: "cc.row.startedOutside", defaultValue: "Started outside Togo")

    static func machineOffline(_ machine: String) -> String {
        String(localized: "cc.row.machineOffline", defaultValue: "\(machine) is offline")
    }

    static func machineNotConnected(_ machine: String) -> String {
        String(localized: "cc.row.machineNotConnected", defaultValue: "\(machine) is not connected")
    }

    static func killTitle(_ label: String) -> String {
        String(localized: "cc.kill.title", defaultValue: "End \(label)?")
    }

    static let killMessage = String(localized: "cc.kill.message", defaultValue: "The agent stops and its screen is gone.")

    static func renameTitle(_ label: String) -> String {
        String(localized: "cc.rename.title", defaultValue: "Rename \(label)")
    }

    static func transport(_ transport: TransportPreference) -> String {
        switch transport {
        case .auto: String(localized: "cc.transport.auto", defaultValue: "Auto")
        case .ssh: String(localized: "cc.transport.ssh", defaultValue: "SSH")
        case .mosh: String(localized: "cc.transport.mosh", defaultValue: "Mosh")
        case .et: String(localized: "cc.transport.et", defaultValue: "ET")
        }
    }

    // MARK: Requests

    static let approve = String(localized: "cc.request.approve", defaultValue: "Approve")
    static let deny = String(localized: "cc.request.deny", defaultValue: "Deny")
    static let more = String(localized: "cc.request.more", defaultValue: "More")
    static let reply = String(localized: "cc.request.reply", defaultValue: "Reply")
    static let send = String(localized: "cc.request.send", defaultValue: "Send")
    static let approved = String(localized: "cc.request.approved", defaultValue: "Approved")
    static let denied = String(localized: "cc.request.denied", defaultValue: "Denied")
    static let answeredElsewhere = String(localized: "cc.request.answeredElsewhere", defaultValue: "Answered at the desk")
    static let expired = String(localized: "cc.request.expired", defaultValue: "Expired")

    static func answerFailed(_ machine: String) -> String {
        String(localized: "cc.request.failed", defaultValue: "Couldn't reach \(machine). Try again.")
    }

    // MARK: Archive

    static let archive = String(localized: "cc.archive.title", defaultValue: "Archive")

    static func finishedSessions(_ count: Int) -> String {
        String(localized: "cc.archive.count", defaultValue: "\(count) finished sessions")
    }
}
