import SwiftUI
import TogoKit

/// The native settings hierarchy uses the same Form fields as API settings.
/// Page selection changes the original preferences source, never a local copy.
struct NativePreferencesPages: View {
    let context: NativeCommandContext

    var body: some View {
        let pages = SurfaceValues.entries((try? context.current())?.properties["pages"])
        List {
            ForEach(pages) { page in
                NavigationLink {
                    Page(context: context, id: page.id, title: SurfaceValues.text(page.properties["label"]))
                } label: {
                    Text(verbatim: SurfaceValues.text(page.properties["label"]))
                }
                .disabled(SurfaceValues.disabled(page.properties["disabled"]))
                .groupedRows()
            }
        }
        .groupedList()
    }

    private struct Page: View {
        let context: NativeCommandContext
        let id: String
        let title: String
        @State private var failure: String?

        var body: some View {
            Group {
                if let failure {
                    StateView(kind: .error, title: PrimitiveCopy.couldNotLoad, message: failure)
                } else {
                    mappedPage
                }
            }
            .navigationTitle(title)
            .task(id: id) {
                do { try await context.page(id) }
                catch is CancellationError {}
                catch { failure = PrimitiveCopy.noAnswer }
            }
        }

        @ViewBuilder private var mappedPage: some View {
            let result = Result { () throws -> HarnessScreen.Settings? in
                let node = try context.current()
                guard node.properties["page"] == .string(id) else { return nil }
                return try NativeStructuredScreen.preferences(node, title: title).settings
            }
            switch result {
            case .success(let settings?): SettingsPrimitive(settings: settings, context: context.primitive())
            case .success(nil): StateView(kind: .loading, title: PrimitiveCopy.loading, message: "")
            case .failure(let error):
                let failure = NativeCommandFailure(error)
                StateView(kind: .error, title: PrimitiveCopy.couldNotLoad, message: failure.message)
                    .task(id: failure.category) { failure.log() }
            }
        }
    }
}
