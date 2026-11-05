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
    static let sendingImage = String(localized: "compose.sendingImage", defaultValue: "Sending image")
    static let uploadFailed = String(localized: "compose.uploadFailed", defaultValue: "Couldn\u{2019}t send the image.")
    static let tooLarge = String(localized: "compose.tooLarge", defaultValue: "This image is too large to send.")
    static let unsupported = String(
        localized: "compose.unsupported", defaultValue: "This machine\u{2019}s helper can\u{2019}t receive images yet."
    )
    static let noHelper = String(
        localized: "compose.noHelper", defaultValue: "This machine has no Togo helper, so images can\u{2019}t be sent."
    )
    static let unreadable = String(localized: "compose.unreadable", defaultValue: "The image could not be read.")
    static let loadFailed = String(localized: "compose.loadFailed", defaultValue: "Couldn\u{2019}t load that photo.")
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
    var text: String
    private(set) var history: [String]
    let images: ImagePasteFlow
    private let store: ComposeHistory
    private let paste: @MainActor (String) -> Void
    private let pressEnter: @MainActor () -> Void

    init(
        text: String = "", history: ComposeHistory, images: ImagePasteFlow,
        paste: @escaping @MainActor (String) -> Void, pressEnter: @escaping @MainActor () -> Void
    ) {
        self.text = text
        self.store = history
        self.history = history.load()
        self.images = images
        self.paste = paste
        self.pressEnter = pressEnter
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
    }

    func recall(_ entry: String) {
        text = entry
    }
}

/// A multi-line field above the key bar for writing a whole message before it reaches the
/// session, plus image attach when the machine's helper accepts uploads.
struct ComposeBar: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var sendDiameter: CGFloat = 32
    @State private var model: ComposeModel
    @State private var photo: PhotosPickerItem?
    private let focusOnAppear: Bool

    init(
        hostID: HostID, sessionID: String, imageSupport: ImageSupport,
        paste: @escaping @MainActor (String) -> Void, pressEnter: @escaping @MainActor () -> Void,
        upload: @escaping ImagePasteFlow.Upload
    ) {
        let images = ImagePasteFlow(support: imageSupport, upload: upload, paste: paste)
        let model = ComposeModel(
            history: ComposeHistory(hostID: hostID, sessionID: sessionID), images: images,
            paste: paste, pressEnter: pressEnter
        )
        self.init(model: model, focusOnAppear: true)
    }

    init(
        engine: any TerminalEngine, hostID: HostID, sessionID: String, imageSupport: ImageSupport,
        upload: @escaping ImagePasteFlow.Upload
    ) {
        self.init(
            hostID: hostID, sessionID: sessionID, imageSupport: imageSupport,
            paste: { [weak engine] in engine?.paste($0) },
            pressEnter: { [weak engine] in engine?.send(KeyStroke(.enter)) },
            upload: upload
        )
    }

    init(model: ComposeModel, focusOnAppear: Bool) {
        _model = State(initialValue: model)
        self.focusOnAppear = focusOnAppear
    }

    var body: some View {
        VStack(spacing: 0) {
            Rectangle()
                .fill(Color.togoLine)
                .frame(height: 0.5)
            ImageStatusRow(flow: model.images)
            if dynamicTypeSize.isAccessibilitySize {
                VStack(spacing: 4) {
                    field
                    HStack(spacing: 4) {
                        photoButton
                        historyMenu
                        Spacer(minLength: 0)
                        sendButton
                    }
                }
                .padding(8)
            } else {
                HStack(alignment: .bottom, spacing: 4) {
                    photoButton
                    field
                    historyMenu
                    sendButton
                }
                .padding(.horizontal, 6)
                .padding(.vertical, 6)
            }
        }
        .background(Color.togoInk)
        .motion(Motion.fade, value: model.images.phase)
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
            text: $model.text, focusOnAppear: focusOnAppear,
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

    @ViewBuilder private var photoButton: some View {
        if model.images.showsPhotoButton {
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

    @ViewBuilder private var historyMenu: some View {
        if !model.history.isEmpty {
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
        .motion(Motion.confirm, value: model.canSend)
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

    var body: some View {
        switch flow.phase {
        case .idle, .annotating:
            EmptyView()
        case .uploading:
            PawWalk(ComposeCopy.sendingImage)
                .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                .padding(.horizontal, 16)
        case .failed(let detail):
            row(ComposeCopy.uploadFailed, detail: detail)
        case .loadFailed, .unreadable, .tooLarge, .unsupported, .noHelper:
            row(flow.phase.message ?? "", detail: nil)
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
