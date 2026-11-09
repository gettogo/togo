import PhotosUI
import SwiftUI
import TogoKit
import UIKit

/// The row of pictures above the composer's field: each a thumbnail with its own Remove,
/// progress while it uploads, Try Again when it did not. Tapping one previews it.
struct AttachmentStrip: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let attachments: ComposerAttachments
    @State private var previewing: ImageViewerRequest?

    var body: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 8) {
                ForEach(Array(attachments.items.enumerated()), id: \.element.id) { index, item in
                    AttachmentChip(
                        item: item, index: index, count: attachments.items.count,
                        onPreview: { previewing = ImageViewerRequest(index: index) },
                        onRemove: { attachments.remove(item.id) },
                        onRetry: { Task { await attachments.retry(item.id) } }
                    )
                    // Chips arrive and leave in ComposerAttachments' `resize` transactions.
                    // motion: cause → effect — the chip arrives by opacity in place as the strip grows.
                    .transition(Motion.arrive(reduce: reduceMotion))
                }
            }
            .padding(.horizontal, 12)
            .padding(.top, 10)
            .padding(.bottom, 2)
        }
        .scrollIndicators(.hidden)
        .presentCover("composer.attachmentPreview", item: $previewing) { request in
            let items = attachments.items
            ChatImageViewer(
                images: items.map { HarnessImage(path: $0.path) },
                index: min(request.index, max(0, items.count - 1)),
                preloaded: Dictionary(uniqueKeysWithValues: items.enumerated().map { ($0.offset, $0.element.preview) })
            )
        }
    }
}

private struct AttachmentChip: View {
    let item: ComposerAttachments.Item
    let index: Int
    let count: Int
    let onPreview: () -> Void
    let onRemove: () -> Void
    let onRetry: () -> Void

    static let side: CGFloat = 56

    var body: some View {
        Button(action: failed ? onRetry : onPreview) {
            Image(uiImage: item.preview)
                .resizable()
                .aspectRatio(contentMode: .fill)
                .frame(width: Self.side, height: Self.side)
                .clipShape(.rect(cornerRadius: 10, style: .continuous))
                .overlay { overlay }
                .overlay {
                    RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Color.togoLine, lineWidth: 0.5)
                }
        }
        .buttonStyle(.plain)
        .overlay(alignment: .topTrailing) {
            Button(ComposerCopy.removeImage, systemImage: "xmark", action: onRemove)
                .labelStyle(.iconOnly)
                .font(.caption2.weight(.bold))
                .foregroundStyle(Color.togoInk)
                .frame(width: 20, height: 20)
                .background(Color.togoSnow, in: .circle)
                // A 44 pt target around the small badge, centred where the badge sits.
                .frame(width: 44, height: 44)
                .contentShape(.rect)
                .buttonStyle(.plain)
                .offset(x: 16, y: -16)
                .accessibilityHidden(true)
        }
        .padding(.top, 8)
        .padding(.trailing, 8)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(ComposerCopy.attachment(index + 1, of: count, state: stateWord))
        .accessibilityAddTraits(.isButton)
        .accessibilityAction { failed ? onRetry() : onPreview() }
        .accessibilityActions {
            Button(ComposerCopy.removeImage, action: onRemove)
            if failed { Button(ComposerCopy.retryImage, action: onRetry) }
        }
        .contextMenu {
            Button(ComposerCopy.previewImage, systemImage: "eye", action: onPreview)
            if failed { Button(ComposerCopy.retryImage, systemImage: "arrow.clockwise", action: onRetry) }
            Button(ComposerCopy.removeImage, systemImage: "trash", role: .destructive, action: onRemove)
        }
    }

    private var failed: Bool { if case .failed = item.state { true } else { false } }

    private var stateWord: String? {
        switch item.state {
        case .uploading: ComposerCopy.uploading
        case .failed: ComposerCopy.uploadFailed
        case .ready: nil
        }
    }

    @ViewBuilder private var overlay: some View {
        switch item.state {
        case .uploading:
            ZStack {
                Color.black.opacity(0.35)
                ProgressView().controlSize(.small).tint(Color.togoSnow)
            }
        case .failed:
            ZStack {
                Color.black.opacity(0.5)
                Image(systemName: "arrow.clockwise").font(.footnote.weight(.semibold)).foregroundStyle(Color.togoSnow)
            }
        case .ready:
            EmptyView()
        }
    }
}

/// The composer's picture button: Photo Library (several at once), Take Photo, Paste Image.
struct AttachMenu: View {
    let attachments: ComposerAttachments
    @State private var picking = false
    @State private var camera = false
    @State private var selection: [PhotosPickerItem] = []

    var body: some View {
        Menu {
            Button(ComposerCopy.photos, systemImage: "photo.on.rectangle") { picking = true }
            if UIImagePickerController.isSourceTypeAvailable(.camera) {
                Button(ComposerCopy.camera, systemImage: "camera") { camera = true }
            }
            Button(ComposerCopy.pasteImage, systemImage: "doc.on.clipboard") { paste() }
                .disabled(!UIPasteboard.general.hasImages)
        } label: {
            Label(ComposerCopy.attach, systemImage: "photo.badge.plus")
        } primaryAction: {
            picking = true
        }
        .labelStyle(.iconOnly)
        .menuStyle(.button)
        .buttonStyle(ComposerChipStyle())
        .disabled(attachments.isFull)
        .accessibilityIdentifier("composer.attach")
        .photosPicker(
            isPresented: $picking, selection: $selection, maxSelectionCount: max(1, attachments.room),
            selectionBehavior: .ordered, matching: .images, preferredItemEncoding: .compatible
        )
        .onChange(of: selection) { _, picked in
            guard !picked.isEmpty else { return }
            selection = []
            Task {
                var datas: [Data] = []
                for item in picked {
                    if let data = try? await item.loadTransferable(type: Data.self) { datas.append(data) }
                }
                await attachments.add(datas)
            }
        }
        .presentCover("composer.camera", isPresented: $camera) {
            CameraPicker { image in
                guard let data = image.jpegData(compressionQuality: 0.9) else { return }
                Task { await attachments.add([data]) }
            }
            .ignoresSafeArea()
        }
    }

    private func paste() {
        let datas = (UIPasteboard.general.images ?? []).compactMap { $0.pngData() }
        guard !datas.isEmpty else { return }
        Task { await attachments.add(datas) }
    }
}

/// The system camera, for one photo.
private struct CameraPicker: UIViewControllerRepresentable {
    let onPhoto: (UIImage) -> Void
    @Environment(\.dismiss) private var dismiss

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ controller: UIImagePickerController, context: Context) {}

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    final class Coordinator: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
        let parent: CameraPicker
        init(_ parent: CameraPicker) { self.parent = parent }

        func imagePickerController(
            _ picker: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]
        ) {
            if let image = info[.originalImage] as? UIImage { parent.onPhoto(image) }
            parent.dismiss()
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            parent.dismiss()
        }
    }
}
