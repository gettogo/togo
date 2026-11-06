import Foundation

/// Every sentence dictation shows, keyed under `dictation.` in the string catalog.
enum DictationCopy {
    static let dictate = String(localized: "dictation.dictate", defaultValue: "Dictate")
    static let hint = String(
        localized: "dictation.hint", defaultValue: "Hold to talk and let go to add the words. Or tap to start and tap again to stop."
    )
    static let stop = String(localized: "dictation.stop", defaultValue: "Stop dictating")
    static let listening = String(localized: "dictation.listening", defaultValue: "Listening")
    static let denied = String(
        localized: "dictation.denied", defaultValue: "Togo can't use the microphone. Turn it on in Settings."
    )
    static let openSettings = String(localized: "dictation.openSettings", defaultValue: "Open settings")
    static let unsupported = String(
        localized: "dictation.unsupported", defaultValue: "Dictation isn't available for this language."
    )
    static let dismiss = String(localized: "dictation.dismiss", defaultValue: "Dismiss")
    static let stopped = String(localized: "dictation.stopped", defaultValue: "Dictation stopped.")
    static let stoppedKept = String(
        localized: "dictation.stoppedKept", defaultValue: "Dictation stopped. What it heard is in the message."
    )
    static let download = String(localized: "dictation.downloadButton", defaultValue: "Download")

    static func needsModel(language: String, bytes: Int64?) -> String {
        guard let bytes else {
            return String(localized: "dictation.download", defaultValue: "Download speech for \(language)")
        }
        let size = ByteCountFormatter.string(fromByteCount: bytes, countStyle: .file)
        return String(localized: "dictation.downloadSize", defaultValue: "Download speech for \(language), \(size)")
    }

    static func downloading(language: String) -> String {
        String(localized: "dictation.downloading", defaultValue: "Downloading speech for \(language)")
    }

    /// "English (US)": the language in full and the region as its short code, so a list of
    /// variants stays readable.
    static func language(for locale: Locale, in display: Locale = .current) -> String {
        let code = locale.language.languageCode?.identifier ?? locale.identifier
        let name = display.localizedString(forLanguageCode: code) ?? code
        guard let region = locale.region?.identifier else { return name }
        return "\(name) (\(region))"
    }

    // MARK: Settings

    static let title = String(localized: "dictation.settings.title", defaultValue: "Dictation")
    static let language = String(localized: "dictation.settings.language", defaultValue: "Language")
    static let automatic = String(localized: "dictation.settings.automatic", defaultValue: "Automatic")
    static let languageFooter = String(
        localized: "dictation.settings.languageFooter",
        defaultValue: "Hold the microphone in the compose bar to talk. The words go into the message, and nothing is sent until you tap Send."
    )
    static let speech = String(localized: "dictation.settings.speech", defaultValue: "Speech")
    static let installed = String(localized: "dictation.settings.installed", defaultValue: "Installed")
    static let checking = String(localized: "dictation.settings.checking", defaultValue: "Checking")
    static let speechFooter = String(
        localized: "dictation.settings.speechFooter",
        defaultValue: "Speech becomes text on this device. Audio and words never leave it."
    )
    static let recent = String(localized: "dictation.settings.recent", defaultValue: "Recent dictations")
    static let insert = String(localized: "dictation.settings.insert", defaultValue: "Insert")
    static let clear = String(localized: "dictation.settings.clear", defaultValue: "Clear recent dictations")
    static let noneYet = String(localized: "dictation.settings.none", defaultValue: "Nothing dictated yet.")
    static let recentFooter = String(
        localized: "dictation.settings.recentFooter",
        defaultValue: "The last 20, kept on this device only. Insert puts one in the compose bar of the session on screen."
    )
    static func speechFor(_ language: String) -> String {
        String(localized: "dictation.settings.speechFor", defaultValue: "Speech for \(language)")
    }
}
