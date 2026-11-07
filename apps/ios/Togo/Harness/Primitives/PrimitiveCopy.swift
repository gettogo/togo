import Foundation

/// Every string the native harness screens show. Keys share the `primitive.` prefix.
enum PrimitiveCopy {
    static let terminal = String(localized: "primitive.terminal", defaultValue: "Terminal")
    static let openTerminal = String(localized: "primitive.openTerminal", defaultValue: "Open terminal")
    static let done = String(localized: "primitive.done", defaultValue: "Done")
    static let sent = String(localized: "primitive.sent", defaultValue: "Sent")
    static let queued = String(localized: "primitive.queued", defaultValue: "Queued")
    static let restarted = String(localized: "primitive.restarted", defaultValue: "Restarted the agent")
    static let failed = String(localized: "primitive.failed", defaultValue: "That didn't work")
    static let openedTerminal = String(localized: "primitive.openedTerminal", defaultValue: "Opened in the terminal")
    static let noAnswer = String(localized: "primitive.noAnswer", defaultValue: "The machine didn't answer")
    static let cancel = String(localized: "primitive.cancel", defaultValue: "Cancel")
    static let close = String(localized: "primitive.close", defaultValue: "Close")
    static let select = String(localized: "primitive.select", defaultValue: "Select")
    static let selected = String(localized: "primitive.selected", defaultValue: "Selected")
    static let details = String(localized: "primitive.details", defaultValue: "Details")
    static let copyID = String(localized: "primitive.copyID", defaultValue: "Copy id")
    static let copied = String(localized: "primitive.copied", defaultValue: "Copied")
    static let all = String(localized: "primitive.all", defaultValue: "All")
    static let other = String(localized: "primitive.other", defaultValue: "Other")
    static let filters = String(localized: "primitive.filters", defaultValue: "Filters")
    static let actions = String(localized: "primitive.actions", defaultValue: "Actions")
    static let search = String(localized: "primitive.search", defaultValue: "Search")
    static let showAll = String(localized: "primitive.showAll", defaultValue: "Show all")
    static let noMatches = String(localized: "primitive.noMatches", defaultValue: "No matches")
    static let tryShorter = String(localized: "primitive.tryShorter", defaultValue: "Try a shorter search or clear the filters.")
    static let nothingHere = String(localized: "primitive.nothingHere", defaultValue: "Nothing here yet")
    static let useAs = String(localized: "primitive.useAs", defaultValue: "Use as")
    static let set = String(localized: "primitive.set", defaultValue: "Set")
    static let notSet = String(localized: "primitive.notSet", defaultValue: "Not set")
    static let replace = String(localized: "primitive.replace", defaultValue: "Replace")
    static let save = String(localized: "primitive.save", defaultValue: "Save")
    static let newValue = String(localized: "primitive.newValue", defaultValue: "New value")
    static let secretFooter = String(
        localized: "primitive.secretFooter",
        defaultValue: "Stored on the machine. Togo never shows it and keeps no copy."
    )
    static let running = String(localized: "primitive.running", defaultValue: "Running")
    static let submit = String(localized: "primitive.submit", defaultValue: "Send answer")
    static let confirm = String(localized: "primitive.confirm", defaultValue: "Confirm")
    static let yourAnswer = String(localized: "primitive.yourAnswer", defaultValue: "Or type an answer")
    static let answered = String(localized: "primitive.answered", defaultValue: "Answered")
    static let carriesOn = String(localized: "primitive.carriesOn", defaultValue: "The agent carries on.")
    static let changedWhileOpen = String(
        localized: "primitive.changedWhileOpen", defaultValue: "The question changed. Check it before you answer."
    )
    static let pickAll = String(localized: "primitive.pickAll", defaultValue: "Pick any that apply")
    static let pickOne = String(localized: "primitive.pickOne", defaultValue: "Pick one")
    static let context = String(localized: "primitive.contextWindow", defaultValue: "Context")
    static let cost = String(localized: "primitive.cost", defaultValue: "Cost")
    static let model = String(localized: "primitive.model", defaultValue: "Model")
    static let mode = String(localized: "primitive.mode", defaultValue: "Mode")
    static let effort = String(localized: "primitive.effort", defaultValue: "Effort")
    static let unknownTitle = String(localized: "primitive.unknownTitle", defaultValue: "Shown in the terminal")
    static let unknownMessage = String(
        localized: "primitive.unknownMessage",
        defaultValue: "This version of Togo can't draw this screen. The terminal has it."
    )
    static let couldNotLoad = String(localized: "primitive.couldNotLoad", defaultValue: "Couldn't read this screen")
    static let fallbackMessage = String(
        localized: "primitive.fallbackMessage", defaultValue: "The agent didn't answer in a way Togo can show. The terminal has it."
    )
    static let tryAgain = String(localized: "primitive.tryAgain", defaultValue: "Try again")
    static let loading = String(localized: "primitive.loading", defaultValue: "Reading")
    static let noBody = String(localized: "primitive.noBody", defaultValue: "Nothing to show")
    static let moreLines = String(localized: "primitive.moreLines", defaultValue: "More in the terminal")
    static let changes = String(localized: "primitive.changes", defaultValue: "Changes")

    /// "Last read 4 min ago" for a stale screen.
    static func stale(_ ago: String) -> String {
        String(localized: "primitive.stale", defaultValue: "Couldn't refresh. Last read \(ago).")
    }

    /// "3 of 7 done".
    static func progress(done: Int, total: Int) -> String {
        String(localized: "primitive.progress", defaultValue: "\(done) of \(total) done")
    }

    /// "Choose an item for default".
    static func choosing(_ label: String) -> String {
        String(localized: "primitive.choosing", defaultValue: "Choose one for \(label)")
    }

    /// "Search 116 items".
    static func searchCount(_ count: Int) -> String {
        String(localized: "primitive.searchCount", defaultValue: "Search \(count) items")
    }

    /// "Resets in 2 hours".
    static func resets(_ relative: String) -> String {
        String(localized: "primitive.resets", defaultValue: "Resets \(relative)")
    }

    /// "Question 2 of 3".
    static func question(_ index: Int, of count: Int) -> String {
        String(localized: "primitive.question", defaultValue: "Question \(index) of \(count)")
    }

    /// "2 roles".
    static func holds(_ count: Int) -> String {
        String(localized: "primitive.holds", defaultValue: "\(count) roles")
    }

    /// "+12 -3", a file's change counts.
    static func lines(added: Int, removed: Int) -> String {
        "+\(added) -\(removed)"
    }

    /// "12 added, 3 removed", for VoiceOver.
    static func linesSpoken(added: Int, removed: Int) -> String {
        String(localized: "primitive.linesSpoken", defaultValue: "\(added) added, \(removed) removed")
    }
}
