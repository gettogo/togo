import SwiftUI
import TogoKit
import UIKit
import UniformTypeIdentifiers

/// What one pasted item is to the composer.
enum ComposerPasteKind: Equatable {
    /// Words, links and code: pasted into the field as they are.
    case text
    /// A screenshot, a photo or an image file: a chip above the field, uploaded like a picked
    /// photo.
    case image
    /// Any other file (a PDF, a log, an archive): uploaded, and its path typed into the prompt.
    case file
}

/// Decides what a paste is from its type identifiers, before anything is read.
enum ComposerPasteRoute {
    /// Plain text decides first: a selection from Notes with a picture in it, or a link with a
    /// preview, is still words. Then an image, as copied from Photos or a screenshot (a web
    /// image also carries its address, which does not make it words). Then other text, such as
    /// HTML or JSON alone. A link alone pastes as its address. Any other data is a file.
    static func kind(_ identifiers: [String]) -> ComposerPasteKind {
        let types = identifiers.compactMap(UTType.init)
        if types.contains(where: { $0.conforms(to: .plainText) }) { return .text }
        if types.contains(where: { $0.conforms(to: .image) }) { return .image }
        if types.contains(where: { $0.conforms(to: .text) || $0.conforms(to: .url) }) { return .text }
        if types.contains(where: { $0.conforms(to: .data) || $0.conforms(to: .fileURL) }) { return .file }
        return .text
    }

    /// The image's own encoding when it has one the shrinker reads, so a JPEG is not first
    /// turned into a much larger PNG.
    static func imageType(_ identifiers: [String]) -> String? {
        let types = identifiers.compactMap(UTType.init)
        for preferred in [UTType.heic, .jpeg, .png] where types.contains(where: { $0 == preferred }) {
            return preferred.identifier
        }
        return types.first { $0.conforms(to: .image) }?.identifier
    }

    /// "notes.pdf": the name the source gave, with its type's extension when it lacks one.
    static func fileName(_ suggested: String?, type: String) -> String {
        let name = suggested.flatMap { $0.isEmpty ? nil : $0 } ?? "file"
        guard let ext = UTType(type)?.preferredFilenameExtension, !name.lowercased().hasSuffix("." + ext.lowercased()) else {
            return name
        }
        return "\(name).\(ext)"
    }

    /// The type a file's bytes are read as: its own, never a link to it.
    static func fileType(_ identifiers: [String]) -> String? {
        identifiers.compactMap(UTType.init).first { $0.conforms(to: .data) && !$0.conforms(to: .url) }?.identifier
    }

    /// The text typed for an uploaded file: its path, quoted only when the shell would need it,
    /// with a space before when the draft does not end in one and a space after to keep typing.
    static func pathInsert(_ path: String, after draft: String) -> String {
        let quoted = path.unicodeScalars.allSatisfy(plainPath.contains) ? path : shellQuote([path])
        let lead = draft.isEmpty || draft.last?.isWhitespace == true ? "" : " "
        return lead + quoted + " "
    }

    /// Characters a path can hold and still read as one word without quotes.
    private static let plainPath = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "/._-~+@%:,="))

    /// The types the field takes, added to its own: images always, any file where the machine
    /// takes uploads.
    static func accepted(files: Bool) -> [String] {
        var types = [UTType.image.identifier]
        if files { types.append(UTType.data.identifier) }
        return types
    }
}

/// The composer field's paste: text as UIKit pastes it (the field's own delegate first, when
/// SwiftUI set one), pictures into the attachment strip, other files uploaded with their path
/// typed in. Installed on the field's text view when it takes focus (`ComposerPasteHook`).
/// Every other paste step is the previous delegate's or UIKit's own, so text lands exactly as
/// it would without this.
@MainActor final class ComposerPasteDelegate: NSObject, UITextPasteDelegate {
    /// The pictures go to the strip: `ComposerAttachments.add`.
    var onImages: (@MainActor @Sendable ([Data]) -> Void)?
    /// A file's name and bytes, for upload; nil where the machine takes no uploads.
    var onFile: (@MainActor @Sendable (String, Data) -> Void)?
    /// Said when a picture or file cannot go anywhere.
    var onRefused: (@MainActor () -> Void)?
    /// SwiftUI's own paste delegate on the field, if it had one. Read by the Objective-C
    /// runtime's forwarding, which is not actor-aware; it is only set and used on the main thread.
    nonisolated(unsafe) weak var previous: (any UITextPasteDelegate)?

    func textPasteConfigurationSupporting(
        _ textPasteConfigurationSupporting: any UITextPasteConfigurationSupporting, transform item: any UITextPasteItem
    ) {
        let provider = item.itemProvider
        let identifiers = provider.registeredTypeIdentifiers
        switch ComposerPasteRoute.kind(identifiers) {
        case .text:
            if let previous, previous.responds(to: #selector(UITextPasteDelegate.textPasteConfigurationSupporting(_:transform:))) {
                previous.textPasteConfigurationSupporting?(textPasteConfigurationSupporting, transform: item)
            } else {
                item.setDefaultResult()
            }
        case .image:
            if let onImages, let type = ComposerPasteRoute.imageType(identifiers) {
                provider.loadDataRepresentation(forTypeIdentifier: type) { data, _ in
                    guard let data else { return }
                    Task { @MainActor in onImages([data]) }
                }
            } else {
                onRefused?()
            }
            item.setNoResult()
        case .file:
            if let onFile, let type = ComposerPasteRoute.fileType(identifiers) {
                let fileName = ComposerPasteRoute.fileName(provider.suggestedName, type: type)
                provider.loadDataRepresentation(forTypeIdentifier: type) { data, _ in
                    guard let data else { return }
                    Task { @MainActor in onFile(fileName, data) }
                }
            } else {
                onRefused?()
            }
            item.setNoResult()
        }
    }

    // The steps this class leaves alone go to the previous delegate when it has them.

    override nonisolated func responds(to aSelector: Selector!) -> Bool {
        super.responds(to: aSelector) || (previous?.responds(to: aSelector) ?? false)
    }

    override nonisolated func forwardingTarget(for aSelector: Selector!) -> Any? {
        previous?.responds(to: aSelector) == true ? previous : super.forwardingTarget(for: aSelector)
    }
}

/// Puts `ComposerPasteDelegate` on the composer's text view once the field has focus, and
/// stops the keyboard's smart quotes and dashes there at the source. SwiftUI's `TextField`
/// gives no paste hook on iOS 26, so the field's text view is reached as the first responder.
@MainActor enum ComposerPasteHook {
    static func install(_ delegate: ComposerPasteDelegate, files: Bool) {
        guard let field = UIResponder.togoCurrent as? UITextView, field.isEditable else { return }
        if field.pasteDelegate !== delegate {
            if !(field.pasteDelegate is ComposerPasteDelegate) { delegate.previous = field.pasteDelegate }
            field.pasteDelegate = delegate
        }
        let configuration = field.pasteConfiguration ?? UIPasteConfiguration()
        let missing = ComposerPasteRoute.accepted(files: files)
            .filter { !configuration.acceptableTypeIdentifiers.contains($0) }
        if !missing.isEmpty {
            if configuration.acceptableTypeIdentifiers.isEmpty { configuration.addTypeIdentifiers(forAccepting: NSString.self) }
            configuration.addAcceptableTypeIdentifiers(missing)
            field.pasteConfiguration = configuration
        }
        // Prompts carry commands and paths: quotes stay straight and `--` stays two hyphens.
        field.smartQuotesType = .no
        field.smartDashesType = .no
    }
}

extension ComposerAttachments {
    /// The largest file the helper takes in one upload (its `MaxBytes`).
    static let fileLimit = 700_000
}

extension CommandLineModel {
    /// The paste delegate's ends: pictures to the strip, files uploaded with their path typed
    /// at the end of the draft.
    func pasteDelegate(_ delegate: ComposerPasteDelegate) {
        let attachments = attachments
        delegate.onRefused = { attachments.refuse() }
        guard attachments.available else {
            delegate.onImages = nil
            delegate.onFile = nil
            return
        }
        delegate.onImages = { @MainActor @Sendable (datas: [Data]) in
            Task { await attachments.add(datas) }
        }
        delegate.onFile = { @MainActor @Sendable [weak self] (name: String, data: Data) in
            Task { @MainActor [weak self] in
                guard let path = await attachments.upload(file: name, data: data), let self else { return }
                self.draft += ComposerPasteRoute.pathInsert(path, after: self.draft)
            }
        }
    }
}
