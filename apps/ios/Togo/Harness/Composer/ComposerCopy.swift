import Foundation

/// Every string the composer shows. Keys share the `composer.` prefix.
enum ComposerCopy {
    static let send = String(localized: "composer.send", defaultValue: "Send")
    static let queue = String(localized: "composer.queue", defaultValue: "Queue")
    static let interrupt = String(localized: "composer.interrupt", defaultValue: "Interrupt")
    static let stop = String(localized: "composer.stop", defaultValue: "Stop")
    static let commands = String(localized: "composer.commands", defaultValue: "Commands")
    static let mention = String(localized: "composer.mention", defaultValue: "Mention a file")
    static let mode = String(localized: "composer.mode", defaultValue: "Mode")
    static let queued = String(localized: "composer.queued", defaultValue: "Queued")
    static let cancelQueued = String(localized: "composer.cancelQueued", defaultValue: "Take back")
    static let favourite = String(localized: "composer.favourite", defaultValue: "Add to favourites")
    static let unfavourite = String(localized: "composer.unfavourite", defaultValue: "Remove from favourites")
    static let favourites = String(localized: "composer.favourites", defaultValue: "Favourites")
    static let recent = String(localized: "composer.recent", defaultValue: "Recent")
    static let allCommands = String(localized: "composer.allCommands", defaultValue: "All commands")
    static let noCommands = String(localized: "composer.noCommands", defaultValue: "No commands match")
    static let returnSends = String(localized: "composer.returnSends", defaultValue: "Return still sends it as typed.")
    static let filterCommands = String(localized: "composer.filterCommands", defaultValue: "Type to filter")
    static let input = String(localized: "composer.input", defaultValue: "Input")
    static let optional = String(localized: "composer.optional", defaultValue: "optional")
    static let open = String(localized: "composer.open", defaultValue: "Open")
    static let hasNative = String(localized: "composer.hasNative", defaultValue: "Opens a native screen")
    static let hasNativeDetail = String(localized: "composer.hasNativeDetail", defaultValue: "The same choices without typing.")
    static let typesIntoTerminal = String(
        localized: "composer.typesIntoTerminal", defaultValue: "Send types the command into the session. Togo never runs it itself."
    )
    static let takesInput = String(localized: "composer.takesInput", defaultValue: "Takes input")
    static let notSent = String(localized: "composer.notSent", defaultValue: "Not sent. Your words are back in the field.")
    static let notInterrupted = String(localized: "composer.notInterrupted", defaultValue: "Couldn't interrupt. The terminal has Esc.")
    static let commandsFailed = String(localized: "composer.commandsFailed", defaultValue: "Couldn't read the commands")
    static let tryAgain = String(localized: "composer.tryAgain", defaultValue: "Try again")
    static let notCancelled = String(localized: "composer.notCancelled", defaultValue: "Couldn't take it back")
    static let modeNotChanged = String(localized: "composer.modeNotChanged", defaultValue: "The mode didn't change")
    static let focusField = String(localized: "composer.focusField", defaultValue: "Focus the message field")
    static let history = String(localized: "composer.history", defaultValue: "Previous message")

    /// "2 queued".
    static func queuedCount(_ count: Int) -> String {
        String(localized: "composer.queuedCount", defaultValue: "\(count) queued")
    }

    /// "Message Claude Code".
    static func message(_ name: String) -> String {
        String(localized: "composer.message", defaultValue: "Message \(name)")
    }

    /// "Opens Model".
    static func opens(_ title: String) -> String {
        String(localized: "composer.opens", defaultValue: "Opens \(title)")
    }

    /// "Mode: Ask", for VoiceOver.
    static func modeValue(_ label: String) -> String {
        String(localized: "composer.modeValue", defaultValue: "Mode: \(label)")
    }

    // MARK: Pictures

    static let attach = String(localized: "composer.attach", defaultValue: "Add Image")
    static let photos = String(localized: "composer.attach.photos", defaultValue: "Photo Library")
    static let camera = String(localized: "composer.attach.camera", defaultValue: "Take Photo")
    static let pasteImage = String(localized: "composer.attach.paste", defaultValue: "Paste Image")
    static let removeImage = String(localized: "composer.attach.remove", defaultValue: "Remove")
    static let previewImage = String(localized: "composer.attach.preview", defaultValue: "Preview")
    static let retryImage = String(localized: "composer.attach.retry", defaultValue: "Try Again")
    static let uploading = String(localized: "composer.attach.uploading", defaultValue: "Uploading")
    static let uploadFailed = String(localized: "composer.attach.failed", defaultValue: "Not uploaded")
    static let attachUnreadable = String(localized: "composer.attach.unreadable", defaultValue: "That image could not be read.")
    static let attachUnavailable = String(localized: "composer.attach.unavailable",
                                          defaultValue: "This Mac's Togo cannot take images yet. Update Togo on the Mac.")

    /// "Up to 10 images per message."
    static func attachLimit(_ n: Int) -> String {
        String(localized: "composer.attach.limit", defaultValue: "Up to \(n) images per message.")
    }

    /// "Image 2 of 3, uploading", for VoiceOver.
    static func attachment(_ index: Int, of count: Int, state: String?) -> String {
        let base = String(localized: "composer.attach.item", defaultValue: "Image \(index) of \(count)")
        return state.map { "\(base), \($0)" } ?? base
    }
}
