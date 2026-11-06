import Foundation
import WhisperKit

/// Turns checked files on disk into a running model. Never fetches anything.
protocol WhisperRuntime: Sendable {
    /// Throws if the tokenizer in `tokenizer` or the models in `models` do not load.
    func load(models: URL, tokenizer: URL) async throws -> any WhisperTranscribing
}

/// WhisperKit, given its tokenizer up front. Left to itself WhisperKit looks for a tokenizer
/// and, when the local one fails, fetches one from Hugging Face into the model's folder. With
/// a tokenizer already set it skips that step entirely, so loading reads only the folder.
struct WhisperKitRuntime: WhisperRuntime {
    /// A tokenizer that will not load counts as a damaged model; a model that will not load
    /// (as under memory pressure) is reported as it is.
    func load(models: URL, tokenizer folder: URL) async throws -> any WhisperTranscribing {
        let tokenizer: VerifiedTokenizer
        do {
            tokenizer = try await VerifiedTokenizer(folder: folder)
        } catch {
            throw WhisperLoadError.damaged
        }
        return try await WhisperRunner(folder: models, tokenizer: tokenizer)
    }

    /// How dictation decodes: in the chosen language when there is one, otherwise the model
    /// tells; plain text, no timestamps.
    static func options(language: String?) -> DecodingOptions {
        DecodingOptions(
            task: .transcribe, language: language, usePrefillPrompt: language != nil,
            detectLanguage: language == nil, skipSpecialTokens: true, withoutTimestamps: true
        )
    }

    /// WhisperKit marks a model multilingual only while it finds a tokenizer itself, which a
    /// tokenizer given up front skips. Without the mark the decoder leaves out the language
    /// and task, so this sets it by WhisperKit's own rule: only the English-only models have
    /// 51,864 logits.
    static func markLanguages(_ decoder: inout some LanguageMarked) {
        decoder.isModelMultilingual = decoder.logitsSize != 51_864
    }
}

/// The two things about a text decoder that decide whether it is told the language.
protocol LanguageMarked {
    var logitsSize: Int? { get }
    var isModelMultilingual: Bool { get set }
}

/// WhisperKit's decoder, as `LanguageMarked`.
private struct KitDecoder: LanguageMarked {
    let kit: WhisperKit

    var logitsSize: Int? { kit.textDecoder.logitsSize }

    var isModelMultilingual: Bool {
        get { kit.textDecoder.isModelMultilingual }
        nonmutating set { kit.textDecoder.isModelMultilingual = newValue }
    }
}

/// One loaded model. WhisperKit is not thread-safe, so calls queue up and run one at a time,
/// even across the actor's suspension points.
private actor WhisperRunner: WhisperTranscribing {
    private let kit: WhisperKitBox
    private var tail: Task<Void, Never>?

    init(folder: URL, tokenizer: VerifiedTokenizer) async throws {
        let config = WhisperKitConfig(
            modelFolder: folder.path, tokenizerFolder: folder, verbose: false, logLevel: .none,
            prewarm: false, load: false, download: false
        )
        let kit = try await WhisperKit(config)
        kit.tokenizer = tokenizer
        try await kit.loadModels()
        var decoder = KitDecoder(kit: kit)
        WhisperKitRuntime.markLanguages(&decoder)
        self.kit = WhisperKitBox(kit)
    }

    func transcribe(_ samples: [Float], language: String?) async throws -> String {
        let options = WhisperKitRuntime.options(language: language)
        let previous = tail
        let job = Task { [kit] in
            await previous?.value
            let results = try await kit.value.transcribe(audioArray: samples, decodeOptions: options)
            return results.map(\.text).joined(separator: " ")
        }
        tail = Task { _ = try? await job.value }
        return try await job.value
    }
}

/// WhisperKit is a class from before strict concurrency. Only `WhisperRunner` touches it, one
/// call at a time.
private final class WhisperKitBox: @unchecked Sendable {
    let value: WhisperKit

    init(_ value: WhisperKit) {
        self.value = value
    }
}

/// Whisper's tokenizer, read from the checked files and nowhere else. The special tokens and
/// their fallbacks are the ones WhisperKit's own wrapper uses.
struct VerifiedTokenizer: WhisperTokenizer, Sendable {
    let specialTokens: SpecialTokens
    let allLanguageTokens: Set<Int>
    private let base: TokenizerWrapper

    init(folder: URL) async throws {
        let manager = FileManager.default
        guard manager.fileExists(atPath: folder.appending(path: "tokenizer.json").path),
              manager.fileExists(atPath: folder.appending(path: "tokenizer_config.json").path) else {
            throw WhisperLoadError.damaged
        }
        let base = try await AutoTokenizerWrapper.from(modelFolder: folder)
        func id(_ token: String, _ fallback: Int) -> Int { base.convertTokenToId(token) ?? fallback }
        let special = SpecialTokens(
            endToken: id("<|endoftext|>", 50_257), englishToken: id("<|en|>", 50_259),
            noSpeechToken: id("<|nospeech|>", 50_362), noTimestampsToken: id("<|notimestamps|>", 50_363),
            specialTokenBegin: id("<|endoftext|>", 50_257), startOfPreviousToken: id("<|startofprev|>", 50_361),
            startOfTranscriptToken: id("<|startoftranscript|>", 50_258), timeTokenBegin: id("<|0.00|>", 50_364),
            transcribeToken: id("<|transcribe|>", 50_359), translateToken: id("<|translate|>", 50_358),
            whitespaceToken: id(" ", 220)
        )
        self.base = base
        specialTokens = special
        allLanguageTokens = Set(
            Constants.languages.values
                .compactMap { base.convertTokenToId("<|\($0)|>") }
                .filter { $0 > special.specialTokenBegin }
        )
    }

    func encode(text: String) -> [Int] { base.encode(text: text) }
    func decode(tokens: [Int]) -> String { base.decode(tokens: tokens) }
    func convertTokenToId(_ token: String) -> Int? { base.convertTokenToId(token) }
    func convertIdToToken(_ id: Int) -> String? { base.convertIdToToken(id) }

    /// Only word timestamps use this, and dictation asks for none; words start at a space.
    func splitToWordTokens(tokenIds: [Int]) -> (words: [String], wordTokens: [[Int]]) {
        var words: [String] = []
        var groups: [[Int]] = []
        for token in tokenIds where token < specialTokens.specialTokenBegin {
            let piece = base.decode(tokens: [token])
            if piece.hasPrefix(" ") || groups.isEmpty {
                words.append(piece)
                groups.append([token])
            } else {
                words[words.count - 1] += piece
                groups[groups.count - 1].append(token)
            }
        }
        return (words, groups)
    }
}
