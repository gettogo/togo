import SwiftUI
import TogoKit
import UIKit

/// A file, read-only: syntax colours and line numbers with find, an image with zoom, or a
/// sentence saying why the file is not shown.
struct FileViewer: View {
    @State private var model: FileViewerModel

    @Environment(\.dynamicTypeSize) private var typeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// The header (ref, cut-short notice) as drawn: it arrives with the file in a
    /// `resize` transaction, so the code below moves down with it.
    @State private var readyShown = LiveMirror<Bool>()

    init(model: FileViewerModel) {
        _model = State(initialValue: model)
    }

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .phaseSwap(model.state)
            .background(Color.togoInk)
            .safeAreaInset(edge: .top, spacing: 0) { header }
            .mirrors(model.state == .ready, into: $readyShown)
            .navigationTitle(Text(verbatim: model.fileName))
            // The folder is the subtitle, as on a diff, rather than a row of its own.
            .navigationSubtitle(Text(verbatim: model.breadcrumb))
            .navigationBarTitleDisplayMode(.inline)
            .modifier(FindBar(model: model))
            .toolbar {
                if case .text? = model.content {
                    ToolbarItem(placement: .topBarTrailing) {
                        Menu {
                            Toggle(FilesCopy.wrap, isOn: $model.wrap)
                                .menuToggle()
                                .disabled(typeSize.isAccessibilitySize)
                        } label: {
                            Label(FilesCopy.options, systemImage: "slider.horizontal.3")
                        }
                    }
                }
            }
            .task { await model.loadIfNeeded() }
    }

    @ViewBuilder private var content: some View {
        switch model.state {
        case .loading:
            StateView(kind: .loading, title: FilesCopy.loadingFile, message: model.displayPath)
        case .failed(let message):
            StateView(kind: .error, title: FilesCopy.failedFile, message: message, action: .init(FilesCopy.tryAgain) {
                Task { await model.load() }
            })
        case .ready:
            switch model.content {
            case .text:
                CodeLinesView(
                    rows: model.displayRows, wrap: model.wrap, showsOldNumbers: false, scrollTo: model.scrollTarget
                )
            case .image(let file):
                ZoomableImage(image: file.picture, label: model.fileName)
            case .binary(let size):
                FileNotice(symbol: "doc", title: FilesCopy.binary(size: size), name: model.fileName)
            case .empty:
                StateView(kind: .empty, title: FilesCopy.emptyFile, message: FilesCopy.emptyFileMessage)
            case nil:
                EmptyView()
            }
        }
    }

    @ViewBuilder private var header: some View {
        if readyShown.read(model.state == .ready) {
            VStack(alignment: .leading, spacing: 4) {
                if let ref = model.refBanner {
                    Text(verbatim: ref).font(.footnote).foregroundStyle(Color.togoMute)
                }
                if let notice = model.truncatedNotice {
                    Text(notice).font(.footnote).foregroundStyle(Color.togoMute)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Layout.margin)
            .padding(.vertical, hasHeader ? 8 : 0)
            .background(Color.togoInk)
            // motion: state change — the file loaded; its header fades in with it, nothing rises
            .transition(Motion.arrive(reduce: reduceMotion))
        }
    }

    private var hasHeader: Bool {
        model.refBanner != nil || model.truncatedNotice != nil
    }
}

/// Find in a text file: the search field, and previous and next buttons with the position.
private struct FindBar: ViewModifier {
    @Bindable var model: FileViewerModel
    @Environment(\.reviewFindOpen) private var findOpen
    @State private var presented = false

    func body(content: Content) -> some View {
        if case .text? = model.content {
            content
                .searchable(text: $model.query, isPresented: $presented, prompt: FilesCopy.findPrompt)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .onSubmit(of: .search) { model.next() }
                // Escape clears and closes the find field first; the review's own Escape waits.
                .onChange(of: presented, initial: true) { _, open in findOpen?.wrappedValue = open }
                .onDisappear { findOpen?.wrappedValue = false }
                .background {
                    if presented {
                        Button(FilesCopy.closeFind) {
                            model.query = ""
                            presented = false
                        }
                        .keyboardShortcut(.cancelAction)
                        .opacity(0)
                        .accessibilityHidden(true)
                    }
                }
                .toolbar {
                    if let position = model.position {
                        ToolbarItemGroup(placement: .bottomBar) {
                            Button(FilesCopy.previousMatch, systemImage: "chevron.up") { model.previous() }
                                .disabled(model.matches.isEmpty)
                                .accessibilityValue(position)
                            // The count rides on the symbol as a badge so the bar keeps only icon items.
                            Button(FilesCopy.nextMatch, systemImage: "chevron.down") { model.next() }
                                .badge(model.matches.count)
                                .disabled(model.matches.isEmpty)
                                .accessibilityValue(position)
                        }
                    }
                }
        } else {
            content
        }
    }
}

private struct FileNotice: View {
    let symbol: String
    let title: String
    let name: String

    var body: some View {
        VStack(spacing: 6) {
            Image(systemName: symbol)
                .font(.system(.largeTitle).weight(.light))
                .foregroundStyle(Color.togoMute)
                .accessibilityHidden(true)
            Text(title)
                .font(.title3.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
            Text(verbatim: name)
                .font(.system(.footnote, design: .monospaced))
                .foregroundStyle(Color.togoMute)
        }
        .multilineTextAlignment(.center)
        .accessibilityElement(children: .combine)
        .padding(.horizontal, Layout.margin)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// An image that fits the screen, pinches up to 5 times its size and toggles with a double tap.
/// The picture was decoded with the file (`FileImage`); a pinch only changes the frame.
private struct ZoomableImage: View {
    let image: UIImage
    let label: String

    @State private var zoom: CGFloat = 1
    @State private var settled: CGFloat = 1
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        GeometryReader { proxy in
            ScrollView([.horizontal, .vertical]) {
                Image(uiImage: image)
                    .resizable()
                    .interpolation(.medium)
                    .scaledToFit()
                    .frame(width: proxy.size.width * zoom, height: proxy.size.height * zoom)
            }
            .scrollIndicators(zoom > 1 ? .visible : .hidden)
            .defaultScrollAnchor(.center)
            .gesture(
                MagnifyGesture()
                    .onChanged { zoom = min(max(settled * $0.magnification, 1), 5) }
                    .onEnded { _ in settled = zoom }
            )
            // VoiceOver's three-finger double tap and zoom gestures, and Switch Control.
            .accessibilityZoomAction { action in
                zoom = action.direction == .zoomIn ? min(zoom * 2, 5) : max(zoom / 2, 1)
                settled = zoom
            }
            .onTapGesture(count: 2) {
                // motion: cause → effect — the double tap zooms the picture where it was tapped
                withAnimation(Motion.animation(.resize, reduce: reduceMotion)) {
                    zoom = zoom > 1 ? 1 : 3
                    settled = zoom
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text(verbatim: label))
            .accessibilityAddTraits(.isImage)
            .accessibilityHint(FilesCopy.zoomHint)
        }
    }
}

#if DEBUG
extension FileViewerModel {
    static func preview(
        _ path: String = "src/router.ts", ref: String? = nil, services: FakeReviewServices = FakeReviewServices()
    ) -> FileViewerModel {
        FileViewerModel(subject: DiffModel.previewSubject, services: services, path: path, ref: ref)
    }
}

#Preview("Code") {
    NavigationStack { FileViewer(model: .preview()) }
}
#endif
