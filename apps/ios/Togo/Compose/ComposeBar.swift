import Observation
import PhotosUI
import SwiftUI
import TogoTerminal
import UIKit

/// Every sentence the compose bar and image paste show, keyed under `compose.` in the string catalog.
enum ComposeCopy {
    static let placeholder = String(localized: "compose.placeholder", defaultValue: "Message")
    static let send = String(localized: "compose.send", defaultValue: "Send")
    static let history = String(localized: "compose.history", defaultValue: "Recent messages")
    static let photo = String(localized: "compose.photo", defaultValue: "Attach image")
    static let dismiss = String(localized: "compose.dismiss", defaultValue: "Dismiss")
    static let tryAgain = String(localized: "compose.tryAgain", defaultValue: "Try again")
    static func to(_ bib: String) -> String {
        String(localized: "compose.to", defaultValue: "To \(bib)")
    }
    static let sendingImage = String(localized: "compose.sendingImage", defaultValue: "Sending image")
    static let uploadFailed = String(localized: "compose.uploadFailed", defaultValue: "Couldn't send the image.")
    static let tooLarge = String(localized: "compose.tooLarge", defaultValue: "This image is too large to send.")
    static let unsupported = String(
        localized: "compose.unsupported", defaultValue: "This machine's helper can't receive images yet."
    )
    static let noHelper = String(
        localized: "compose.noHelper", defaultValue: "This machine has no Togo helper, so images can't be sent."
    )
    static let unreadable = String(localized: "compose.unreadable", defaultValue: "The image could not be read.")
    static let loadFailed = String(localized: "compose.loadFailed", defaultValue: "Couldn't load that photo.")
}

/// The last sends for one session, newest first. Kept on this device only.
struct ComposeHistory {
    static let limit = 20
    private let key: String
    private let defaults: UserDefaults

    init(hostID: HostID, sessionID: String, defaults: UserDefaults = .standard) {
        // Session ids are only unique within one machine.
        key = "compose.history.\(hostID.uuidString).\(sessionID)"
        self.defaults = defaults
    }

    func load() -> [String] {
        defaults.stringArray(forKey: key) ?? []
    }

    func save(_ entries: [String]) {
        defaults.set(Array(entries.prefix(Self.limit)), forKey: key)
    }
}

@Observable @MainActor final class ComposeModel {
    /// Reads and writes the draft the model was given, so the text outlives the bar.
    var text: String {
        get {
            access(keyPath: \.text)
            return draft?.wrappedValue ?? localText
        }
        set {
            withMutation(keyPath: \.text) {
                if let draft { draft.wrappedValue = newValue } else { localText = newValue }
            }
        }
    }
    @ObservationIgnored private var localText: String
    @ObservationIgnored private let draft: Binding<String>?
    private(set) var history: [String]
    /// The caret in the field, in UTF-16 units; nil puts dictated text at the end.
    var selection: NSRange?
    let images: ImagePasteFlow
    /// Nil hides the microphone.
    private(set) var dictation: DictationModel?
    private let store: ComposeHistory
    private let paste: @MainActor (String) -> Void
    private let pressEnter: @MainActor () -> Void

    init(
        text: String = "", draft: Binding<String>? = nil, history: ComposeHistory, images: ImagePasteFlow,
        paste: @escaping @MainActor (String) -> Void, pressEnter: @escaping @MainActor () -> Void,
        speech: DictationSpeech? = nil
    ) {
        self.localText = text
        self.draft = draft
        self.store = history
        self.history = history.load()
        self.images = images
        self.paste = paste
        self.pressEnter = pressEnter
        if let speech {
            dictation = DictationModel(
                engine: speech.engine, locale: speech.locale, history: speech.history,
                insert: { [weak self] in self?.insertDictated($0) }, clock: speech.clock
            )
        }
    }

    var canSend: Bool {
        !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    /// Pasted, so with bracketed paste on the agent receives every line as one message;
    /// Enter then submits it.
    func send() {
        guard canSend else { return }
        var message = text
        while message.last?.isWhitespace == true { message.removeLast() }
        paste(message)
        pressEnter()
        history = Array(([message] + history.filter { $0 != message }).prefix(ComposeHistory.limit))
        store.save(history)
        text = ""
        selection = nil
    }

    func recall(_ entry: String) {
        text = entry
        selection = nil
    }

    /// Dictated words go in at the caret, after a space when they would run into a word.
    /// They only ever land in the field: nothing is pasted and Enter is never pressed.
    func insertDictated(_ words: String) {
        let clean = PasteText.sanitize(words)
        guard !clean.isEmpty else { return }
        let current = text as NSString
        var range = selection ?? NSRange(location: current.length, length: 0)
        range.location = min(max(range.location, 0), current.length)
        range.length = min(max(range.length, 0), current.length - range.location)
        let before = current.substring(to: range.location)
        let piece = DictationModel.needsSpace(between: before, and: clean) ? " " + clean : clean
        text = current.replacingCharacters(in: range, with: piece)
        selection = NSRange(location: range.location + (piece as NSString).length, length: 0)
    }
}

/// What a compose bar dictates with.
struct DictationSpeech {
    var engine: any SpeechEngine
    var locale: Locale
    var history: DictationHistory
    var clock: DictationClock = .system
}

/// A multi-line field above the key bar for writing a whole message before it reaches the
/// session, plus image attach when the machine's helper accepts uploads.
struct ComposeBar: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var sendDiameter: CGFloat = 32
    @State private var model: ComposeModel
    @State private var photo: PhotosPickerItem?
    /// The rows and buttons around the field as they show (see `ComposeShape`).
    @State private var shown = ComposeShape()
    private let focusOnAppear: Bool
    /// The bib of the session the text goes to, shown as "To 04"; nil hides the label.
    private let target: String?

    init(
        hostID: HostID, sessionID: String, imageSupport: ImageSupport, draft: Binding<String>,
        target: String? = nil,
        paste: @escaping @MainActor (String) -> Void, pressEnter: @escaping @MainActor () -> Void,
        upload: @escaping ImagePasteFlow.Upload
    ) {
        let images = ImagePasteFlow(support: imageSupport, upload: upload, paste: paste)
        let model = ComposeModel(
            draft: draft, history: ComposeHistory(hostID: hostID, sessionID: sessionID), images: images,
            paste: paste, pressEnter: pressEnter
        )
        self.init(model: model, focusOnAppear: true, target: target)
    }

    init(
        engine: any TerminalEngine, hostID: HostID, sessionID: String, imageSupport: ImageSupport,
        draft: Binding<String>, target: String? = nil, upload: @escaping ImagePasteFlow.Upload
    ) {
        self.init(
            hostID: hostID, sessionID: sessionID, imageSupport: imageSupport, draft: draft, target: target,
            paste: { [weak engine] in engine?.paste($0) },
            pressEnter: { [weak engine] in engine?.send(KeyStroke(.enter)) },
            upload: upload
        )
    }

    init(model: ComposeModel, focusOnAppear: Bool, target: String? = nil) {
        _model = State(initialValue: model)
        self.focusOnAppear = focusOnAppear
        self.target = target
    }

    var body: some View {
        VStack(spacing: 0) {
            Rectangle()
                .fill(Color.togoLine)
                .frame(height: 0.5)
            if let target {
                Text(ComposeCopy.to(target))
                    .font(.caption.weight(.medium))
                    .foregroundStyle(Color.togoMute)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 12)
                    .padding(.top, 6)
            }
            if let dictation = model.dictation, shown.stage.dictation != 0 {
                // One row whose words change in place from stage to stage: no re-identity, no rise.
                DictationLine(model: dictation, phase: shown.dictation)
                    // motion: state change — the dictation row arrives by opacity as the bar makes room.
                    .transition(Motion.arrive(reduce: reduceMotion))
            }
            if shown.stage.image != 0 {
                ImageStatusRow(flow: model.images, phase: shown.image)
                    // motion: state change — the picture's status row arrives by opacity; its words then change in place.
                    .transition(Motion.arrive(reduce: reduceMotion))
            }
            if dynamicTypeSize.isAccessibilitySize {
                VStack(spacing: 4) {
                    field
                    HStack(spacing: 4) {
                        photoButton
                        historyMenu
                        Spacer(minLength: 0)
                        dictationButton
                        sendButton
                    }
                }
                .padding(8)
            } else {
                HStack(alignment: .bottom, spacing: 4) {
                    photoButton
                    field
                    historyMenu
                    dictationButton
                    sendButton
                }
                .padding(.horizontal, 6)
                .padding(.vertical, 6)
            }
        }
        .background(Color.togoInk)
        .clipped()
        .onChange(of: live, initial: true) { old, new in
            guard old != new else { return shown = new }
            ComposeShape.follow($shown, to: new, reduce: reduceMotion)
        }
        .onDisappear {
            // Leaving the bar ends a dictation; what was heard stays in the draft.
            model.dictation?.relinquish()
        }
        .onChange(of: photo) { _, item in
            guard let item else { return }
            photo = nil
            Task { await model.images.load { try await item.loadTransferable(type: Data.self) } }
        }
        .fullScreenCover(item: annotating) { pending in
            AnnotateView(image: pending.preview, onCancel: model.images.dismiss) { annotated in
                Task { await model.images.send(annotated: annotated) }
            }
        }
    }

    private var annotating: Binding<PendingImage?> {
        Binding(
            get: { model.images.annotating },
            set: { if $0 == nil, model.images.isAnnotating { model.images.dismiss() } }
        )
    }

    private var field: some View {
        ComposeTextField(
            text: $model.text, selection: $model.selection, focusOnAppear: focusOnAppear,
            onPasteImage: { data in Task { await model.images.begin(data) } }, onSubmit: model.send
        )
        .frame(maxWidth: .infinity)
        .clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .strokeBorder(Color.togoLine)
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder private var dictationButton: some View {
        if let dictation = model.dictation {
            DictationButton(model: dictation)
        }
    }

    /// The rows and buttons the model calls for now.
    private var live: ComposeShape {
        ComposeShape(
            image: model.images.phase, dictation: model.dictation?.phase,
            photo: model.images.showsPhotoButton, history: !model.history.isEmpty
        )
    }

    /// The photo button and the history menu come and go with the bar's `resize`.
    @ViewBuilder private var photoButton: some View {
        if shown.photo {
            Group {
                if model.images.support == .available {
                    PhotosPicker(selection: $photo, matching: .images) {
                        BarIcon(systemName: "photo")
                    }
                    .accessibilityLabel(ComposeCopy.photo)
                } else {
                    Button(action: model.images.explainUnsupported) {
                        BarIcon(systemName: "photo")
                    }
                    .accessibilityLabel(ComposeCopy.photo)
                }
            }
        }
    }

    @ViewBuilder private var historyMenu: some View {
        if shown.history {
            Menu {
                ForEach(model.history, id: \.self) { entry in
                    Button(entry) { model.recall(entry) }
                }
            } label: {
                BarIcon(systemName: "clock.arrow.circlepath")
            }
            .fixedSize()
            .accessibilityLabel(ComposeCopy.history)
        }
    }

    private var sendButton: some View {
        Button(action: model.send) {
            Image(systemName: "arrow.up")
                .font(.body.weight(.semibold))
                .foregroundStyle(model.canSend ? Color.togoInk : Color.togoMute)
                .frame(width: sendDiameter, height: sendDiameter)
                .background(Circle().fill(model.canSend ? Color.togoSnow : Color.togoLine))
                .frame(minWidth: 44, minHeight: 44)
                .fixedSize()
                .contentShape(Rectangle())
        }
        .disabled(!model.canSend)
        .accessibilityLabel(ComposeCopy.send)
        // motion: state change — Send lights as there is something to send.
        .motion(.respond, value: model.canSend)
    }
}

private struct BarIcon: View {
    let systemName: String

    var body: some View {
        Image(systemName: systemName)
            .font(.body)
            .foregroundStyle(Color.togoMute)
            .frame(minWidth: 44, minHeight: 44)
            // Without this the minimum frame stretches and takes width from the text field.
            .fixedSize()
            .contentShape(Rectangle())
    }
}

/// What is happening to an attached image, above the field. At accessibility sizes the
/// actions move below the text so neither has to wrap word by word.
private struct ImageStatusRow: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let flow: ImagePasteFlow
    /// The phase as the bar shows it, which leads or trails the model's by a transaction.
    let phase: ImagePasteFlow.Phase

    var body: some View {
        switch phase {
        case .idle, .annotating:
            EmptyView()
        case .uploading:
            PawWalk(ComposeCopy.sendingImage)
                .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                .padding(.horizontal, 16)
        case .failed(let detail):
            row(ComposeCopy.uploadFailed, detail: detail)
        case .loadFailed, .unreadable, .tooLarge, .unsupported, .noHelper:
            row(phase.message ?? "", detail: nil)
        }
    }

    @ViewBuilder private func row(_ title: String, detail: String?) -> some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: 4) {
                message(title, detail: detail)
                HStack(spacing: 12) {
                    retryButton
                    Spacer(minLength: 0)
                    dismissButton
                }
            }
            .padding(.leading, 16)
            .padding(.trailing, 4)
            .padding(.vertical, 4)
        } else {
            HStack(alignment: .center, spacing: 12) {
                message(title, detail: detail)
                retryButton
                dismissButton
            }
            .padding(.leading, 16)
            .padding(.trailing, 4)
            .padding(.vertical, 4)
        }
    }

    private func message(_ title: String, detail: String?) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
                .font(.subheadline)
                .foregroundStyle(Color.togoSnow)
            if let detail {
                Text(detail)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private var retryButton: some View {
        if flow.canRetry {
            Button(ComposeCopy.tryAgain) { Task { await flow.retry() } }
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .frame(minHeight: 44)
                .fixedSize()
        }
    }

    private var dismissButton: some View {
        Button(action: flow.dismiss) {
            Image(systemName: "xmark")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.togoMute)
                .frame(minWidth: 44, minHeight: 44)
                .fixedSize()
                .contentShape(Rectangle())
        }
        .accessibilityLabel(ComposeCopy.dismiss)
    }
}
