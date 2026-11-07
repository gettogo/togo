#if DEBUG
import SwiftUI
import TogoKit

/// A sample screen in a navigation stack, acting through preview services so selections,
/// settings and answers change what it shows.
struct PrimitivePreviewHost: View {
    let screenID: String
    var services = PreviewHarnessServices()

    var body: some View {
        NavigationStack {
            HarnessScreenLoader(
                store: .preview(services), screenID: screenID,
                openScreen: { _ in }, open: { _ in }, showTerminal: {}
            )
            .navigationBarTitleDisplayMode(.inline)
        }
        .background(Color.togoInk)
    }
}

#Preview("Picker, model") { PrimitivePreviewHost(screenID: "model") }
#Preview("Picker, roles and filters") { PrimitivePreviewHost(screenID: "models") }
#Preview("Picker, Duo folded", traits: .fixedLayout(width: 466, height: 678)) { PrimitivePreviewHost(screenID: "models") }
#Preview("Picker, iPad", traits: .fixedLayout(width: 1032, height: 800)) {
    PrimitivePreviewHost(screenID: "models").environment(\.horizontalSizeClass, .regular)
}
#Preview("Picker, AX3") { PrimitivePreviewHost(screenID: "models").environment(\.dynamicTypeSize, .accessibility3) }
#Preview("Settings") { PrimitivePreviewHost(screenID: "settings") }
#Preview("Settings, AX3") { PrimitivePreviewHost(screenID: "settings").environment(\.dynamicTypeSize, .accessibility3) }
#Preview("Checklist") { PrimitivePreviewHost(screenID: "plan") }
#Preview("Checklist, Duo folded", traits: .fixedLayout(width: 466, height: 678)) { PrimitivePreviewHost(screenID: "plan") }
#Preview("Live list") { PrimitivePreviewHost(screenID: "jobs") }
#Preview("Live list, AX3") { PrimitivePreviewHost(screenID: "jobs").environment(\.dynamicTypeSize, .accessibility3) }
#Preview("Detail, diff") { PrimitivePreviewHost(screenID: "patch") }
#Preview("Detail, markdown") { PrimitivePreviewHost(screenID: "notes") }
#Preview("Detail, iPad", traits: .fixedLayout(width: 1032, height: 800)) { PrimitivePreviewHost(screenID: "patch") }
#Preview("Confirm, questions") { PrimitivePreviewHost(screenID: "question") }
#Preview("Confirm, needs you") {
    NavigationStack {
        ConfirmPrimitive(confirm: HarnessSamples.question.confirm!, context: .inert, needsYou: true)
            .navigationTitle("Question")
    }
}
#Preview("Confirm, plain") { PrimitivePreviewHost(screenID: "compact") }
#Preview("Confirm, AX3") { PrimitivePreviewHost(screenID: "question").environment(\.dynamicTypeSize, .accessibility3) }
#Preview("Status") { PrimitivePreviewHost(screenID: "usage") }
#Preview("Status, Duo folded", traits: .fixedLayout(width: 466, height: 678)) { PrimitivePreviewHost(screenID: "usage") }
#Preview("Unknown primitive") {
    NavigationStack {
        PrimitiveScreenView(
            screen: HarnessScreen(id: "x", primitive: "timeline", title: "Timeline", fetchedAt: HarnessSamples.at),
            context: .inert
        )
    }
}
#Preview("Read failed") {
    let services = PreviewHarnessServices()
    services.failing["usage"] = true
    return PrimitivePreviewHost(screenID: "usage", services: services)
}
#endif
