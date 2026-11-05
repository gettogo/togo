import SwiftUI
import TogoKit
import UIKit

/// A file, read-only: syntax colours and line numbers with find, an image with zoom, or a
/// sentence saying why the file is not shown.
struct FileViewer: View {
    @State private var model: FileViewerModel

    @Environment(\.dynamicTypeSize) private var typeSize

    init(model: FileViewerModel) {
        _model = State(initialValue: model)
    }

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
            .safeAreaInset(edge: .top, spacing: 0) { header }
            .navigationTitle(Text(verbatim: model.fileName))
            .navigationBarTitleDisplayMode(.inline)
            .modifier(FindBar(model: model))
            .toolbar {
                if case .text? = model.content {
                    ToolbarItem(placement: .topBarTrailing) {
                        Menu {
                            Toggle(FilesCopy.wrap, isOn: $model.wrap)
                                .disabled(typeSize.isAccessibilitySize)
                        } label: {
                            Label(FilesCopy.options, systemImage: "ellipsis")
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
            case .image(let data):
                ZoomableImage(data: data, label: model.fileName)
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
        if model.state == .ready {
            VStack(alignment: .leading, spacing: 4) {
                if !model.breadcrumb.isEmpty { PathText(model.breadcrumb) }
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
        }
    }

    private var hasHeader: Bool {
        !model.breadcrumb.isEmpty || model.refBanner != nil || model.truncatedNotice != nil
    }
}

/// Find in a text file: the search field, and previous and next buttons with the position.
private struct FindBar: ViewModifier {
    @Bindable var model: FileViewerModel

    func body(content: Content) -> some View {
        if case .text? = model.content {
            content
                .searchable(text: $model.query, prompt: FilesCopy.findPrompt)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .onSubmit(of: .search) { model.next() }
                .toolbar {
                    if let position = model.position {
                        ToolbarItemGroup(placement: .bottomBar) {
                            Button(FilesCopy.previousMatch, systemImage: "chevron.up") { model.previous() }
                                .disabled(model.matches.isEmpty)
                            Text(verbatim: position)
                                .font(.footnote.monospacedDigit())
                                .foregroundStyle(Color.togoMute)
                            Button(FilesCopy.nextMatch, systemImage: "chevron.down") { model.next() }
                                .disabled(model.matches.isEmpty)
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
private struct ZoomableImage: View {
    let data: Data
    let label: String

    @State private var zoom: CGFloat = 1
    @State private var settled: CGFloat = 1
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        GeometryReader { proxy in
            if let image = UIImage(data: data) {
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
                .onTapGesture(count: 2) {
                    withAnimation(Motion.motion(Motion.confirm, reduce: reduceMotion)) {
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
