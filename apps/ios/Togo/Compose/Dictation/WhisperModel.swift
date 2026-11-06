import Foundation

/// A Whisper model Togo can download: the files it is made of, where each comes from and
/// what it must hash to. Every file is pinned to one revision of its Hugging Face repository,
/// so a changed file upstream fails the check instead of reaching the device.
struct WhisperModel: Sendable, Equatable, Identifiable {
    struct File: Sendable, Equatable {
        /// The Hugging Face repository and the commit the file is pinned to.
        var repo: String
        var revision: String
        /// Its path in the repository.
        var path: String
        /// Its path in the model's folder on this device.
        var local: String
        var bytes: Int64
        /// SHA-256 of the file, lowercase hex.
        var sha256: String

        var url: URL {
            URL(string: "https://huggingface.co/\(repo)/resolve/\(revision)/\(path)")!
        }
    }

    var id: String
    var name: String
    var files: [File]

    var bytes: Int64 { files.reduce(0) { $0 + $1.bytes } }

    /// Whisper listens at 16 kHz, one channel.
    static let sampleRate = 16_000

    /// Where the tokenizer's files sit inside the model's folder.
    static let tokenizerFolder = "models/openai/whisper-base"

    /// Whisper Base, multilingual: Argmax's Core ML conversion, with OpenAI's tokenizer.
    static let base: WhisperModel = {
        let models = "argmaxinc/whisperkit-coreml"
        let modelsRevision = "0f63a7800b00dd0226abd051b906c246e1907482"
        let folder = "openai_whisper-base"
        let parts: [(String, Int64, String)] = [
            ("AudioEncoder.mlmodelc/analytics/coremldata.bin", 243, "c4e096b2abd561f00b9b698401df3fbe1a0d0c8d2476ff19cb4e1995680e827e"),
            ("AudioEncoder.mlmodelc/coremldata.bin", 347, "e316980638e2099e83cb1a93b903717dc12b3c2168d0ab69113764c3767696ba"),
            ("AudioEncoder.mlmodelc/metadata.json", 1_863, "d6785fae31c3f86d60058c14c79902f716f106dd9e36af7d3fe78b9c34867fce"),
            ("AudioEncoder.mlmodelc/model.mil", 579_125, "dc74813e26fce790fe22a33dcef8be535b9dce566f38a8bdb677f5e07dcf9138"),
            ("AudioEncoder.mlmodelc/model.mlmodel", 79_853, "1d42038f84b508da5ce9b953302387ffedc097c346d36a56b765109002b6080e"),
            ("AudioEncoder.mlmodelc/weights/weight.bin", 41_189_632, "061ff4d74e5de3937b31288465d6c6f2697f92d121c80b23f51dd26bbdfe642b"),
            ("MelSpectrogram.mlmodelc/analytics/coremldata.bin", 243, "7f77e6457285248f99cd7aa3fd4cc2efbb17733e63e7023ac53abe1f95785d07"),
            ("MelSpectrogram.mlmodelc/coremldata.bin", 328, "dabdc5aa69f6ef4d97dc9499f5c30514e00e96b53b750b33a5a6471363c71662"),
            ("MelSpectrogram.mlmodelc/metadata.json", 1_848, "f2b08d80d9cdd39fc0ccdbb5fac86a5f8dd9bcaa839706c3568be6fe8abd82d4"),
            ("MelSpectrogram.mlmodelc/model.mil", 10_176, "b8063d8e57c113472ac7c2d248e44383568a018978a753c1884ac406b997a374"),
            ("MelSpectrogram.mlmodelc/weights/weight.bin", 354_080, "35d74417ef9c765e70f4ef85fe7405015a7086e9af05e3b63a5c2c7c748b2efc"),
            ("TextDecoder.mlmodelc/analytics/coremldata.bin", 243, "6ac1227740ecc2fd7a03df50ac6e2a7f7946acfa77069cf2c486ae0255356b95"),
            ("TextDecoder.mlmodelc/coremldata.bin", 633, "9f1f6fe409486e2797d3f0c65d9a6d5af596771760548cd86f41939c54cdbe7c"),
            ("TextDecoder.mlmodelc/metadata.json", 4_753, "0a64b3686b9a4b2eff0792e7df3cfe1b20467ec5d5ac52c0e01c3dbf6697b65d"),
            ("TextDecoder.mlmodelc/model.mil", 205_217, "45b9e61a0f286cddcad96e2c890c7a68460adcaa3319c4907ee4f962dfeb2c8b"),
            ("TextDecoder.mlmodelc/model.mlmodel", 164_481, "ae260ff7b95d0c957c3c1f4df4dbeaa0ae6c76bacc55eb86caca8f6820d346f0"),
            ("TextDecoder.mlmodelc/weights/weight.bin", 104_122_162, "72325d42a4a4ccc8a6fa974ede6cdf2e0770685a5c4f9da94f41495b94d8d174"),
            ("config.json", 1_464, "67e25477d03bf3a1c34bfd137724beed94b5218d0457e8c1d25f70379a61d9d5"),
            ("generation_config.json", 2_762, "662a99e3db3067708549d04d5f141910eb455f0935aed0c9b06d707f44bfbcaf"),
        ]
        var files = parts.map { path, bytes, sha in
            File(repo: models, revision: modelsRevision, path: "\(folder)/\(path)", local: path, bytes: bytes, sha256: sha)
        }
        // OpenAI's tokenizer, downloaded with the model so loading needs nothing else.
        let tokenizer = "openai/whisper-base"
        let tokenizerRevision = "e37978b90ca9030d5170a5c07aadb050351a65bb"
        files += [
            ("tokenizer.json", 2_480_466, "27fc476bfe7f17299480be2273fc0608e4d5a99aba2ab5dec5374b4482d1a566"),
            ("tokenizer_config.json", 282_683, "2a4c4281cf9f51ac6ccc406fdc711a087afe6530f671fa7b80953edc498275ce"),
        ].map { path, bytes, sha in
            File(repo: tokenizer, revision: tokenizerRevision, path: path, local: "\(tokenizerFolder)/\(path)", bytes: bytes, sha256: sha)
        }
        return WhisperModel(id: folder, name: "Whisper Base", files: files)
    }()
}

/// Turns 16 kHz mono audio into text, on this device.
protocol WhisperTranscribing: Sendable {
    /// `language` is Whisper's code, as "en"; nil lets the model tell.
    func transcribe(_ samples: [Float], language: String?) async throws -> String
}

/// Fetches a model's files and loads them. The app's downloads from Hugging Face and runs
/// WhisperKit; tests use a fake.
protocol WhisperModelLoader: Sendable {
    /// The languages the models understand, as Whisper's codes.
    var languages: Set<String> { get }
    /// Puts every file of `model` into `folder`, each checked against its size and SHA-256.
    func download(_ model: WhisperModel, into folder: URL, progress: @escaping @Sendable (Double) -> Void) async throws
    /// Loads from the folder alone, never the network. Every file's size is checked; with
    /// `fullCheck` its SHA-256 too. Throws when anything is missing or changed.
    func load(_ model: WhisperModel, from folder: URL, fullCheck: Bool) async throws -> any WhisperTranscribing
}

/// The microphone, for Whisper: 16 kHz mono samples, a few times a second.
@MainActor protocol WhisperMicrophone: AnyObject {
    var isDenied: Bool { get }
    /// `ended` reports a call, Siri or a lost route stopping it; it has stopped by then.
    func start(
        samples: @escaping @MainActor ([Float]) -> Void, level: @escaping @MainActor (Float) -> Void,
        ended: @escaping @MainActor (DictationError) -> Void
    ) async throws
    func stop()
}

/// Downloaded models on this device: Application Support/Whisper/<model>, excluded from
/// backups. A model counts as installed once every file has passed its check; the check
/// records the app version it ran under, so the first load after an update checks again.
struct WhisperModelStore: Sendable {
    let root: URL
    var appVersion: String = {
        let info = Bundle.main.infoDictionary
        let short = info?["CFBundleShortVersionString"] as? String ?? ""
        let build = info?["CFBundleVersion"] as? String ?? ""
        return "\(short) (\(build))"
    }()

    static let standard = WhisperModelStore(
        root: URL.applicationSupportDirectory.appending(path: "Whisper", directoryHint: .isDirectory)
    )

    func folder(for model: WhisperModel) -> URL {
        root.appending(path: model.id, directoryHint: .isDirectory)
    }

    func isInstalled(_ model: WhisperModel) -> Bool {
        FileManager.default.fileExists(atPath: marker(for: model).path)
    }

    /// Downloads into a folder of its own, then moves it into place. A failed or cancelled
    /// download leaves nothing behind.
    func install(
        _ model: WhisperModel, with loader: any WhisperModelLoader, progress: @escaping @Sendable (Double) -> Void
    ) async throws {
        let manager = FileManager.default
        try manager.createDirectory(at: root, withIntermediateDirectories: true)
        try Self.excludeFromBackup(root)
        let staging = staging(for: model)
        try? manager.removeItem(at: staging)
        do {
            try await loader.download(model, into: staging, progress: progress)
            try Task.checkCancellation()
            let folder = folder(for: model)
            try? manager.removeItem(at: folder)
            try manager.moveItem(at: staging, to: folder)
            try Self.excludeFromBackup(folder)
            try markChecked(model)
        } catch {
            try? manager.removeItem(at: staging)
            throw error
        }
    }

    /// Removes the model and anything a stopped download left behind.
    func delete(_ model: WhisperModel) throws {
        removeLeftovers(of: model)
        let folder = folder(for: model)
        guard FileManager.default.fileExists(atPath: folder.path) else { return }
        try FileManager.default.removeItem(at: folder)
    }

    /// A download the app was closed during leaves its folder; nothing else would free it.
    func removeLeftovers(of model: WhisperModel) {
        try? FileManager.default.removeItem(at: staging(for: model))
    }

    /// The files were last checked in full under another app version.
    func needsFullCheck(_ model: WhisperModel) -> Bool {
        (try? String(contentsOf: marker(for: model), encoding: .utf8)) != appVersion
    }

    func markChecked(_ model: WhisperModel) throws {
        try Data(appVersion.utf8).write(to: marker(for: model))
    }

    private func staging(for model: WhisperModel) -> URL {
        root.appending(path: "\(model.id).download", directoryHint: .isDirectory)
    }

    private func marker(for model: WhisperModel) -> URL {
        folder(for: model).appending(path: ".verified")
    }

    private static func excludeFromBackup(_ url: URL) throws {
        var url = url
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try url.setResourceValues(values)
    }
}
