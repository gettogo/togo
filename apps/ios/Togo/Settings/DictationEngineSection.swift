import SwiftUI
import TogoKit

/// The Whisper part of Settings > Dictation: choosing the engine, downloading the model on a
/// tap, and deleting it again. The engine only switches to Whisper once its model is here.
@MainActor @Observable final class WhisperSettingsModel {
    /// Whisper is picked, so its model row shows, whether or not the model is here yet.
    private(set) var showsWhisper: Bool
    /// The fraction downloaded while the model downloads.
    private(set) var fraction: Double?
    private(set) var failure: String?
    private(set) var isInstalled: Bool
    /// The settings as they are now, kept current by the page, so a download that finishes
    /// minutes later changes only the engine and keeps every other setting as it is then.
    var settings = AppSettings()
    @ObservationIgnored let whisper: WhisperEngine
    @ObservationIgnored private var task: Task<Void, Never>?

    init(whisper: WhisperEngine = EngineChoice.shared.whisper, showsWhisper: Bool = false, fraction: Double? = nil) {
        self.whisper = whisper
        self.showsWhisper = showsWhisper
        self.fraction = fraction
        isInstalled = whisper.isInstalled
        whisper.removeLeftovers()
    }

    var selection: DictationEngineChoice {
        showsWhisper || (settings.dictationEngine == .whisper && isInstalled) ? .whisper : .apple
    }

    func choose(_ choice: DictationEngineChoice, update: (AppSettings) -> Void) {
        failure = nil
        switch choice {
        case .apple:
            showsWhisper = false
            guard settings.dictationEngine != .apple else { return }
            apply(.apple, model: settings.whisperModel, update: update)
        case .whisper:
            showsWhisper = true
            // Without its model Whisper stays a choice on this page; dictation keeps Apple's.
            guard isInstalled else { return }
            apply(.whisper, model: whisper.model.id, update: update)
        }
    }

    /// Starts the download the user tapped. Cancel stops it and leaves nothing behind.
    func startDownload(update: @escaping (AppSettings) -> Void) {
        guard task == nil else { return }
        task = Task { [weak self] in
            await self?.download(update: update)
            self?.task = nil
        }
    }

    func cancelDownload() {
        task?.cancel()
    }

    func download(update: (AppSettings) -> Void) async {
        guard fraction == nil else { return }
        failure = nil
        fraction = 0
        showsWhisper = true
        do {
            try await whisper.installModel(locale: .current) { [weak self] in self?.fraction = $0 }
            fraction = nil
            isInstalled = whisper.isInstalled
            apply(.whisper, model: whisper.model.id, update: update)
        } catch {
            fraction = nil
            isInstalled = whisper.isInstalled
            if !Task.isCancelled { failure = DictationCopy.whisperFailed }
        }
    }

    /// Frees the model's space and goes back to Apple's speech. If the files will not go,
    /// the page says so and keeps offering Delete.
    func delete(update: (AppSettings) -> Void) {
        do {
            try whisper.deleteModel()
            failure = nil
        } catch {
            failure = DictationCopy.whisperDeleteFailed
        }
        isInstalled = whisper.isInstalled
        showsWhisper = isInstalled
        apply(.apple, model: isInstalled ? settings.whisperModel : nil, update: update)
    }

    private func apply(_ engine: DictationEngineChoice, model: String?, update: (AppSettings) -> Void) {
        var changed = settings
        changed.dictationEngine = engine
        changed.whisperModel = model
        settings = changed
        update(changed)
    }
}

/// The Engine section: Apple or Whisper, and Whisper's model when it is picked.
struct DictationEngineSection: View {
    let model: WhisperSettingsModel
    let update: (AppSettings) -> Void

    private var whisper: WhisperModel { model.whisper.model }
    private var size: String { ByteCountFormatter.string(fromByteCount: whisper.bytes, countStyle: .file) }

    var body: some View {
        Section {
            Picker(DictationCopy.engine, selection: selection) {
                Text(DictationCopy.engineApple).tag(DictationEngineChoice.apple)
                Text(DictationCopy.engineWhisper).tag(DictationEngineChoice.whisper)
            }
            .pickerStyle(.inline)
            .labelsHidden()
        } header: {
            Text(DictationCopy.engine)
        } footer: {
            if model.selection == .apple { Text(DictationCopy.engineFooter) }
        }
        .groupedRows()
        if model.selection == .whisper {
            Section {
                whisperRows
            } footer: {
                VStack(alignment: .leading, spacing: 8) {
                    Text(model.isInstalled ? DictationCopy.whisperInstalledFooter(size: size) : DictationCopy.whisperFooter)
                    Text(DictationCopy.whisperCredit)
                }
            }
            .groupedRows()
        }
    }

    private var selection: Binding<DictationEngineChoice> {
        Binding(get: { model.selection }, set: { model.choose($0, update: update) })
    }

    @ViewBuilder private var whisperRows: some View {
        if let fraction = model.fraction {
            VStack(alignment: .leading, spacing: 8) {
                Text(DictationCopy.whisperDownloading(whisper.name))
                ProgressView(value: fraction)
                    .motion(.progress, value: fraction)
                    .tint(Color.togoSnow)
            }
            .padding(.vertical, 6)
            .accessibilityElement(children: .combine)
            Button(DictationCopy.whisperCancel, role: .cancel, action: model.cancelDownload)
                .foregroundStyle(Color.togoSnow)
                .frame(minHeight: 44)
        } else {
            LabeledContent {
                Text(model.isInstalled ? DictationCopy.whisperDownloaded : size)
                    .foregroundStyle(Color.togoMute)
            } label: {
                Text(whisper.name)
            }
            .frame(minHeight: 44)
            if let failure = model.failure {
                Text(failure)
                    .frame(minHeight: 44)
            }
            if model.isInstalled {
                Button(DictationCopy.whisperDelete, role: .destructive) { model.delete(update: update) }
                    .frame(minHeight: 44)
            } else {
                Button(DictationCopy.whisperDownload(whisper.name, size: size)) { model.startDownload(update: update) }
                    .fontWeight(.semibold)
                    .foregroundStyle(Color.togoSnow)
                    .frame(minHeight: 44)
            }
        }
    }
}

extension DictationCopy {
    static let engine = String(localized: "dictation.engine", defaultValue: "Engine")
    static let engineApple = String(localized: "dictation.engine.apple", defaultValue: "Apple")
    static let engineWhisper = String(localized: "dictation.engine.whisper", defaultValue: "Whisper")
    static let engineFooter = String(
        localized: "dictation.engine.footer",
        defaultValue: "Apple's speech is built into iOS. Whisper is a model you download once."
    )
    static let whisperFooter = String(
        localized: "dictation.whisper.footer", defaultValue: "Downloaded once from Hugging Face, then runs on this device."
    )
    static let whisperDownloaded = String(localized: "dictation.whisper.downloaded", defaultValue: "Downloaded")
    static let whisperCancel = String(localized: "dictation.whisper.cancel", defaultValue: "Cancel download")
    static let whisperDelete = String(localized: "dictation.whisper.delete", defaultValue: "Delete model")
    static let whisperFailed = String(
        localized: "dictation.whisper.failed",
        defaultValue: "The download didn't finish. Dictation keeps using Apple speech."
    )
    static let whisperFailedToLoad = String(
        localized: "dictation.whisper.failedToLoad",
        defaultValue: "Whisper couldn't start. Try again in a moment."
    )
    static let whisperDeleteFailed = String(
        localized: "dictation.whisper.deleteFailed",
        defaultValue: "Couldn't delete the Whisper model. Try again."
    )
    static let whisperDamaged = String(
        localized: "dictation.whisper.damaged",
        defaultValue: "The Whisper model is damaged, so dictation uses Apple speech. Download it again in Settings > Dictation."
    )
    static let whisperCredit = String(
        localized: "dictation.whisper.credit",
        defaultValue: "Whisper is by OpenAI, converted for this device by Argmax. Both are MIT licensed."
    )
    static let whisperMissing = String(
        localized: "dictation.whisper.missing", defaultValue: "Download the Whisper model in Settings, Dictation."
    )
    static let whisperFailedToHear = String(
        localized: "dictation.whisper.failedToHear", defaultValue: "Whisper couldn't turn that into text."
    )

    static func whisperDownload(_ name: String, size: String) -> String {
        String(localized: "dictation.whisper.download", defaultValue: "Download \(name), \(size)")
    }

    static func whisperDownloading(_ name: String) -> String {
        String(localized: "dictation.whisper.downloading", defaultValue: "Downloading \(name)")
    }

    static func whisperInstalledFooter(size: String) -> String {
        String(
            localized: "dictation.whisper.installedFooter",
            defaultValue: "Downloaded once from Hugging Face, then runs on this device. Delete the model to free \(size) and go back to Apple speech."
        )
    }
}
