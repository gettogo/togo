import Observation
import PhotosUI
import SwiftUI
import TogoTerminal
import UIKit

/// Every sentence the terminal's input tools and image paste show, keyed under `compose.` in
/// the string catalog.
enum ComposeCopy {
    static let send = String(localized: "compose.send", defaultValue: "Send")
    static let dismiss = String(localized: "compose.dismiss", defaultValue: "Dismiss")
    static let tryAgain = String(localized: "compose.tryAgain", defaultValue: "Try again")
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
    static let imageSent = String(
        localized: "compose.imageSent", defaultValue: "Image sent. Its path is typed in the terminal."
    )
}

/// One tab's tools beside typing straight into its terminal: dictation and pictures. The
/// terminal has no composer; each tool ends by typing into the terminal as a paste, never with
/// Enter, so the session's own prompt holds the words until the user sends them there.
@Observable @MainActor final class TerminalInputModel {
    let images: ImagePasteFlow
    /// Nil when there is nothing to listen with.
    private(set) var dictation: DictationModel?
    @ObservationIgnored private let paste: @MainActor (String) -> Void

    init(images: ImagePasteFlow, paste: @escaping @MainActor (String) -> Void, speech: DictationSpeech? = nil) {
        self.images = images
        self.paste = paste
        if let speech {
            dictation = DictationModel(
                engine: speech.engine, locale: speech.locale, history: speech.history,
                insert: { [weak self] in self?.type($0) }, clock: speech.clock
            )
        }
    }

    /// Dictated words go to the terminal as a paste, cleaned by `PasteText`: control
    /// characters never reach the session, and Enter is never pressed: a line break the
    /// words carry (a recogniser's "new line", a trailing return) becomes a space, and
    /// words that are only space type nothing.
    func type(_ words: String) {
        let clean = String(PasteText.sanitize(words).map { $0.isNewline ? " " : $0 })
            .trimmingCharacters(in: .whitespaces)
        guard !clean.isEmpty else { return }
        paste(clean)
    }
}

/// What a terminal's dictation speaks with.
struct DictationSpeech {
    var engine: any SpeechEngine
    var locale: Locale
    var history: DictationHistory
    var clock: DictationClock = .system
}

/// What the terminal's input tools are doing, over the foot of the terminal while they do it:
/// the words dictation hears, a picture on its way or why it could not go. It takes no typing,
/// so it is not a composer: the terminal keeps the keyboard. With nothing to say it is empty
/// and takes no room or touches.
struct TerminalInputStatus: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.colorScheme) private var colorScheme
    let model: TerminalInputModel
    /// The rows as they show (see `ComposeShape`).
    @State private var shown = ComposeShape()

    static let radius: CGFloat = 22

    var body: some View {
        VStack(spacing: 0) {
            if let dictation = model.dictation, shown.stage.dictation != 0 {
                // One row whose words change in place from stage to stage: no re-identity, no rise.
                DictationLine(model: dictation, phase: shown.dictation)
                    .padding(.bottom, 8)
                    // motion: state change — the dictation row arrives by opacity as the status makes room.
                    .transition(Motion.arrive(reduce: reduceMotion))
            }
            if shown.stage.image != 0 {
                ImageStatusRow(flow: model.images, phase: shown.image)
                    // motion: state change — the picture's status row arrives by opacity; its words then change in place.
                    .transition(Motion.arrive(reduce: reduceMotion))
            }
        }
        .frame(maxWidth: .infinity)
        // Glass only while there is something to say: an empty status draws nothing.
        .clipShape(.rect(cornerRadius: Self.radius, style: .continuous))
        .glassEffect(glass, in: .rect(cornerRadius: Self.radius, style: .continuous))
        .opacity(isEmpty ? 0 : 1)
        .allowsHitTesting(!isEmpty)
        .onChange(of: live, initial: true) { old, new in
            guard old != new else { return shown = new }
            ComposeShape.follow($shown, to: new, reduce: reduceMotion)
        }
        .onChange(of: reduceMotion, initial: true) { model.dictation?.showsLevel = !reduceMotion }
        // motion: state change — the start of listening is felt, as the Dictate key was pressed.
        .haptic(.listen, trigger: model.dictation?.isListening ?? false) { $0 }
        .onDisappear {
            // Leaving the screen ends a dictation; what was heard is still typed.
            model.dictation?.relinquish()
        }
        .presentCover("compose.annotate", item: annotating) { pending in
            AnnotateView(image: pending.preview, onCancel: model.images.dismiss) { annotated in
                Task { await model.images.send(annotated: annotated) }
            }
        }
    }

    private var isEmpty: Bool { shown.stage.dictation == 0 && shown.stage.image == 0 }

    /// The rows the model calls for now.
    private var live: ComposeShape {
        ComposeShape(image: model.images.phase, dictation: model.dictation?.phase)
    }

    private var annotating: Binding<PendingImage?> {
        Binding(
            get: { model.images.annotating },
            set: { if $0 == nil, model.images.isAnnotating { model.images.dismiss() } }
        )
    }

    /// The prompt bar's glass, so the status reads as the same layer.
    private var glass: Glass {
        colorScheme == .dark
            ? .regular.tint(Color.togoGrouped.opacity(0.5))
            : .regular.tint(Color.togoRaised.opacity(0.6))
    }
}

/// What is happening to an attached image, above the field. At accessibility sizes the
/// actions move below the text so neither has to wrap word by word.
struct ImageStatusRow: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let flow: ImagePasteFlow
    /// The phase as the status shows it, which leads or trails the model's by a transaction.
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
        case .sent:
            HStack(spacing: 8) {
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(Color.togoSnow)
                    .accessibilityHidden(true)
                Text(ComposeCopy.imageSent)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoSnow)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(minHeight: 44)
            .padding(.horizontal, 16)
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
                .contentShape(.rect)
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
